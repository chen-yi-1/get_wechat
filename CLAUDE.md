# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Development Commands

```bash
# Build
go build -o bin/chatlog.exe main.go        # local build (CGO_ENABLED=1 required)
make build                                  # via Makefile

# Test
go test ./... -cover                        # all tests with coverage
make test                                   # via Makefile

# Lint
golangci-lint run ./...                     # lint all packages
make lint                                   # via Makefile

# Tidy
go mod tidy
make tidy                                   # via Makefile

# Run
go run main.go                              # TUI mode
go run main.go server --config <path>       # headless server mode
go run main.go key                          # CLI key extraction

# Cross-build (Windows only)
make crossbuild

# Python web app (separate process)
cd chatmodel && python web_app.py

# Start everything (dev)
start.bat                                   # launches Go TUI + Python web app + browser
```

## Project Architecture

This is a **WeChat chat record decryption tool** (Go 1.24, Windows-only). It extracts database keys from WeChat processes (DLL injection + memory scanning), decrypts SQLite databases, and serves data via TUI, HTTP API, and MCP protocol for AI assistants.

### Layer Structure

```
main.go → cmd/chatlog/           # CLI entry (cobra)
  └─ internal/chatlog/           # Core application
       ├── manager.go            # Service orchestrator
       ├── app.go                # TUI application
       ├── ctx/                  # Application state/context (config + current account)
       ├── conf/                 # Configuration loading
       ├── database/             # DB service layer (wraps wechatdb)
       ├── http/                 # Gin HTTP server + MCP server (mark3labs/mcp-go)
       │   ├── mcp.go            # SSE/Streamable HTTP MCP transport
       │   ├── mcp_desc.go       # MCP tool descriptions (long-form prompts for LLMs)
       │   ├── route.go          # API routes (chatlog, contact, session, media, SNS, DB browser)
       │   └── service.go        # HTTP service lifecycle, FunASR daemon management
       ├── wechat/               # WeChat file monitoring & auto-decrypt
       └── webhook/              # Webhook push on new messages
  ├─ internal/wechat/            # WeChat process interaction
  │   ├── key/                   # Key extraction (DLL injector + native memory scanner)
  │   ├── decrypt/               # DB decryption
  │   └── process/               # Process detection
  ├─ internal/wechatdb/          # DB abstraction (DataSource + Repository pattern)
  │   ├── datasource/            # DB connection management, supports v4
  │   │   └── v4/                # WeChat v4 data source implementation
  │   └── repository/            # Business queries (message, contact, chatroom, session, media)
  ├─ internal/model/             # Domain model types
  ├─ internal/mcp/               # MCP protocol implementation (JSON-RPC, SSE, stdio)
  ├─ internal/errors/            # Error types & HTTP error middleware
  └─ internal/ui/                # TUI components (tview + tcell)
       ├── menu/                 # Main menu
       ├── footer/               # Status footer with latest message
       ├── infobar/              # Info bar (keys, status, dirs)
       └── form/                 # Settings forms
  ├─ pkg/
  │   ├── config/                # Viper-based config persistence (~/.chatlog/config.json)
  │   ├── util/                  # dat2img (image decryption), silk (audio), zstd, lz4, time
  │   ├── process/               # Single-instance lock
  │   ├── filemonitor/           # fsnotify-based file watching
  │   ├── filecopy/              # File copy with cache
  │   └── appver/                # Windows version detection
  └─ chatmodel/                  # Python Flask web app (separate process)
       ├── web_app.py            # Flask web UI + LLM chat interface
       ├── llm_client.py         # LLM API client
       ├── mcp_client.py         # MCP client for tool calling
       └── static/               # Frontend assets
```

### Key Data Flow

1. **Key Extraction**: Detect WeChat process → DLL inject → capture DB key + memory scan for image key
2. **Decryption**: Use keys to decrypt SQLite `.db` files from WeChat data dir to work dir
3. **Serving**: HTTP API + MCP protocol exposes decrypted data (messages, contacts, media)
4. **Auto-decrypt**: fsnotify watches data dir, incrementally decrypts WAL changes
5. **MCP Tools**: query_contact, query_chat_room, query_recent_chat, query_chat_log, get_media_content, ocr_image_message, analyze_chat_activity, get_user_profile, search_shared_files + 3 prompts

### HTTP API Routes

| Route | Description |
|-------|-------------|
| `GET /api/v1/chatlog` | Query messages (params: time, talker, sender, keyword, limit, offset, format) |
| `GET /api/v1/contact` | Query contacts |
| `GET /api/v1/chatroom` | Query chat rooms |
| `GET /api/v1/session` | Query recent sessions |
| `GET /api/v1/sns` | Query SNS timeline (朋友圈) |
| `GET /api/v1/db/*` | Database browser & SQL console |
| `GET /image/*key` | Image media with auto-decryption |
| `POST /mcp` | Streamable HTTP MCP endpoint |
| `GET /sse` | SSE MCP endpoint |

### MCP Transport

Supports both **SSE** (for Claude Desktop) and **Streamable HTTP** (for Claude Code). Configured at `127.0.0.1:5030/mcp`.

### MCP Tool Usage Pattern

The `query_chat_log` tool enforces a multi-step query flow documented in `internal/chatlog/http/mcp_desc.go`:
- Step 1: Broad search with keyword/sender to locate relevant timestamps
- Step 2: Narrow context queries per timestamp (without keyword/sender)
- Step 3: Synthesize all contexts

## Key Technical Details

- **CGO is required** (mattn/go-sqlite3, Windows DLL calls)
- **Windows only** (process memory reading, DLL injection)
- **Config**: JSON file at `~/.chatlog/config.json` (managed by viper)
- **Logging**: zerolog, structured debug logs
- **Smart message ID**: `timestamp * 1000000 + local_id` to avoid ID collision
- **Media files**: `.dat` files decrypted via XOR/AES → JPEG/PNG; Silk audio → MP3
- **ChatLab export**: Standardized JSON format (v0.0.1) for cross-platform analysis
- **Supported WeChat**: v4.x only (v3 support removed)
