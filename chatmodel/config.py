import os
from dataclasses import dataclass
from pathlib import Path


@dataclass(frozen=True)
class Settings:
    llm_api_key: str
    llm_model: str
    llm_base_url: str | None
    mcp_url: str
    db_path: Path
    max_history_messages: int
    max_tool_calls_per_turn: int


def _load_dotenv(dotenv_path: Path) -> None:
    if not dotenv_path.exists():
        return
    for raw_line in dotenv_path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip('"').strip("'")
        if key and key not in os.environ:
            os.environ[key] = value


def load_settings() -> Settings:
    root_dir = Path(__file__).resolve().parent
    data_dir = root_dir / "data"
    data_dir.mkdir(parents=True, exist_ok=True)
    _load_dotenv(root_dir / ".env")

    # 优先使用 DeepSeek 环境变量，同时兼容 OpenAI 命名。
    api_key = os.getenv("DEEPSEEK_API_KEY", "").strip() or os.getenv("OPENAI_API_KEY", "").strip()
    if not api_key:
        raise RuntimeError("缺少 API Key。请在环境变量或 chatmodel/.env 中设置 DEEPSEEK_API_KEY。")

    base_url = os.getenv("DEEPSEEK_BASE_URL", "").strip() or os.getenv("OPENAI_BASE_URL", "").strip()
    if not base_url:
        base_url = "https://api.deepseek.com"

    model = os.getenv("DEEPSEEK_MODEL", "").strip() or os.getenv("OPENAI_MODEL", "").strip()
    if not model:
        model = "deepseek-chat"

    return Settings(
        llm_api_key=api_key,
        llm_model=model,
        llm_base_url=base_url,
        mcp_url=os.getenv("CHATLOG_MCP_URL", "http://127.0.0.1:5030/mcp").strip(),
        db_path=data_dir / "chatmodel.db",
        max_history_messages=int(os.getenv("CHATMODEL_MAX_HISTORY", "20")),
        max_tool_calls_per_turn=int(os.getenv("CHATMODEL_MAX_TOOL_CALLS", "8")),
    )
