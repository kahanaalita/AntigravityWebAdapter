# DSH Gemini Web Adapter (Go Edition) 🚀

High-performance standalone gateway written in **Go** integrating Google Gemini Web (Google AI Pro: **Gemini 4 Hydrogen**, **Gemini 3.5 Flash-Lite**, **Gemini 3.1 Pro**, **Gemini 4 Argon**) with **DeepSeek Harness (DSH)** and **Google Antigravity**.

Full native replacement for the Node.js bridge in pure Go: **0 external dependencies**, **15–20 MB RAM footprint**, instantaneous **5 ms startup**, and a single compiled binary.

---

## 🌟 Key Features

- ⚡ **Zero-Dependency Architecture:** Built solely with Go standard library and in-tree RFC 6455 WebSocket client.
- 🧠 **Thinking & Multimodal:** Multimodal file/image uploads via synthetic ClipboardEvent/DragEvent with streaming `reasoning_content` deltas.
- 🛠️ **Tolerant Tool Call Parser:** 5-level progressive JSON repair (strips comments, unescapes newlines, repairs unquoted keys, fixes trailing commas).
- 🔌 **Dual Operation Mode:**
  - **OpenAI Compatible HTTP Gateway (`--port 5690`):** Seamless drop-in for DeepSeek Harness.
  - **Antigravity Native MCP Server (`--mcp`):** Standard stdio Model Context Protocol server exposing `ask_gemini_web` and `gemini_web_status`.
- 🛡️ **Anti-Canvas & Anti-Sandbox:** Auto-dismisses Gemini Canvas and Workspace overlays; auto-approves upload consent modals.
- 🌐 **Auto-Proxy Probing:** Detects local SOCKS5/HTTP proxies on ports 10808, 7890, 1080, 10809.

---

## 🚀 Quickstart

```bash
git clone https://github.com/kahanaalita/dsh-gemini-web-adapter-go.git
cd dsh-gemini-web-adapter-go
go build -o bin/gemini-gateway ./cmd/gemini-gateway

# Run HTTP Server (DSH Mode)
./bin/gemini-gateway --port 5690

# Or configure as Antigravity MCP Server
# Command: /path/to/bin/gemini-gateway
# Args: ["--mcp"]
```

## 📄 License
MIT License © 2026 kahanaalita
