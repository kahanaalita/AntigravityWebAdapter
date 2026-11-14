# AntigravityWebAdapter (Go Edition) 🚀

> **High-performance standalone Go adapter and MCP server seamlessly integrating Google Antigravity with Google AI Pro Web** (**Gemini 4 Hydrogen**, **Gemini 4 Argon**, **Gemini 3.1 Pro**, and **Gemini 3.5 Flash-Lite**).

`AntigravityWebAdapter` bridges Google Antigravity with `gemini.google.com/app`, unlocking the full reasoning and multimodal power of Google AI Pro tier without minute-by-minute API costs (Vertex AI / Generative Language API).

Engineered in pure Go: **zero external runtime dependencies**, instant cold start in **<10 ms**, memory footprint of only **15–20 MB**, and a single static binary.

> 💡 **Using DeepSeek Harness (DSH)?** Detailed setup instructions for both native MCP and HTTP/SSE modes are available in [DEEPSEEK_HARNESS_SETUP.md](DEEPSEEK_HARNESS_SETUP.md).

---

## 🌟 Key Features

* ⚡ **Zero-Dependency Architecture:** Built exclusively with the Go standard library and an in-tree RFC 6455 WebSocket client. No Node.js, Puppeteer, or Playwright runtimes required.
* 📦 **Native Codebase Bundler (`internal/bundler`):** Pack entire directory trees into compact context bundles with `.gitignore` support, binary filtering, and token counting (`-bundle` flag, `project_dir` parameter).
* 📎 **CDP File Upload (`DOM.setFileInputFiles`):** Native CDP file upload directly to the file input with automated clipboard fallback.
* 🗂️ **Session Management & SPA Navigation:** Instant listing and seamless switching between existing chat sessions without page reloads via HTTP (`/v1/sessions`) and MCP (`gemini_web_sessions`).
* 📸 **CDP Screenshot Capture (`Page.captureScreenshot`):** High-precision page or DOM-element screenshots via HTTP (`GET /v1/screenshot`) and MCP (`gemini_web_screenshot`).
* 🔌 **Extended Model Context Protocol (MCP):** Full stdio JSON-RPC 2.0 server for Antigravity (`--mcp`). Exposes `ask_gemini_web`, `gemini_web_sessions`, `gemini_web_screenshot`, `gemini_web_status`, and granular atomic tools.
* 🌐 **Dual Operation Modes:**
  * **MCP Stdio Server (`--mcp`):** Direct drop-in configuration for Antigravity (`mcp_config.json`).
  * **OpenAI-Compatible HTTP/SSE Server (`--port 5690`):** Local REST/SSE gateway (`/v1/chat/completions`, `/v1/sessions`, `/v1/screenshot`) for third-party scripts, CLI tools, and web apps.
* 🧠 **Real-Time Thinking Trace Streaming:** Model reasoning thoughts are streamed cleanly and separated into collapsible `<details><summary>Thinking Process</summary>...</details>` blocks.
* 🖼️ **Multimodal Vision & File Attachments:** Synthetic `ClipboardEvent('paste')` and `DragEvent('drop')` injection into the Gemini Quill editor supporting screenshots, Base64 data URIs, and local file paths.
* 🛠️ **Fault-Tolerant Tool-Call AST Parser:** 5-tier syntax recovery heuristic handling 65+ LLM JSON syntax errors (unescaped backslashes, trailing commas, argument aliases).
* 🛡️ **Anti-Canvas & Anti-Detect:** Automatic dismissal of unwanted Canvas popups, automated consent acceptance, and Chrome anti-detection flags (`--disable-blink-features=AutomationControlled`).
* 🔄 **Zero-Config Auto-Proxy Probing:** Sub-15ms socket scan across standard local proxy ports (SOCKS5/HTTP: `10808`, `7890`, `1080`, `10809`). Falls back to system network stack automatically.

---

## 🚀 Quick Start

### 1. Build the Binary
```bash
git clone https://github.com/kahanaalita/AntigravityWebAdapter.git
cd AntigravityWebAdapter
go build -o bin/antigravity-web-adapter ./cmd/gemini-gateway
```

### 2. Configure in Antigravity (`mcp_config.json`)
```json
{
  "mcpServers": {
    "antigravity-web-adapter": {
      "command": "/Users/username/path/to/AntigravityWebAdapter/bin/antigravity-web-adapter",
      "args": [
        "--mcp",
        "--base",
        "/Users/username/.config/antigravity-web-adapter/runtime"
      ],
      "env": {
        "DEFAULT_HYDROGEN_EFFORT": "middle",
        "DEFAULT_ARGON_EFFORT": "medium"
      },
      "timeoutSeconds": 600
    }
  }
}
```

### 3. Usage via Slash Commands in Antigravity Chat
```text
/a-argon-high Review this architecture and identify bottlenecks
/a-argon-medium @src/server.go Refactor HTTP handler
/a-hydrogen-high @diagram.png Explain microservice communication flow
/a-hydrogen-low Summarize key concepts briefly
```

### 4. Running Unit Tests
```bash
go test -v -count=1 ./...
```

---

## 📄 License

Distributed under the MIT License. See [LICENSE](LICENSE) for details.
