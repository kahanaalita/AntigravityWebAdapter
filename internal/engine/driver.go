package engine

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/cdp"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/parser"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/prompt"
)

const GeminiAppURL = "https://gemini.google.com/app"

// DriverConfig holds parameters for running the Chrome automation engine.
type DriverConfig struct {
	BaseDir     string
	ChromePath  string
	Headless    bool
	ProxyServer string
}

// AskRequest specifies parameters for a completion request.
type AskRequest struct {
	Question  string
	Model     string
	Effort    string
	Files     []string
	Tools     []prompt.ToolDefinition
	IsNewChat bool
}

// AskResponse contains completion text, reasoning, and tool calls.
type AskResponse struct {
	Content   string            `json:"content"`
	Reasoning string            `json:"reasoning"`
	ToolCalls []parser.ToolCall `json:"tool_calls,omitempty"`
}

// Driver orchestrates the Chrome browser and CDP communication.
type Driver struct {
	cfg     DriverConfig
	chrome  *cdp.ChromeInstance
	client  *cdp.Client
	pageID  string
	mu      sync.Mutex
	initMu  sync.Mutex
	ready   bool
}

// NewDriver creates an automation driver instance.
func NewDriver(cfg DriverConfig) *Driver {
	return &Driver{
		cfg: cfg,
	}
}

// EnsureReady launches Chrome and attaches to the Gemini tab if not already connected.
func (d *Driver) EnsureReady() error {
	d.initMu.Lock()
	defer d.initMu.Unlock()

	if d.ready && d.client != nil {
		return nil
	}

	launcherCfg := cdp.LauncherConfig{
		ChromePath:  d.cfg.ChromePath,
		BaseDir:     d.cfg.BaseDir,
		Headless:    d.cfg.Headless,
		ProxyServer: d.cfg.ProxyServer,
	}

	chrome, err := cdp.LaunchChrome(launcherCfg)
	if err != nil {
		return fmt.Errorf("failed to launch chrome: %w", err)
	}
	d.chrome = chrome

	targets, err := chrome.FetchTargets()
	if err != nil {
		return fmt.Errorf("failed to list chrome targets: %w", err)
	}

	var target *cdp.ChromeTarget
	for _, t := range targets {
		if t.Type == "page" && strings.Contains(t.URL, "gemini.google.com") {
			target = &t
			break
		}
	}

	if target == nil {
		// Create new tab with Gemini URL
		t, err := chrome.NewTarget(GeminiAppURL)
		if err != nil {
			return fmt.Errorf("failed to open gemini tab: %w", err)
		}
		target = t
	}

	client, err := cdp.NewClient(target.WebSocketDebuggerURL)
	if err != nil {
		return fmt.Errorf("failed to connect to gemini tab via cdp: %w", err)
	}

	d.client = client
	d.pageID = target.ID
	d.ready = true

	// Check if page needs navigation
	val, err := client.Evaluate("window.location.href", false)
	if err == nil {
		if currentURL, ok := val.(string); ok && !strings.Contains(currentURL, "gemini.google.com") {
			_ = client.Navigate(GeminiAppURL)
		}
	}

	return nil
}

// CheckLoginStatus verifies if user is authenticated in Gemini.
func (d *Driver) CheckLoginStatus() (bool, error) {
	if err := d.EnsureReady(); err != nil {
		return false, err
	}

	// 1. Fast path: check auth cookies via Storage.getCookies
	rawCookies, err := d.client.Call("Storage.getCookies", map[string]interface{}{})
	if err == nil {
		var cookieResp struct {
			Cookies []struct {
				Name string `json:"name"`
			} `json:"cookies"`
		}
		if json.Unmarshal(rawCookies, &cookieResp) == nil {
			for _, c := range cookieResp.Cookies {
				if c.Name == "__Secure-1PSID" || c.Name == "SID" || c.Name == "__Secure-3PSID" || c.Name == "__Secure-1PSIDTS" {
					return true, nil
				}
			}
		}
	}

	// 2. DOM Check
	res, err := d.client.Evaluate(`(() => {
		const u = location.href;
		const isAccounts = u.includes('accounts.google.com');
		const editor = document.querySelector('rich-textarea div.ql-editor, rich-textarea, [contenteditable]');
		const switcher = document.querySelector('button.input-area-switch, bard-mode-switcher button, [data-test-id="bard-mode-menu-button"]');
		const avatar = document.querySelector('img[alt*="Google Account"], [aria-label*="Google Account"], img[alt*="Аккаунт Google"], [aria-label*="Аккаунт Google"], bard-avatar, a[href*="SignOutOptions"]');
		const hasSignIn = document.querySelector('a[href*="ServiceLogin"], [aria-label*="Войти"], [aria-label*="Sign in"]');
		const isApp = u.includes('gemini.google.com/app');
		return !isAccounts && !hasSignIn && (!!avatar || !!editor || !!switcher || isApp);
	})()`, false)
	if err != nil {
		return false, err
	}

	b, _ := res.(bool)
	return b, nil
}

// SelectModelAndReasoning sets the desired model and thinking/reasoning effort level.
func (d *Driver) SelectModelAndReasoning(targetModel, targetEffort string) error {
	script := fmt.Sprintf(`((targetModel, targetEffort) => {
		const EFFORT_SYNONYMS = {
			low: ['низкий', 'быстрый', 'low', 'fast', 'fast and efficient'],
			medium: ['средний', 'умеренный', 'medium', 'balanced'],
			high: ['высокий', 'глубокий', 'high', 'deep']
		};

		const MODEL_ALIASES = {
			'4 Hydrogen': ['4 hydrogen', 'hydrogen', 'повседневные дела', 'новинка'],
			'3.5 Flash-Lite': ['3.5 flash-lite', 'flash-lite', 'быстрые ответы'],
			'3.1 Pro': ['3.1 pro', 'исследования', 'pro'],
			'4 Argon': ['4 argon', 'argon', 'код и анализ']
		};

		const aliases = MODEL_ALIASES[targetModel] || [targetModel.toLowerCase()];
		const synonyms = EFFORT_SYNONYMS[targetEffort] || [];

		// Check current status
		const modeBtn = document.querySelector('button.input-area-switch, bard-mode-switcher button, [data-test-id="bard-mode-menu-button"]');
		const curText = modeBtn ? (modeBtn.innerText || '').toLowerCase() : '';
		const modelMatches = aliases.some(a => curText.includes(a));

		if (modelMatches && !targetEffort) {
			return { ok: true, matched: true };
		}

		// Open menu
		if (modeBtn) modeBtn.click();
		return { ok: true, opened: true };
	})(%q, %q)`, targetModel, targetEffort)

	_, err := d.client.Evaluate(script, false)
	if err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)

	// Click model if not matched
	clickScript := fmt.Sprintf(`((targetModel, targetEffort) => {
		const MODEL_ALIASES = {
			'4 Hydrogen': ['4 hydrogen', 'hydrogen', 'повседневные дела', 'новинка'],
			'3.5 Flash-Lite': ['3.5 flash-lite', 'flash-lite', 'быстрые ответы'],
			'3.1 Pro': ['3.1 pro', 'исследования', 'pro'],
			'4 Argon': ['4 argon', 'argon', 'код и анализ']
		};
		const EFFORT_SYNONYMS = {
			low: ['низкий', 'быстрый', 'low', 'fast', 'fast and efficient'],
			medium: ['средний', 'умеренный', 'medium', 'balanced'],
			high: ['высокий', 'глубокий', 'high', 'deep']
		};

		const aliases = MODEL_ALIASES[targetModel] || [targetModel.toLowerCase()];
		const synonyms = EFFORT_SYNONYMS[targetEffort] || [];

		const items = Array.from(document.querySelectorAll('gem-menu-item, [role="menuitem"], .mat-mdc-menu-item'));
		for (const item of items) {
			const txt = (item.innerText || '').toLowerCase();
			if (aliases.some(a => txt.includes(a))) {
				item.click();
				return true;
			}
		}
		for (const item of items) {
			const txt = (item.innerText || '').toLowerCase();
			if (synonyms.some(s => txt.includes(s))) {
				item.click();
				return true;
			}
		}
		return false;
	})(%q, %q)`, targetModel, targetEffort)

	_, _ = d.client.Evaluate(clickScript, false)
	time.Sleep(400 * time.Millisecond)

	// Press Escape to dismiss overlay
	_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
		"type": "rawKeyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
	})
	_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
		"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
	})

	return nil
}

// AutoDismissOverlays removes Canvas and auto-accepts file upload consent modals.
func (d *Driver) AutoDismissOverlays() {
	script := `(() => {
		const canvas = document.querySelector('.canvas-container, [data-test-id*="canvas"], side-panel-canvas');
		if (canvas) {
			const closeBtn = canvas.querySelector('button[aria-label*="Закрыть"], button[aria-label*="Close"], button.close-button');
			if (closeBtn) closeBtn.click();
		}
		const consent = document.querySelector('.consent-dialog-container');
		if (consent) {
			const btns = Array.from(consent.querySelectorAll('button, [role=button], gem-button'));
			const accept = btns.find(b => (b.innerText || '').includes('Принять') || (b.innerText || '').includes('Accept'));
			if (accept) accept.click();
		}
	})()`
	_, _ = d.client.Evaluate(script, false)
}

// UploadFiles attaches local files to the Gemini editor via synthetic Clipboard and DragDrop events.
func (d *Driver) UploadFiles(files []string) error {
	for _, fp := range files {
		data, err := os.ReadFile(fp)
		if err != nil {
			log.Printf("[driver] Failed to read file %s: %v", fp, err)
			continue
		}

		b64 := base64.StdEncoding.EncodeToString(data)
		name := filepath.Base(fp)
		ext := strings.ToLower(filepath.Ext(fp))
		mime := "application/octet-stream"
		switch ext {
		case ".png":
			mime = "image/png"
		case ".jpg", ".jpeg":
			mime = "image/jpeg"
		case ".webp":
			mime = "image/webp"
		}

		script := fmt.Sprintf(`((b64, mime, name) => {
			const bin = atob(b64);
			const u8 = new Uint8Array(bin.length);
			for (let i = 0; i < bin.length; i++) u8[i] = bin.charCodeAt(i);
			const file = new File([u8], name, { type: mime });
			const dt = new DataTransfer();
			dt.items.add(file);

			const ed = document.querySelector('rich-textarea div.ql-editor, [contenteditable="true"]');
			if (ed) {
				ed.focus();
				const pasteEvt = new ClipboardEvent('paste', {
					bubbles: true,
					cancelable: true,
					clipboardData: dt
				});
				ed.dispatchEvent(pasteEvt);
				const dropEvt = new DragEvent('drop', {
					bubbles: true,
					cancelable: true,
					dataTransfer: dt
				});
				ed.dispatchEvent(dropEvt);
			}
		})(%q, %q, %q)`, b64, mime, name)

		if _, err := d.client.Evaluate(script, false); err != nil {
			log.Printf("[driver] Failed to inject file %s: %v", fp, err)
		} else {
			log.Printf("[driver] File attached via synthetic event: %s", name)
		}
		time.Sleep(600 * time.Millisecond)
	}

	return nil
}

// InsertText types or injects prompt text into the Quill editor.
func (d *Driver) InsertText(text string) error {
	// Focus editor
	focusScript := `(() => {
		const ed = document.querySelector('rich-textarea div.ql-editor, [contenteditable="true"]');
		if (ed) {
			ed.focus();
			return true;
		}
		return false;
	})()`

	if _, err := d.client.Evaluate(focusScript, false); err != nil {
		return err
	}

	// For short messages, level 1 DOM insertion + input event dispatch
	if len(text) < 4000 {
		insertScript := fmt.Sprintf(`((text) => {
			const ed = document.querySelector('rich-textarea div.ql-editor, [contenteditable="true"]');
			if (!ed) return false;
			while (ed.firstChild) ed.removeChild(ed.firstChild);
			const p = document.createElement('p');
			p.textContent = text;
			ed.appendChild(p);
			ed.dispatchEvent(new Event('input', { bubbles: true, cancelable: true }));
			return true;
		})(%q)`, text)
		_, err := d.client.Evaluate(insertScript, false)
		return err
	}

	// For long messages (> 4000 chars), chunk via Input.insertText
	const chunkSize = 4000
	for i := 0; i < len(text); i += chunkSize {
		end := i + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunk := text[i:end]
		_, err := d.client.Call("Input.insertText", map[string]interface{}{
			"text": chunk,
		})
		if err != nil {
			return err
		}
		time.Sleep(40 * time.Millisecond)
	}

	return nil
}

// SubmitMessage clicks the send button in the interface.
func (d *Driver) SubmitMessage() error {
	clickScript := `(() => {
		const sendBtn = document.querySelector('button[aria-label*="Send"], button[aria-label*="Отправить"], button.send-button, [data-test-id="send-button"]');
		if (sendBtn && !sendBtn.disabled) {
			sendBtn.click();
			return true;
		}
		return false;
	})()`

	val, err := d.client.Evaluate(clickScript, false)
	if err != nil {
		return err
	}

	if clicked, _ := val.(bool); !clicked {
		// Fallback to Enter key
		_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "rawKeyDown", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13,
		})
		_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyUp", "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13,
		})
	}

	return nil
}

// Ask sends a prompt, streams response tokens via callback, and returns the finished response.
func (d *Driver) Ask(ctx context.Context, req AskRequest, onDelta func(reasoningDelta, contentDelta string)) (*AskResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.EnsureReady(); err != nil {
		return nil, err
	}

	// Dismiss any initial overlays
	d.AutoDismissOverlays()

	// Select Model and Reasoning Effort
	if req.Model != "" {
		_ = d.SelectModelAndReasoning(req.Model, req.Effort)
	}

	// Upload files if provided
	if len(req.Files) > 0 {
		_ = d.UploadFiles(req.Files)
		time.Sleep(500 * time.Millisecond)
	}

	// Insert Text
	if err := d.InsertText(req.Question); err != nil {
		return nil, fmt.Errorf("failed to insert question into editor: %w", err)
	}
	time.Sleep(300 * time.Millisecond)

	// Count responses prior to submit
	countVal, _ := d.client.Evaluate("document.querySelectorAll('.model-response-text, message-content, [data-test-id*=\"model-response\"]').length", false)
	initialCount := 0
	if f, ok := countVal.(float64); ok {
		initialCount = int(f)
	}

	// Submit
	if err := d.SubmitMessage(); err != nil {
		return nil, fmt.Errorf("failed to submit prompt: %w", err)
	}

	// Polling loop
	targetIndex := initialCount
	var fullThoughts, fullContent string
	lastThoughtsLen := 0
	lastContentLen := 0
	stableChecks := 0
	generationStarted := false

	extractScript := fmt.Sprintf(`((targetIndex) => {
		const responses = document.querySelectorAll('.model-response-text, message-content, [data-test-id*="model-response"]');
		const idx = targetIndex < responses.length ? targetIndex : responses.length - 1;
		const latest = responses[idx];
		if (!latest) return null;

		const thoughtsEl = latest.querySelector('model-thoughts, .model-thoughts, [data-test-id*="thoughts"], thinking-overlay');
		const thoughtsText = thoughtsEl ? (thoughtsEl.innerText || '').trim() : '';

		let contentEl = latest.querySelector('.markdown, message-content, .model-response-text') || latest;
		const rawContent = (contentEl.innerText || '').trim();

		const stopBtn = document.querySelector('button[aria-label*="Остановить"], button[aria-label*="Stop"], button.stop-button');
		const isGenerating = !!stopBtn;

		return {
			thoughts: thoughtsText,
			content: rawContent,
			isGenerating: isGenerating
		};
	})(%d)`, targetIndex)

	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			d.AutoDismissOverlays()

			res, err := d.client.Evaluate(extractScript, false)
			if err != nil || res == nil {
				continue
			}

			m, ok := res.(map[string]interface{})
			if !ok {
				continue
			}

			thoughts, _ := m["thoughts"].(string)
			content, _ := m["content"].(string)
			isGenerating, _ := m["isGenerating"].(bool)

			if isGenerating || len(content) > 0 || len(thoughts) > 0 {
				generationStarted = true
			}

			// Stream reasoning delta
			var rDelta, cDelta string
			if len(thoughts) > lastThoughtsLen {
				rDelta = thoughts[lastThoughtsLen:]
				lastThoughtsLen = len(thoughts)
				fullThoughts = thoughts
			}

			// Stream content delta
			if len(content) > lastContentLen {
				cDelta = content[lastContentLen:]
				lastContentLen = len(content)
				fullContent = content
			}

			if (rDelta != "" || cDelta != "") && onDelta != nil {
				onDelta(rDelta, cDelta)
			}

			// Check completion stability
			if generationStarted && !isGenerating && (len(content) > 0 || len(thoughts) > 0) {
				if len(content) == lastContentLen && len(thoughts) == lastThoughtsLen {
					stableChecks++
					if stableChecks >= 5 { // 5 * 150ms = 750ms stable
						openCalls := strings.Count(fullContent, "```tool_call")
						closeCalls := strings.Count(fullContent, "```")
						if openCalls > 0 && closeCalls%2 != 0 {
							// Unclosed code block, wait extra
							time.Sleep(1 * time.Second)
						} else {
							goto done
						}
					}
				} else {
					stableChecks = 0
				}
			} else {
				stableChecks = 0
			}
		}
	}

done:
	// Parse tool calls if any
	var toolCalls []parser.ToolCall
	if len(req.Tools) > 0 && strings.Contains(fullContent, "```") {
		var toolSchemas []parser.ToolSchema
		for _, t := range req.Tools {
			toolSchemas = append(toolSchemas, parser.ToolSchema{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			})
		}
		toolCalls = parser.ParseToolCalls(fullContent, toolSchemas)
	}

	return &AskResponse{
		Content:   fullContent,
		Reasoning: fullThoughts,
		ToolCalls: toolCalls,
	}, nil
}
