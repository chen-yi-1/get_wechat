from __future__ import annotations

import json
from typing import Any

import httpx
from openai import APIConnectionError, AuthenticationError, OpenAI, RateLimitError, APIStatusError

from config import Settings
from mcp_client import MCPHTTPClient, MCPTool


SYSTEM_PROMPT = """你是一个严谨的微信工作群分析助手，核心任务是总结工作群消息、提取行动项、识别风险和关键决策。
你可以使用 chatlog MCP 工具检索用户聊天记录。

强制规则：
1. 只要聊天记录里出现 [图片] 或 [语音]，必须调用工具进一步查看，不得跳过。
2. 图片消息优先调用 ocr_image_message；必要时调用 get_media_content 获取原始媒体信息。
3. 语音消息必须调用 get_media_content 获取转写文本后再分析。
4. 禁止在未查看图片/语音内容前下结论。

输出要求：
1. 先给结论，再给证据。
2. 对不确定信息明确标注“不确定”并说明缺失数据。
3. 不编造聊天记录、时间、联系人。
4. 在适合图示表达时使用 ```mermaid 代码块```，并保证语法正确。
"""


class ChatEngine:
    def __init__(self, settings: Settings, mcp_client: MCPHTTPClient):
        client_kwargs: dict[str, Any] = {"api_key": settings.llm_api_key}
        if settings.llm_base_url:
            client_kwargs["base_url"] = settings.llm_base_url
        client_kwargs["timeout"] = httpx.Timeout(120.0, connect=10.0)
        self._client = OpenAI(**client_kwargs)
        self._model = settings.llm_model
        self._mcp_client = mcp_client
        self._max_tool_calls_per_turn = settings.max_tool_calls_per_turn

    @staticmethod
    def _to_openai_tools(tools: list[MCPTool]) -> list[dict[str, Any]]:
        openai_tools: list[dict[str, Any]] = []
        for tool in tools:
            schema = dict(tool.input_schema) if tool.input_schema else {"type": "object", "properties": {}}
            schema.setdefault("type", "object")
            schema.setdefault("properties", {})
            openai_tools.append(
                {
                    "type": "function",
                    "function": {
                        "name": tool.name,
                        "description": tool.description,
                        "parameters": schema,
                    },
                }
            )
        return openai_tools

    def summarize_session(self, old_summary: str, recent_messages: list[dict[str, str]]) -> str:
        transcript = "\n".join([f"{m['role']}: {m['content']}" for m in recent_messages])
        prompt = (
            "请把下面会话压缩为长期记忆，要求简洁、可检索、保留事实。"
            "输出 6~10 条要点，每条一行，包含人物、事件、时间或约束（若有）。"
        )
        content = f"旧摘要:\n{old_summary or '(无)'}\n\n最近对话:\n{transcript}"
        try:
            resp = self._client.chat.completions.create(
                model=self._model,
                messages=[
                    {"role": "system", "content": prompt},
                    {"role": "user", "content": content},
                ],
                temperature=0.2,
            )
        except APIConnectionError as err:
            raise RuntimeError(
                f"LLM 连接失败（摘要阶段）: {err}. base_url={self._client.base_url}, model={self._model}"
            ) from err
        except AuthenticationError as err:
            raise RuntimeError(f"LLM 鉴权失败（摘要阶段）: {err}") from err
        except RateLimitError as err:
            raise RuntimeError(f"LLM 速率限制（摘要阶段）: {err}") from err
        except APIStatusError as err:
            raise RuntimeError(f"LLM API错误（摘要阶段）: status={err.status_code}, {err.message}") from err
        return (resp.choices[0].message.content or "").strip()

    def chat(self, session_summary: str, history_messages: list[dict[str, str]], user_message: str) -> str:
        mcp_tools = self._mcp_client.list_tools()
        tools = self._to_openai_tools(mcp_tools)

        messages: list[dict[str, Any]] = [{"role": "system", "content": SYSTEM_PROMPT}]
        if session_summary.strip():
            messages.append({"role": "system", "content": f"这是该会话的长期记忆:\n{session_summary}"})
        messages.extend(history_messages)
        messages.append({"role": "user", "content": user_message})

        tool_calls_count = 0
        while True:
            try:
                resp = self._client.chat.completions.create(
                    model=self._model,
                    messages=messages,
                    tools=tools,
                    tool_choice="auto",
                    temperature=0.3,
                )
            except APIConnectionError as err:
                raise RuntimeError(
                    f"LLM 连接失败: {err}. base_url={self._client.base_url}, model={self._model}"
                ) from err
            except AuthenticationError as err:
                raise RuntimeError(f"LLM 鉴权失败: {err}") from err
            except RateLimitError as err:
                raise RuntimeError(f"LLM 速率限制: {err}") from err
            except APIStatusError as err:
                raise RuntimeError(f"LLM API错误: status={err.status_code}, {err.message}") from err
            msg = resp.choices[0].message
            tool_calls = msg.tool_calls or []

            if not tool_calls:
                return (msg.content or "").strip()

            messages.append(
                {
                    "role": "assistant",
                    "content": msg.content or "",
                    "tool_calls": [
                        {
                            "id": call.id,
                            "type": "function",
                            "function": {
                                "name": call.function.name,
                                "arguments": call.function.arguments,
                            },
                        }
                        for call in tool_calls
                    ],
                }
            )

            for call in tool_calls:
                tool_calls_count += 1
                if tool_calls_count > self._max_tool_calls_per_turn:
                    messages.append(
                        {
                            "role": "tool",
                            "tool_call_id": call.id,
                            "content": "工具调用次数已达到上限，请直接给出当前最佳答案并声明限制。",
                        }
                    )
                    continue

                try:
                    arguments = json.loads(call.function.arguments or "{}")
                except json.JSONDecodeError:
                    arguments = {}
                tool_output = self._mcp_client.call_tool(call.function.name, arguments)
                messages.append(
                    {
                        "role": "tool",
                        "tool_call_id": call.id,
                        "content": tool_output or "(工具返回为空)",
                    }
                )

    def repair_mermaid_response(self, original_response: str, render_error: str) -> str:
        prompt = (
            "你要修复一段给前端渲染失败的 Mermaid 输出。"
            "请保持原有业务含义，修复语法错误并返回可渲染内容。"
            "如果原文包含普通说明文字，保留它。"
            "只输出最终可展示文本，不要解释。"
        )
        user_content = (
            f"原始回复:\n{original_response}\n\n"
            f"前端渲染错误:\n{render_error}\n\n"
            "请返回修复后的完整回复。"
        )
        try:
            resp = self._client.chat.completions.create(
                model=self._model,
                messages=[
                    {"role": "system", "content": prompt},
                    {"role": "user", "content": user_content},
                ],
                temperature=0.1,
            )
        except APIConnectionError as err:
            raise RuntimeError(f"Mermaid 修复时连接失败: {err}") from err
        except AuthenticationError as err:
            raise RuntimeError(f"Mermaid 修复时鉴权失败: {err}") from err
        except RateLimitError as err:
            raise RuntimeError(f"Mermaid 修复时速率限制: {err}") from err
        except APIStatusError as err:
            raise RuntimeError(f"Mermaid 修复时API错误: status={err.status_code}") from err
        return (resp.choices[0].message.content or "").strip()
