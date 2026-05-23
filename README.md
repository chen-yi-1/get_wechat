# 微信工作群智能分析助手

基于大语言模型（LLM）的微信工作群智能分析工具，通过 MCP 协议连接微信聊天记录，提供智能总结、行动项提取、风险识别等功能。

## 项目简介

本项目包含两个核心模块：

- **chatmodel**：基于 FastAPI 的 Web 应用，提供智能聊天分析服务
- **chatlog**：微信聊天记录解密工具，提供 MCP 服务接口（基于 [chatlog](https://github.com/sjzar/chatlog) 二开版本）

## 核心功能

### 智能分析能力
- **工作群消息总结**：自动总结工作群聊天内容，提取关键信息
- **行动项提取**：识别待办事项、任务分配等行动项
- **风险识别**：检测潜在风险和问题
- **关键决策识别**：提取重要决策和结论

### 多媒体支持
- **图片消息分析**：自动调用 OCR 识别图片内容
- **语音消息转写**：语音消息自动转文字后分析
- **媒体内容获取**：支持获取图片、语音等媒体文件

### 会话管理
- **多会话支持**：创建、管理多个聊天会话
- **长期记忆**：自动压缩历史对话，保留关键信息
- **会话历史**：完整的对话历史记录

### Web 界面
- **现代化 UI**：简洁美观的 Web 界面
- **实时对话**：流式响应，实时显示 AI 回复
- **Markdown 支持**：支持 Markdown 格式和 Mermaid 图表

## 技术架构

```
┌─────────────────┐
│   Web Frontend  │  (HTML/CSS/JavaScript)
└────────┬────────┘
         │ HTTP API
┌────────▼────────┐
│  FastAPI Server │  (chatmodel/web_app.py)
├─────────────────┤
│  Chat Engine    │  (LLM + Tool Calling)
├─────────────────┤
│   MCP Client    │  (Model Context Protocol)
└────────┬────────┘
         │ MCP Protocol
┌────────▼────────┐
│  chatlog MCP    │  (微信聊天记录服务)
├─────────────────┤
│  WeChat DB      │  (解密后的微信数据库)
└─────────────────┘
```

## 快速开始

### 环境要求

- Python 3.11+
- Go 1.24+ (用于 chatlog)
- Windows 操作系统

### 安装依赖

```bash
# 安装 Python 依赖
cd chatmodel
pip install -r requirements.txt
```

### 配置

在 `chatmodel` 目录下创建 `.env` 文件：

```env
# LLM 配置 (支持 DeepSeek 或 OpenAI)
DEEPSEEK_API_KEY=your_api_key_here
DEEPSEEK_BASE_URL=https://api.deepseek.com  # 可选
DEEPSEEK_MODEL=deepseek-chat  # 可选

# 或使用 OpenAI
# OPENAI_API_KEY=your_api_key_here
# OPENAI_BASE_URL=https://api.openai.com/v1  # 可选
# OPENAI_MODEL=gpt-4  # 可选

# MCP 服务地址
CHATLOG_MCP_URL=http://127.0.0.1:5030/mcp

# 可选配置
CHATMODEL_MAX_HISTORY=20  # 最大历史消息数
CHATMODEL_MAX_TOOL_CALLS=8  # 每轮最大工具调用次数
```

### 启动服务

1. **启动 chatlog 服务**（需要先解密微信数据库）

```bash
# 在项目根目录
go run main.go server
```

2. **启动 chatmodel Web 服务**

```bash
cd chatmodel
python web_app.py
```

3. **访问 Web 界面**

打开浏览器访问：http://127.0.0.1:8000

## 使用示例

### 分析工作群消息

```
用户：帮我总结一下产品开发群最近一周的讨论内容

AI：我来帮您分析产品开发群的聊天记录...

[调用 MCP 工具检索聊天记录]

根据检索结果，本周产品开发群的主要讨论内容包括：

1. **功能开发进度**
   - 用户认证模块已完成 80%
   - 支付接口集成遇到技术难点
   
2. **待办事项**
   - @张三 负责完成支付接口调试（截止：本周五）
   - @李四 准备下周的产品演示
   
3. **风险提示**
   - 支付接口可能影响上线时间
   - 需要增加测试覆盖率
```

### 提取行动项

```
用户：从昨天的项目会议群里提取所有待办事项

AI：正在检索项目会议群的聊天记录...

[调用 MCP 工具并分析图片和语音消息]

提取到以下待办事项：

1. 完成技术文档编写 - @王五 (截止：明天)
2. 准备客户演示材料 - @赵六 (截止：本周四)
3. 修复登录页面 Bug - 开发团队 (优先级：高)
```

## API 文档

### 会话管理

- `GET /api/sessions` - 获取会话列表
- `POST /api/sessions` - 创建新会话
- `DELETE /api/sessions/{session_id}` - 删除会话

### 聊天接口

- `POST /api/sessions/{session_id}/messages` - 发送消息
- `GET /api/sessions/{session_id}/messages` - 获取消息历史

### 辅助功能

- `POST /api/repair-mermaid` - 修复 Mermaid 图表语法

## MCP 工具列表

chatmodel 通过 MCP 协议调用以下工具：

- `query_contact` - 查询联系人信息
- `query_chat_room` - 查询群组信息
- `query_recent_chat` - 查询最近聊天
- `query_chat_log` - 查询聊天记录
- `get_media_content` - 获取媒体内容
- `ocr_image_message` - OCR 识别图片
- `analyze_chat_activity` - 分析聊天活跃度
- `get_user_profile` - 获取用户画像
- `search_shared_files` - 搜索共享文件

## 项目结构

```
get_wechat/
├── chatmodel/              # Python Web 应用
│   ├── web_app.py         # FastAPI 主应用
│   ├── llm_client.py      # LLM 客户端
│   ├── mcp_client.py      # MCP 客户端
│   ├── config.py          # 配置管理
│   ├── memory_store.py    # 会话存储
│   ├── templates/         # HTML 模板
│   └── static/            # 静态资源
├── internal/              # Go 核心代码
│   ├── chatlog/          # chatlog 核心逻辑
│   ├── wechat/           # 微信相关功能
│   └── wechatdb/         # 数据库操作
├── main.go               # Go 程序入口
└── README.md             # 本文件
```

## 技术栈

### 后端
- **FastAPI** - 现代 Python Web 框架
- **OpenAI SDK** - LLM API 客户端
- **httpx** - HTTP 客户端
- **SQLite** - 会话数据存储

### 前端
- **HTML/CSS/JavaScript** - 原生 Web 技术
- **Markdown** - 支持 Markdown 渲染
- **Mermaid** - 支持流程图等图表

### AI 集成
- **MCP Protocol** - Model Context Protocol
- **Tool Calling** - LLM 工具调用
- **Streaming** - 流式响应

## 注意事项

1. **数据安全**：所有数据在本地处理，不会上传到云端
2. **API Key 安全**：请妥善保管 API Key，不要提交到代码仓库
3. **微信版本**：chatlog 目前仅支持微信 4.x 版本
4. **性能优化**：大量聊天记录可能需要较长处理时间

## 致谢

- [chatlog](https://github.com/sjzar/chatlog) - 微信聊天记录解密工具
- [wx_key](https://github.com/ycccccccy/wx_key) - 微信密钥提取
- [FastAPI](https://fastapi.tiangolo.com/) - Web 框架
- [OpenAI](https://openai.com/) - LLM API

## 许可证

本项目仅供学习和研究使用，请勿用于非法用途。使用本工具产生的任何后果由使用者自行承担。
