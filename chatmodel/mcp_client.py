from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any

import httpx

DEFAULT_PROTOCOL_VERSION = "2024-11-05"


@dataclass(frozen=True)
class MCPTool:
    name: str
    description: str
    input_schema: dict[str, Any]


class MCPHTTPClient:
    def __init__(self, base_url: str, timeout_seconds: float = 30.0):
        self._base_url = base_url
        self._client = httpx.Client(timeout=timeout_seconds)
        self._session_id: str | None = None
        self._request_id = 0
        self._initialized = False

    def _next_id(self) -> int:
        self._request_id += 1
        return self._request_id

    def _rpc(self, method: str, params: dict[str, Any] | None = None) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "jsonrpc": "2.0",
            "id": self._next_id(),
            "method": method,
        }
        if params is not None:
            payload["params"] = params

        headers = self._build_headers(with_session=True)
        try:
            resp = self._client.post(self._base_url, headers=headers, json=payload)
            resp.raise_for_status()
        except httpx.HTTPError as err:
            raise RuntimeError(
                f"MCP 连接失败: {err}. 请确认 chatlog HTTP 服务已启动且 `CHATLOG_MCP_URL` 可访问。"
            ) from err
        response_session_id = resp.headers.get("Mcp-Session-Id") or resp.headers.get("mcp-session-id")
        if response_session_id:
            self._session_id = response_session_id

        data = resp.json()
        if "error" in data:
            raise RuntimeError(f"MCP调用失败: {json.dumps(data['error'], ensure_ascii=False)}")
        if "result" not in data:
            raise RuntimeError(f"MCP响应缺少result字段: {json.dumps(data, ensure_ascii=False)}")
        return data["result"]

    def _build_headers(self, with_session: bool) -> dict[str, str]:
        headers = {
            "Content-Type": "application/json",
            "Accept": "application/json, text/event-stream",
            "MCP-Protocol-Version": DEFAULT_PROTOCOL_VERSION,
        }
        if with_session and self._session_id:
            headers["Mcp-Session-Id"] = self._session_id
        return headers

    def initialize(self) -> None:
        if self._initialized:
            return
        self._rpc(
            "initialize",
            {
                "protocolVersion": DEFAULT_PROTOCOL_VERSION,
                "capabilities": {"sampling": {}, "roots": {"listChanged": False}},
                "clientInfo": {"name": "chatmodel-cli", "version": "0.1.0"},
            },
        )
        # MCP 协议要求 initialize 后发送 initialized 通知。
        notify_payload = {"jsonrpc": "2.0", "method": "notifications/initialized"}
        headers = self._build_headers(with_session=True)
        try:
            resp = self._client.post(self._base_url, headers=headers, json=notify_payload)
            resp.raise_for_status()
        except httpx.HTTPError as err:
            raise RuntimeError(f"MCP initialized 通知失败: {err}") from err
        self._initialized = True

    def list_tools(self) -> list[MCPTool]:
        self.initialize()
        result = self._rpc("tools/list", {})
        tools = []
        for item in result.get("tools", []):
            tools.append(
                MCPTool(
                    name=item["name"],
                    description=item.get("description", ""),
                    input_schema=item.get("inputSchema", {"type": "object", "properties": {}}),
                )
            )
        return tools

    def call_tool(self, tool_name: str, arguments: dict[str, Any]) -> str:
        self.initialize()
        result = self._rpc("tools/call", {"name": tool_name, "arguments": arguments})
        if result.get("isError"):
            return f"[MCP工具错误] {tool_name}: {result}"

        output_parts: list[str] = []
        for content in result.get("content", []):
            ctype = content.get("type")
            if ctype == "text":
                output_parts.append(content.get("text", ""))
            elif ctype == "image":
                output_parts.append(
                    f"[image:mime={content.get('mimeType', 'unknown')},data_length={len(content.get('data', ''))}]"
                )
            elif ctype == "audio":
                output_parts.append(
                    f"[audio:mime={content.get('mimeType', 'unknown')},data_length={len(content.get('data', ''))}]"
                )
            else:
                output_parts.append(json.dumps(content, ensure_ascii=False))
        return "\n".join(part for part in output_parts if part).strip()

    def close(self) -> None:
        self._client.close()
