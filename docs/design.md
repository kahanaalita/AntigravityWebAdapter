# AntigravityWebAdapter: Design Specification

> High-performance, zero-dependency Go adapter connecting Google Gemini Web (Google AI Pro tier — Hydrogen & Argon) to Antigravity and OpenAI-compatible clients via both stdio MCP and HTTP gateway.

## 1. Objectives

- **Dual Mode Interface:** Supports direct stdio MCP server mode (`--mcp`) for Antigravity integration and OpenAI-compatible HTTP SSE streaming on port 5690 (`--port`).
- **Zero External Dependencies:** Built with pure Go standard libraries (`net/http`, `net`, `encoding/json`, `os/exec`) and an in-tree RFC 6455 WebSocket client for Chrome DevTools Protocol (CDP).
- **Full Model Matrix Support:**
  1. Full Google AI Pro models: Hydrogen 4, Argon 4, 3.5 Flash-Lite, 3.1 Pro.
  2. Granular reasoning effort levels: `low`, `middle`/`medium`, `high`.
- **Advanced Capabilities:**
  1. Real-time streaming with separated thought traces (`delta.reasoning_content`) and output text (`delta.content`).
  2. Multimodal image pipeline: synthetic clipboard injection and drag-and-drop into the Gemini Quill editor.
  3. High-tolerance tool call AST parser (65+ syntax recovery rules, forward traversal, block stripping).
  4. Universal proxy autodetection (<15ms via `net.DialTimeout`).
  5. Anti-detect Chrome launch (`--disable-blink-features=AutomationControlled`).
  6. Automated UI handling: Anti-Canvas dismissal, auto-acceptance of upload terms, session reuse, and automatic temporary file cleanup.

## 2. Directory Layout

```
AntigravityWebAdapter/
├── cmd/
│   └── gemini-gateway/
│       └── main.go              # Entrypoint, CLI flag parsing (--mcp, --port, --chrome, --base, --proxy, --headless)
├── internal/
│   ├── cdp/
│   │   ├── client.go            # CDP client multiplexer, request/event dispatch
│   │   ├── launcher.go          # Anti-detect Chrome launcher, proxy autodetect
│   │   ├── ws.go                # RFC 6455 WebSocket client (zero-dep)
│   │   └── ws_test.go           # WebSocket RFC 6455 unit tests
│   ├── engine/
│   │   └── driver.go            # Browser driver, tab management, Quill editor injection, model/effort switcher, response capture
│   ├── mcp/
│   │   └── server.go            # Native stdio MCP server implementation (JSON-RPC 2.0, ping, tools/list, tools/call)
│   ├── models/
│   │   └── registry.go          # Model catalog and reasoning effort resolution
│   ├── multimodal/
│   │   └── image.go             # Multimodal image extraction, data URI decoding, and temporary file management
│   ├── parser/
│   │   └── toolcall.go          # AST JSON tool-call & reasoning extractor, tolerance rules, block stripping
│   ├── prompt/
│   │   └── system.go            # Prompt and conversation context builder (full and incremental)
│   └── server/
│       └── server.go            # HTTP OpenAI-compatible gateway (/v1/chat/completions, /v1/models, /login, SSE streaming)
├── tests/
│   └── parser_test.go           # Tool-call parser, escape repair, and block stripping tests
├── docs/
│   └── design.md                # Design architecture specification
├── go.mod
├── README.md                    # Russian documentation & installation guide
├── README.en.md                 # English documentation & installation guide
└── DEEPSEEK_HARNESS_SETUP.md    # Guide for DeepSeek Harness setup with OpenAI gateway
```

## 3. Data Flow

### A. MCP Stdio Mode (`--mcp`)
1. Antigravity or an MCP client launches `gemini-gateway --mcp` via stdio.
2. The MCP server (`internal/mcp/server.go`) handles JSON-RPC 2.0 requests (`initialize`, `ping`, `tools/list`, `tools/call`).
3. For tools (`hydrogen_low`, `hydrogen_middle`, `hydrogen_high`, `argon_low`, `argon_medium`, `argon_high`), image inputs (local paths, file URLs, base64 data URIs) are resolved and tracked via `internal/multimodal/image.go`.
4. Prompt and effort are dispatched to `internal/engine/driver.go` with a safe execution timeout (10 minutes).
5. The result (with reasoning trace formatted in `<details>` tags and generated text) is returned as an MCP text response; decoded temporary image files are cleanly pruned via `defer`.

### B. HTTP / OpenAI SSE Gateway Mode (`--port 5690`)
1. Client sends `POST /v1/chat/completions` (streaming or non-streaming).
2. `internal/server/server.go` resolves target model and effort (`internal/models/registry.go`), extracts multimodal images with temporary file tracking (`internal/multimodal/image.go`), and formats context (`internal/prompt/system.go`).
3. `internal/engine/driver.go` ensures Chrome is launched via `internal/cdp/launcher.go` and connected via WebSocket (`internal/cdp/ws.go`, `internal/cdp/client.go`).
4. `driver.go` selects model and reasoning depth in the Gemini Web UI if necessary, resets chat context if incremental mode is inactive (`IsNewChat: !isIncremental`), injects images and prompt text into the Quill editor, and submits the message.
5. In streaming mode, `server.go` filters raw ```` ```tool_call ```` content from user deltas, streams reasoning traces into `delta.reasoning_content` and content into `delta.content`, sends structured `tool_calls` chunks if detected, and finishes with `finish_reason: "tool_calls"` or `"stop"`.
6. In non-streaming mode, `internal/parser/toolcall.go` parses and strips technical tool call blocks from `resp.Content`, populates `message.tool_calls`, and sets `"finish_reason": "tool_calls"` (or `"stop"`).
7. Temporary image files are deleted on completion.
