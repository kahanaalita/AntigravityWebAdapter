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
  3. High-tolerance tool call AST parser (65+ syntax recovery rules).
  4. Universal proxy autodetection (<15ms via `net.DialTimeout`).
  5. Anti-detect Chrome launch (`--disable-blink-features=AutomationControlled`).
  6. Automated UI handling: Anti-Canvas dismissal, auto-acceptance of upload terms, and session reuse.

## 2. Directory Layout

```
AntigravityWebAdapter/
├── cmd/
│   └── gemini-gateway/
│       └── main.go              # Entrypoint, CLI flag parsing (--mcp, --port, --debug)
├── internal/
│   ├── cdp/
│   │   ├── client.go            # CDP client multiplexer, request/event dispatch
│   │   ├── launcher.go          # Anti-detect Chrome launcher, proxy autodetect
│   │   └── ws.go                # RFC 6455 WebSocket client (zero-dep)
│   ├── engine/
│   │   ├── driver.go            # Browser driver, tab management, session routing
│   │   ├── input.go             # Quill editor input engine, clipboard injection
│   │   └── models.go            # Model switcher, CDK overlay handler
│   ├── mcp/
│   │   └── server.go            # Native stdio MCP server implementation
│   ├── models/
│   │   └── catalog.go           # Model definitions and reasoning effort mapping
│   ├── multimodal/
│   │   └── images.go            # Multimodal image extraction, temporary file manager
│   ├── parser/
│   │   └── toolcall.go          # AST JSON tool-call & reasoning extractor
│   ├── prompt/
│   │   └── builder.go           # Prompt & context builder
│   └── server/
│       ├── handlers.go          # HTTP & SSE endpoints (/v1/chat/completions, /login)
│       └── sse.go               # Server-Sent Events streaming helper
├── tests/
│   ├── parser_test.go           # Tool-call parser and AST tests
│   └── router_test.go           # Session hash & model routing tests
├── docs/
│   └── design.md                # Design architecture specification
├── go.mod
├── README.md                    # Russian documentation & installation guide
└── README.en.md                 # English documentation & installation guide
```

## 3. Data Flow

### A. MCP Stdio Mode (`--mcp`)
1. Antigravity launches `gemini-gateway --mcp` via stdio.
2. The MCP server registers tools for Hydrogen 4 (`hydrogen_low`, `hydrogen_middle`, `hydrogen_high`) and Argon 4 (`argon_low`, `argon_medium`, `argon_high`).
3. When Antigravity invokes a tool call, `internal/mcp/server.go` dispatches the prompt and reasoning level to `internal/engine/driver.go`.
4. The driver navigates Gemini Web in an automated Chrome session, enters the prompt into the Quill editor, selects model/effort, and captures the streaming response.
5. The result (including reasoning trace and generated text) is returned to Antigravity as an MCP text response.

### B. HTTP / OpenAI SSE Gateway Mode (`--port 5690`)
1. Client sends `POST /v1/chat/completions` with streaming enabled.
2. `server/handlers.go` resolves target model and effort (`models/catalog.go`), extracts multimodal images (`multimodal/images.go`), and formats context (`prompt/builder.go`).
3. `engine/driver.go` acquires or launches an anti-detect Chrome session via `cdp/launcher.go` and `cdp/client.go`.
4. `engine/models.go` configures model and reasoning depth in the Gemini Web UI if changed.
5. `engine/input.go` injects images and prompt text into Quill editor and clicks send.
6. `engine/driver.go` monitors response generation, parses reasoning thoughts and content chunks, and streams SSE deltas back to the client.
7. `parser/toolcall.go` detects and repairs tool calls if emitted.
