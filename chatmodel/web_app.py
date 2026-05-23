from __future__ import annotations

from pathlib import Path
from threading import Lock
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from fastapi.responses import HTMLResponse
from fastapi.staticfiles import StaticFiles
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel, Field

from config import load_settings
from llm_client import ChatEngine
from mcp_client import MCPHTTPClient
from memory_store import MemoryStore


class CreateSessionRequest(BaseModel):
    title: str = Field(default="新会话", min_length=1, max_length=100)


class SendMessageRequest(BaseModel):
    content: str = Field(min_length=1, max_length=4000)


class MermaidRepairRequest(BaseModel):
    original_response: str = Field(min_length=1, max_length=20000)
    render_error: str = Field(min_length=1, max_length=4000)


class ChatWebService:
    def __init__(self) -> None:
        self.settings = load_settings()
        self.store = MemoryStore(self.settings.db_path)
        self.mcp_client = MCPHTTPClient(self.settings.mcp_url)
        self.engine = ChatEngine(self.settings, self.mcp_client)
        self.reply_lock = Lock()

    def close(self) -> None:
        self.mcp_client.close()
        self.store.close()


BASE_DIR = Path(__file__).resolve().parent
svc = ChatWebService()


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Startup logic (if any)
    yield
    # Shutdown logic
    svc.close()


app = FastAPI(title="chatmodel-web", version="0.1.0", lifespan=lifespan)
app.mount("/static", StaticFiles(directory=str(BASE_DIR / "static")), name="static")
templates = Jinja2Templates(directory=str(BASE_DIR / "templates"))


@app.get("/", response_class=HTMLResponse)
def index(request: Request) -> HTMLResponse:
    # 兼容 Starlette 新签名：TemplateResponse(request, name, context)
    return templates.TemplateResponse(request, "index.html", {})


@app.get("/api/sessions")
def list_sessions() -> list[dict]:
    sessions = svc.store.list_sessions(limit=100)
    return [
        {
            "id": s.session_id,
            "title": s.title,
            "created_at": s.created_at,
            "updated_at": s.updated_at,
        }
        for s in sessions
    ]


@app.post("/api/sessions")
def create_session(payload: CreateSessionRequest) -> dict:
    session = svc.store.create_session(payload.title.strip() or "新会话")
    return {
        "id": session.session_id,
        "title": session.title,
        "created_at": session.created_at,
        "updated_at": session.updated_at,
    }


@app.delete("/api/sessions/{session_id}")
def delete_session(session_id: int) -> dict:
    try:
        svc.store.delete_session(session_id)
    except ValueError as err:
        raise HTTPException(status_code=404, detail=str(err)) from err
    return {"ok": True}


@app.get("/api/sessions/{session_id}/messages")
def get_messages(session_id: int) -> list[dict]:
    try:
        svc.store.get_session(session_id)
    except ValueError as err:
        raise HTTPException(status_code=404, detail=str(err)) from err
    return svc.store.recent_messages(session_id, limit=200)


@app.post("/api/sessions/{session_id}/messages")
def send_message(session_id: int, payload: SendMessageRequest) -> dict:
    content = payload.content.strip()
    if not content:
        raise HTTPException(status_code=400, detail="消息不能为空")
    try:
        svc.store.get_session(session_id)
    except ValueError as err:
        raise HTTPException(status_code=404, detail=str(err)) from err

    svc.store.append_message(session_id, "user", content)
    summary = svc.store.get_summary(session_id)
    history = svc.store.recent_messages(session_id, limit=svc.settings.max_history_messages)

    with svc.reply_lock:
        try:
            reply = svc.engine.chat(summary, history_messages=history, user_message=content)
        except Exception as err:  # noqa: BLE001
            reply = f"当前请求失败：{err}"

    svc.store.append_message(session_id, "assistant", reply)

    if svc.store.total_message_count(session_id) % 8 == 0:
        try:
            recent_for_summary = svc.store.recent_messages(session_id, limit=24)
            new_summary = svc.engine.summarize_session(summary, recent_for_summary)
            if new_summary:
                svc.store.upsert_summary(session_id, new_summary)
        except Exception:
            pass

    return {"assistant": reply}


@app.get("/api/sessions/{session_id}/memory")
def get_memory(session_id: int) -> dict:
    try:
        svc.store.get_session(session_id)
    except ValueError as err:
        raise HTTPException(status_code=404, detail=str(err)) from err
    return {"summary": svc.store.get_summary(session_id)}


@app.post("/api/sessions/{session_id}/repair-mermaid")
def repair_mermaid(session_id: int, payload: MermaidRepairRequest) -> dict:
    try:
        svc.store.get_session(session_id)
    except ValueError as err:
        raise HTTPException(status_code=404, detail=str(err)) from err

    with svc.reply_lock:
        try:
            repaired = svc.engine.repair_mermaid_response(
                original_response=payload.original_response,
                render_error=payload.render_error,
            )
        except Exception as err:  # noqa: BLE001
            raise HTTPException(status_code=500, detail=f"Mermaid 修复失败: {err}") from err

    try:
        svc.store.update_last_assistant_message(session_id, repaired)
    except ValueError:
        # 无可更新消息时仅返回修复文本，不阻断前端显示。
        pass
    return {"assistant": repaired}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8000)



