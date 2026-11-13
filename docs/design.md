# dsh-gemini-web-adapter-go: Design Specification

> Standalone, zero-dependency Go implementation of the DeepSeek Harness (DSH) adapter for Google Gemini Web (Google AI Pro tier).

## 1. Objectives

- **Complete Go Rewrite:** Replace `gemini-gateway.js` and `gemini-driver.js` with a single high-performance compiled binary `gemini-gateway`.
- **Zero External Dependencies:** Use standard Go libraries (`net/http`, `net`, `encoding/json`, `os/exec`) and an in-tree RFC 6455 WebSocket client.
- **Full Parity with Node.js version:**
  1. Full model matrix (Gemini 4 Hydrogen, 3.5 Flash-Lite, 3.1 Pro, 4 Argon + reasoning levels `low`, `medium`, `high`).
  2. OpenAI SSE `/v1/chat/completions` streaming with separated thoughts (`delta.reasoning_content`) and content (`delta.content`).
  3. High-tolerance tool call AST parser (65+ syntax recovery rules).
  4. Multimodal image upload (base64 image extraction, synthetic clipboard paste & drag-drop).
  5. Universal proxy probing (<15ms via `net.DialTimeout`).
  6. Anti-detect Chrome launch (`--disable-blink-features=AutomationControlled`).
  7. Anti-Canvas auto-dismissal and auto-acceptance of upload terms.

## 2. Directory Layout

```
dsh-gemini-web-adapter-go/
├── cmd/
│   └── gemini-gateway/
│       └── main.go              # Entrypoint, CLI flag parsing, signal handling
├── internal/
│   ├── cdp/
│   │   ├── client.go            # CDP client multiplexer, request/event dispatch
│   │   ├── launcher.go          # Anti-detect Chrome launcher, proxy autodetect
│   │   └── ws.go                # RFC 6455 WebSocket client (zero-dep)
│   ├── engine/
│   │   ├── driver.go            # Browser driver, tab management, session routing
│   │   ├── input.go             # Quill editor input engine, clipboard injection
│   │   └── models.go            # Model switcher, CDK overlay handler
│   ├── multimodal/
│   │   └── images.go            # OpenAI image extraction, tmp file management
│   ├── parser/
│   │   └── toolcall.go          # AST JSON tool-call & reasoning extractor
│   ├── prompt/
│   │   └── builder.go           # Anti-Canvas prompt & context builder
│   └── server/
│       ├── handlers.go          # HTTP & SSE endpoints (/v1/chat/completions, /login)
│       └── sse.go               # Server-Sent Events streaming helper
├── tests/
│   ├── parser_test.go           # 65+ tool-call parser tests
│   └── router_test.go           # Session hash & model routing tests
├── dsh-bundle/
│   ├── package.json             # Cordis bundle definition
│   └── index.js                 # Minimal supervisor spawning gemini-gateway binary
├── go.mod
└── README.md
```

## 3. Data Flow

1. DSH client sends `POST /v1/chat/completions` with streaming.
2. `server/handlers.go` resolves target model and effort (`models.go`), extracts multimodal images (`multimodal/images.go`), and formats context (`prompt/builder.go`).
3. `engine/driver.go` acquires or launches an anti-detect Chrome session via `cdp/launcher.go` and `cdp/client.go`.
4. `engine/models.go` configures model and reasoning depth in the Gemini Web UI if changed.
5. `engine/input.go` injects images and prompt text into Quill editor, dispatches submit.
6. `engine/driver.go` monitors response generation, parses reasoning thoughts and content chunks, and streams SSE deltas back to DSH.
7. `parser/toolcall.go` detects and repairs tool calls if emitted.
