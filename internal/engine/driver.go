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
	"sync/atomic"
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
	Question       string
	Model          string
	Effort         string
	Files          []string
	Tools          []prompt.ToolDefinition
	IsNewChat      bool
	ConversationID string
}

// AskResponse contains completion text, reasoning, and tool calls.
type AskResponse struct {
	Content        string            `json:"content"`
	Reasoning      string            `json:"reasoning"`
	ConversationID string            `json:"conversation_id,omitempty"`
	ToolCalls      []parser.ToolCall `json:"tool_calls,omitempty"`
}

// ChatSession represents an existing Gemini conversation in the sidebar.
type ChatSession struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// ScreenshotOptions configures capture parameters.
type ScreenshotOptions struct {
	Selector string `json:"selector,omitempty"`
	FullPage bool   `json:"full_page,omitempty"`
	Format   string `json:"format,omitempty"` // "png", "jpeg", "webp"
	Quality  int    `json:"quality,omitempty"`
}

// Driver orchestrates the Chrome browser and CDP communication.
type Driver struct {
	cfg           DriverConfig
	chrome        *cdp.ChromeInstance
	client        *cdp.Client
	pageID        string
	mu            sync.Mutex
	initMu        sync.Mutex
	ready         bool
	generating    atomic.Bool
	currentModel  string
	currentEffort string
}

// NewDriver creates an automation driver instance.
func NewDriver(cfg DriverConfig) *Driver {
	return &Driver{
		cfg: cfg,
	}
}

// IsGenerating returns true if the driver is currently generating a response.
func (d *Driver) IsGenerating() bool {
	return d.generating.Load()
}

// Close closes the CDP client WebSocket/connection cleanly.
func (d *Driver) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.ready = false
	if d.client != nil {
		err := d.client.Close()
		d.client = nil
		return err
	}
	return nil
}

// EnsureReady launches Chrome and attaches to the Gemini tab if not already connected.
func (d *Driver) EnsureReady() error {
	d.initMu.Lock()
	defer d.initMu.Unlock()

	if d.ready && d.client != nil {
		if d.client.IsAlive() {
			return nil
		}
		// Reset dead connection
		d.ready = false
		_ = d.client.Close()
		d.client = nil
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

	// Wait for SPA DOM and Quill editor readiness
	if err := d.WaitForPageReady(15 * time.Second); err != nil {
		val, uErr := d.client.Evaluate("window.location.href", false)
		if uErr == nil {
			if curURL, ok := val.(string); ok {
				if strings.Contains(curURL, "accounts.google.com") || strings.Contains(curURL, "ServiceLogin") || strings.Contains(curURL, "challenge") {
					return fmt.Errorf("authentication required: page redirected to Google accounts login")
				}
			}
		}
		return fmt.Errorf("gemini page not ready: %w", err)
	}

	return nil
}

// WaitForPageReady polls until document.readyState is complete and Quill editor is mounted.
func (d *Driver) WaitForPageReady(timeout time.Duration) error {
	checkScript := `(() => {
		const isReady = document.readyState === 'complete';
		const editor = document.querySelector('rich-textarea div.ql-editor, rich-textarea, [contenteditable="true"]');
		return isReady && !!editor;
	})()`

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		val, err := d.client.Evaluate(checkScript, false)
		if err == nil {
			if ok, _ := val.(bool); ok {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for Gemini page and editor ready")
}

// WaitForSelector polls until document.querySelector(selector) returns an element or timeout expires.
func (d *Driver) WaitForSelector(selector string, timeout time.Duration) error {
	script := fmt.Sprintf(`(() => {
		return !!document.querySelector(%q);
	})()`, selector)

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		val, err := d.client.Evaluate(script, false)
		if err == nil {
			if ok, _ := val.(bool); ok {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for selector %q", selector)
}

// ResetChatIfNecessary clicks the "New chat" button in Gemini UI if forced or message count is >= 25.
func (d *Driver) ResetChatIfNecessary(force bool) error {
	if !force {
		countVal, err := d.client.Evaluate("document.querySelectorAll('.model-response-text, message-content, [data-test-id*=\"model-response\"]').length", false)
		if err == nil {
			if f, ok := countVal.(float64); ok && int(f) < 25 {
				return nil
			}
		}
	}

	script := `(() => {
		const newChatBtn = document.querySelector('a[aria-label*="Новый чат"], a[aria-label*="New chat"], [data-test-id="new-chat-button"], button.new-chat-button, button[aria-label*="Новый чат"], button[aria-label*="New chat"], [aria-label*="New chat"], [aria-label*="Новый чат"]');
		if (newChatBtn) {
			newChatBtn.click();
			return true;
		}
		return false;
	})()`

	val, err := d.client.Evaluate(script, false)
	if err == nil {
		if clicked, _ := val.(bool); clicked {
			d.currentModel = ""
			d.currentEffort = ""
			return d.WaitForPageReady(10 * time.Second)
		}
	}

	// Fallback: navigate directly to GeminiAppURL
	_ = d.client.Navigate(GeminiAppURL)
	d.currentModel = ""
	d.currentEffort = ""
	return d.WaitForPageReady(10 * time.Second)
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
	// 1. Fast path: in-memory cache check (0ms delay)
	if d.currentModel == targetModel && d.currentEffort == targetEffort {
		return nil
	}

	// 2. Fast path: check current text on the button in DOM
	checkScript := fmt.Sprintf(`((targetModel, targetEffort) => {
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

		const modeBtn = document.querySelector('button.input-area-switch, bard-mode-switcher button, [data-test-id="bard-mode-menu-button"]');
		const curText = modeBtn ? (modeBtn.innerText || '').toLowerCase() : '';
		const curAria = modeBtn ? (modeBtn.getAttribute('aria-label') || '').toLowerCase() : '';
		const full = curText + ' ' + curAria;

		const aliases = MODEL_ALIASES[targetModel] || [targetModel.toLowerCase()];
		const modelMatched = aliases.some(a => full.includes(a));
		if (!modelMatched) return false;

		if (!targetEffort) return true;

		const synonyms = EFFORT_SYNONYMS[targetEffort] || [targetEffort.toLowerCase()];
		return synonyms.some(s => full.includes(s));
	})(%q, %q)`, targetModel, targetEffort)

	if res, err := d.client.Evaluate(checkScript, false); err == nil {
		if matched, _ := res.(bool); matched {
			d.currentModel = targetModel
			d.currentEffort = targetEffort
			return nil
		}
	}

	// Open menu
	openMenuScript := `(() => {
		const modeBtn = document.querySelector('button.input-area-switch, bard-mode-switcher button, [data-test-id="bard-mode-menu-button"]');
		if (modeBtn) {
			modeBtn.click();
			return true;
		}
		return false;
	})()`

	if _, err := d.client.Evaluate(openMenuScript, false); err != nil {
		return err
	}

	// Wait dynamically for dropdown menu to appear
	menuSelector := "gem-menu-item, [role=\"menuitem\"], .mat-mdc-menu-item"
	if err := d.WaitForSelector(menuSelector, 2*time.Second); err != nil {
		log.Printf("[driver] Warning: dropdown menu did not appear: %v", err)
	}

	// Step 1: Find and click target model item
	clickModelScript := fmt.Sprintf(`((targetModel) => {
		const MODEL_ALIASES = {
			'4 Hydrogen': ['4 hydrogen', 'hydrogen', 'повседневные дела', 'новинка'],
			'3.5 Flash-Lite': ['3.5 flash-lite', 'flash-lite', 'быстрые ответы'],
			'3.1 Pro': ['3.1 pro', 'исследования', 'pro'],
			'4 Argon': ['4 argon', 'argon', 'код и анализ']
		};

		const aliases = MODEL_ALIASES[targetModel] || [targetModel.toLowerCase()];
		const items = Array.from(document.querySelectorAll('gem-menu-item, [role="menuitem"], .mat-mdc-menu-item'));
		for (const item of items) {
			const txt = (item.innerText || '').toLowerCase();
			if (aliases.some(a => txt.includes(a))) {
				item.click();
				return true;
			}
		}
		return false;
	})(%q)`, targetModel)

	_, _ = d.client.Evaluate(clickModelScript, false)

	// Step 2: If targetEffort != "", find and click effort item
	if targetEffort != "" {
		clickEffortScript := fmt.Sprintf(`((targetEffort) => {
			const EFFORT_SYNONYMS = {
				low: ['низкий', 'быстрый', 'low', 'fast', 'fast and efficient'],
				medium: ['средний', 'умеренный', 'medium', 'balanced'],
				high: ['высокий', 'глубокий', 'high', 'deep']
			};

			const synonyms = EFFORT_SYNONYMS[targetEffort] || [targetEffort.toLowerCase()];
			const items = Array.from(document.querySelectorAll('gem-menu-item, [role="menuitem"], [role="radio"], [role="option"], .mat-mdc-menu-item, button'));
			for (const item of items) {
				const txt = (item.innerText || '').toLowerCase();
				const aria = (item.getAttribute('aria-label') || '').toLowerCase();
				const full = txt + ' ' + aria;
				if (synonyms.some(s => full.includes(s))) {
					item.click();
					return true;
				}
			}
			return false;
		})(%q)`, targetEffort)

		// Dynamic polling for effort options
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			val, err := d.client.Evaluate(clickEffortScript, false)
			if err == nil {
				if ok, _ := val.(bool); ok {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Dismiss menu cleanly via Escape key
	_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
		"type": "rawKeyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
	})
	_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
		"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
	})

	d.currentModel = targetModel
	d.currentEffort = targetEffort

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

// UploadFiles attaches images, documents, or codebase dumps to the Gemini prompt.
// It prioritizes native CDP DOM.setFileInputFiles via Chrome, with fallback to synthetic ClipboardEvent.
func (d *Driver) UploadFiles(files []string) error {
	if len(files) == 0 {
		return nil
	}

	var validPaths []string
	for _, fp := range files {
		abs, err := filepath.Abs(fp)
		if err != nil {
			abs = fp
		}
		if _, err := os.Stat(abs); err == nil {
			validPaths = append(validPaths, abs)
		} else {
			log.Printf("[driver] File not found or unreadable: %s (%v)", fp, err)
		}
	}
	if len(validPaths) == 0 {
		return nil
	}

	// Try native CDP file upload first
	if err := d.uploadFilesCDP(validPaths); err == nil {
		return nil
	} else {
		log.Printf("[driver] Native CDP upload failed (%v), falling back to clipboard paste...", err)
	}

	return d.uploadFilesFallback(validPaths)
}

func (d *Driver) uploadFilesCDP(paths []string) error {
	// Step 1: Ensure input[type=file] elements are mounted in the DOM.
	// Gemini Web lazy-mounts hidden file inputs when the upload button is clicked.
	ensureScript := `(() => {
		const inps = document.querySelectorAll('input[type=file]');
		if (inps.length > 0) return true;
		const btn = document.querySelector('button[aria-label*="Загрузка"], button[aria-label*="Upload"], button[aria-label*="Tools"], button[aria-label*="Инструменты"]');
		if (btn) {
			btn.click();
			return true;
		}
		return false;
	})()`
	_, _ = d.client.Evaluate(ensureScript, false)
	time.Sleep(300 * time.Millisecond)

	// Step 2: Separate media from documents/code
	var mediaPaths []string
	var docPaths []string
	for _, p := range paths {
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".mov", ".webm":
			mediaPaths = append(mediaPaths, p)
		default:
			docPaths = append(docPaths, p)
		}
	}

	uploadBatch := func(batch []string, isMedia bool) error {
		if len(batch) == 0 {
			return nil
		}

		selectScript := fmt.Sprintf(`(() => {
			const inputs = Array.from(document.querySelectorAll('input[type=file]'));
			if (inputs.length === 0) return null;
			if (%t) {
				return inputs.find(i => i.accept.includes('image')) || inputs[0];
			} else {
				return inputs.find(i => i.accept.includes('.txt') || !i.accept.includes('image')) || inputs[0];
			}
		})()`, isMedia)

		objMap, err := d.client.EvaluateRemoteObject(selectScript, false)
		if err != nil {
			return fmt.Errorf("failed to evaluate file input: %w", err)
		}
		objectId, ok := objMap["objectId"].(string)
		if !ok || objectId == "" {
			return fmt.Errorf("file input objectId missing in CDP response")
		}
		defer func() {
			_, _ = d.client.Call("Runtime.releaseObject", map[string]interface{}{"objectId": objectId})
		}()

		// Native CDP DOM.setFileInputFiles
		_, err = d.client.Call("DOM.setFileInputFiles", map[string]interface{}{
			"files":    batch,
			"objectId": objectId,
		})
		if err != nil {
			return fmt.Errorf("DOM.setFileInputFiles call failed: %w", err)
		}

		// Dispatch input and change events to trigger Angular change detection
		dispatchScript := fmt.Sprintf(`(() => {
			const inputs = Array.from(document.querySelectorAll('input[type=file]'));
			const inp = (%t) 
				? (inputs.find(i => i.accept.includes('image')) || inputs[0])
				: (inputs.find(i => i.accept.includes('.txt') || !i.accept.includes('image')) || inputs[0]);
			if (inp) {
				inp.dispatchEvent(new Event('input', { bubbles: true }));
				inp.dispatchEvent(new Event('change', { bubbles: true }));
			}
			const ed = document.querySelector('rich-textarea div.ql-editor, [contenteditable="true"]');
			if (ed) ed.focus();
			return true;
		})()`, isMedia)
		_, _ = d.client.Evaluate(dispatchScript, false)

		// Dismiss any open Angular Material menus via Escape
		_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "rawKeyDown", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
		})
		_, _ = d.client.Call("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyUp", "key": "Escape", "code": "Escape", "windowsVirtualKeyCode": 27,
		})

		return nil
	}

	if err := uploadBatch(mediaPaths, true); err != nil {
		return err
	}
	if err := uploadBatch(docPaths, false); err != nil {
		return err
	}

	// Step 3: Wait for all file preview chips to appear
	for i := 0; i < 20; i++ {
		time.Sleep(200 * time.Millisecond)
		checkScript := `(() => {
			const chips = document.querySelectorAll('uploader-file-preview, .file-preview-chip, gem-attachment');
			return chips.length;
		})()`
		val, _ := d.client.Evaluate(checkScript, false)
		if count, ok := val.(float64); ok && int(count) >= len(paths) {
			log.Printf("[driver] Attached %d file(s) via native CDP DOM.setFileInputFiles", len(paths))
			return nil
		}
	}

	return fmt.Errorf("file preview chips failed to appear in DOM within timeout (expected %d)", len(paths))
}

func (d *Driver) uploadFilesFallback(files []string) error {
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
		case ".md":
			mime = "text/markdown"
		case ".txt", ".go", ".py", ".js", ".ts", ".json", ".yaml", ".yml", ".sh", ".sql", ".rs", ".c", ".cpp", ".h":
			mime = "text/plain"
		case ".pdf":
			mime = "application/pdf"
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
				if (!pasteEvt.defaultPrevented) {
					const dropEvt = new DragEvent('drop', {
						bubbles: true,
						cancelable: true,
						dataTransfer: dt
					});
					ed.dispatchEvent(dropEvt);
				}
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

// InsertText types or injects prompt text into the Quill editor without breaking its internal model.
func (d *Driver) InsertText(text string) error {
	script := fmt.Sprintf(`((text) => {
		const ed = document.querySelector('rich-textarea div.ql-editor, [contenteditable="true"]');
		if (!ed) return false;
		ed.focus();

		// Clear via Selection API
		const sel = window.getSelection();
		sel.selectAllChildren(ed);

		// Insert via execCommand to keep Quill virtual DOM & submit button state synchronized
		const ok = document.execCommand('insertText', false, text);
		if (!ok) {
			while (ed.firstChild) ed.removeChild(ed.firstChild);
			const p = document.createElement('p');
			p.textContent = text;
			ed.appendChild(p);
			ed.dispatchEvent(new Event('input', { bubbles: true, cancelable: true }));
		}
		return true;
	})(%q)`, text)

	val, err := d.client.Evaluate(script, false)
	if err != nil {
		return err
	}
	if success, _ := val.(bool); !success {
		return fmt.Errorf("quill editor element not found in DOM")
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

// ComputeSafeDelta returns the new delta string between prev and curr safely without corrupting UTF-8 runes.
func ComputeSafeDelta(prev, curr string) string {
	return computeSafeDelta(prev, curr)
}

func computeSafeDelta(prev, curr string) string {
	if curr == prev || len(curr) == 0 {
		return ""
	}
	if strings.HasPrefix(curr, prev) {
		return curr[len(prev):]
	}

	// Compare by runes: find longest common prefix
	prevRunes := []rune(prev)
	currRunes := []rune(curr)

	commonPrefixLen := 0
	for commonPrefixLen < len(prevRunes) && commonPrefixLen < len(currRunes) {
		if prevRunes[commonPrefixLen] != currRunes[commonPrefixLen] {
			break
		}
		commonPrefixLen++
	}

	if commonPrefixLen >= len(currRunes) {
		return ""
	}

	return string(currRunes[commonPrefixLen:])
}

// Ask sends a prompt, streams response tokens via callback, and returns the finished response.
func (d *Driver) Ask(ctx context.Context, req AskRequest, onDelta func(reasoningDelta, contentDelta string)) (*AskResponse, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	defer d.generating.Store(false)

	// Ensure context has a default deadline (e.g. 10 minutes) if not already set
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
	}

	if err := d.EnsureReady(); err != nil {
		return nil, err
	}

	// Dismiss any initial overlays
	d.AutoDismissOverlays()

	// Switch conversation if specific ID requested and not starting new chat
	if req.ConversationID != "" && !req.IsNewChat {
		if err := d.switchChatUnlocked(req.ConversationID); err != nil {
			log.Printf("[driver] Warning: failed to switch chat to %s: %v", req.ConversationID, err)
		}
	} else {
		// Check if chat reset is necessary (req.IsNewChat or message count >= 25)
		msgCountVal, _ := d.client.Evaluate("document.querySelectorAll('.model-response-text, message-content, [data-test-id*=\"model-response\"]').length", false)
		msgCount := 0
		if f, ok := msgCountVal.(float64); ok {
			msgCount = int(f)
		}
		if req.IsNewChat || msgCount >= 25 {
			if err := d.ResetChatIfNecessary(true); err != nil {
				log.Printf("[driver] Warning: failed to reset chat: %v", err)
			}
		}
	}

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

	// Count responses prior to submit
	countVal, _ := d.client.Evaluate("document.querySelectorAll('.model-response-text, message-content, [data-test-id*=\"model-response\"]').length", false)
	initialCount := 0
	if f, ok := countVal.(float64); ok {
		initialCount = int(f)
	}

	// Submit
	submitTime := time.Now()
	if err := d.SubmitMessage(); err != nil {
		return nil, fmt.Errorf("failed to submit prompt: %w", err)
	}

	// Wait for the new response element to appear in DOM (prevents reading previous response)
	targetIndex := initialCount
	waitDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(waitDeadline) {
		cVal, err := d.client.Evaluate("document.querySelectorAll('.model-response-text, message-content, [data-test-id*=\"model-response\"]').length", false)
		if err == nil {
			if curLen, ok := cVal.(float64); ok && int(curLen) > initialCount {
				targetIndex = int(curLen) - 1
				break
			}
		}
		time.Sleep(60 * time.Millisecond)
	}

	// Polling loop
	var fullThoughts, fullContent string
	stableChecks := 0
	generationStarted := false

	extractScript := fmt.Sprintf(`((targetIndex) => {
		const responses = document.querySelectorAll('.model-response-text, message-content, [data-test-id*="model-response"]');
		if (responses.length <= targetIndex) {
			const stopBtn = document.querySelector('button[aria-label*="Остановить"], button[aria-label*="Stop"], button.stop-button');
			return { thoughts: '', content: '', isGenerating: !!stopBtn };
		}
		const latest = responses[targetIndex];
		if (!latest) return null;

		const thoughtsEl = latest.querySelector('model-thoughts, .model-thoughts, [data-test-id*="thoughts"], thinking-overlay');
		const thoughtsText = thoughtsEl ? (thoughtsEl.innerText || '').trim() : '';

		let rawContent = '';
		const markdownEls = Array.from(latest.querySelectorAll('.markdown'));
		const mainMarkdown = markdownEls.find(el => !el.closest('model-thoughts, .model-thoughts, [data-test-id*="thoughts"], thinking-overlay'));
		if (mainMarkdown) {
			rawContent = (mainMarkdown.innerText || '').trim();
		} else {
			const clone = latest.cloneNode(true);
			clone.querySelectorAll('model-thoughts, .model-thoughts, [data-test-id*="thoughts"], thinking-overlay').forEach(e => e.remove());
			rawContent = (clone.innerText || '').trim();
		}

		const stopBtn = document.querySelector('button[aria-label*="Остановить"], button[aria-label*="Stop"], button.stop-button');
		const isGenerating = !!stopBtn;

		return {
			thoughts: thoughtsText,
			content: rawContent,
			isGenerating: isGenerating
		};
	})(%d)`, targetIndex)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			stopScript := `(() => {
				const stopBtn = document.querySelector('button[aria-label*="Остановить"], button[aria-label*="Stop"], button.stop-button');
				if (stopBtn) stopBtn.click();
			})()`
			_, _ = d.client.Evaluate(stopScript, false)
			return nil, ctx.Err()
		case <-ticker.C:
			// Check start timeout: if 20 seconds elapsed after submit without generation starting
			if !generationStarted && time.Since(submitTime) > 20*time.Second {
				return nil, fmt.Errorf("timeout waiting for Gemini response generation to start")
			}

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
				if !generationStarted {
					generationStarted = true
					d.generating.Store(true)
				}
			}

			// Stream reasoning delta
			rDelta := computeSafeDelta(fullThoughts, thoughts)
			if rDelta != "" {
				fullThoughts = thoughts
			}

			// Stream content delta
			cDelta := computeSafeDelta(fullContent, content)
			if cDelta != "" {
				fullContent = content
			}

			if (rDelta != "" || cDelta != "") && onDelta != nil {
				onDelta(rDelta, cDelta)
			}

			// Check completion stability: 2 ticks (200ms) is sufficient after generation completes
			if generationStarted && !isGenerating && (len(fullContent) > 0 || len(fullThoughts) > 0) {
				if rDelta == "" && cDelta == "" {
					stableChecks++
					if stableChecks >= 2 { // 2 * 100ms = 200ms stable
						openCalls := strings.Count(fullContent, "```tool_call")
						closeCalls := strings.Count(fullContent, "```")
						if openCalls > 0 && closeCalls%2 != 0 {
							// Unclosed code block, wait extra
							time.Sleep(500 * time.Millisecond)
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
		Content:        fullContent,
		Reasoning:      fullThoughts,
		ConversationID: d.CurrentConversationID(),
		ToolCalls:      toolCalls,
	}, nil
}

// ParseConversationIDFromPath extracts the conversation ID from a URL pathname.
func ParseConversationIDFromPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "app" {
		return parts[1]
	}
	return ""
}

// CleanConversationID normalizes a conversation ID by stripping URL prefixes or paths.
func CleanConversationID(chatID string) string {
	cleanID := strings.TrimSpace(chatID)
	if strings.Contains(cleanID, "/app/") {
		parts := strings.Split(cleanID, "/app/")
		cleanID = parts[len(parts)-1]
	}
	return strings.Trim(cleanID, "/")
}

// ParseChatSessionsFromRaw converts raw JS evaluation data into a slice of ChatSession.
func ParseChatSessionsFromRaw(val interface{}) []ChatSession {
	items, ok := val.([]interface{})
	if !ok {
		return nil
	}

	var sessions []ChatSession
	for _, it := range items {
		m, ok := it.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		title, _ := m["title"].(string)
		url, _ := m["url"].(string)
		if id != "" && title != "" {
			sessions = append(sessions, ChatSession{
				ID:    id,
				Title: title,
				URL:   url,
			})
		}
	}

	return sessions
}

// ListChats extracts the conversation list directly from the Gemini sidebar DOM without reloading.
func (d *Driver) ListChats() ([]ChatSession, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.listChatsUnlocked()
}

func (d *Driver) listChatsUnlocked() ([]ChatSession, error) {
	if err := d.EnsureReady(); err != nil {
		return nil, err
	}

	script := `(() => {
		const links = Array.from(document.querySelectorAll('a[href*="/app/"]'));
		const seen = new Set();
		const list = [];
		for (const a of links) {
			const href = a.getAttribute('href') || '';
			if (!href.startsWith('/app/')) continue;
			const m = href.match(/\/app\/([a-zA-Z0-9_-]+)/);
			if (!m) continue;
			const id = m[1];
			if (seen.has(id)) continue;
			seen.add(id);
			const title = (a.innerText || '').trim();
			if (!title) continue;
			list.push({ id, title, url: 'https://gemini.google.com' + href });
		}
		return list;
	})()`

	val, err := d.client.Evaluate(script, false)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate ListChats: %w", err)
	}

	return ParseChatSessionsFromRaw(val), nil
}

// CurrentConversationID extracts the active conversation ID from window.location.pathname.
func (d *Driver) CurrentConversationID() string {
	if d.client == nil {
		return ""
	}
	val, err := d.client.Evaluate("window.location.pathname", false)
	if err != nil {
		return ""
	}
	path, ok := val.(string)
	if !ok {
		return ""
	}
	return ParseConversationIDFromPath(path)
}

// SwitchChat navigates to an existing conversation inside the SPA via link click or history API.
func (d *Driver) SwitchChat(chatID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.switchChatUnlocked(chatID)
}

func (d *Driver) switchChatUnlocked(chatID string) error {
	if err := d.EnsureReady(); err != nil {
		return err
	}

	cleanID := CleanConversationID(chatID)
	if cleanID == "" {
		return fmt.Errorf("chatID cannot be empty")
	}

	currentID := d.CurrentConversationID()
	if currentID == cleanID {
		return nil
	}

	switchScript := fmt.Sprintf(`((id) => {
		// 1. Try finding and clicking the link in the sidebar
		const link = document.querySelector('a[href="/app/' + id + '"], a[href*="/app/' + id + '"]');
		if (link) {
			link.click();
			return { method: "click", ok: true };
		}

		// 2. Fallback to client-side History pushState
		try {
			window.history.pushState({}, '', '/app/' + id);
			window.dispatchEvent(new PopStateEvent('popstate'));
			return { method: "pushState", ok: true };
		} catch (e) {
			return { method: "failed", ok: false, error: String(e) };
		}
	})(%q)`, cleanID)

	val, err := d.client.Evaluate(switchScript, false)
	if err != nil {
		return fmt.Errorf("failed to execute SwitchChat script: %w", err)
	}

	m, _ := val.(map[string]interface{})
	if m != nil && m["method"] == "failed" {
		// Fallback to hard navigation if SPA routing failed
		targetURL := fmt.Sprintf("https://gemini.google.com/app/%s", cleanID)
		if err := d.client.Navigate(targetURL); err != nil {
			return fmt.Errorf("failed to navigate to chat %s: %w", cleanID, err)
		}
	}

	// Wait up to 2 seconds for SPA route to settle
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if d.CurrentConversationID() == cleanID {
			return nil
		}
	}

	return nil
}

// CaptureScreenshot captures a native raster screenshot of the current page or specific element via CDP.
func (d *Driver) CaptureScreenshot(destPath string, opts ScreenshotOptions) (string, []byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.EnsureReady(); err != nil {
		return "", nil, err
	}

	format := opts.Format
	if format == "" {
		format = "png"
	}

	params := map[string]interface{}{
		"format": format,
	}
	if opts.Quality > 0 && format == "jpeg" {
		params["quality"] = opts.Quality
	}
	if opts.FullPage {
		params["captureBeyondViewport"] = true
	}

	if opts.Selector != "" {
		clipScript := fmt.Sprintf(`((sel) => {
			const el = document.querySelector(sel);
			if (!el) return null;
			const r = el.getBoundingClientRect();
			return { x: r.x, y: r.y, width: r.width, height: r.height, scale: 1 };
		})(%q)`, opts.Selector)

		val, err := d.client.Evaluate(clipScript, false)
		if err == nil && val != nil {
			if m, ok := val.(map[string]interface{}); ok {
				params["clip"] = m
			}
		}
	}

	res, err := d.client.Call("Page.captureScreenshot", params)
	if err != nil {
		return "", nil, fmt.Errorf("Page.captureScreenshot failed: %w", err)
	}

	var result struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(res, &result); err != nil {
		return "", nil, fmt.Errorf("failed to parse screenshot result: %w", err)
	}

	b64 := result.Data
	if b64 == "" {
		return "", nil, fmt.Errorf("empty screenshot data returned")
	}

	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", nil, fmt.Errorf("failed to decode base64 screenshot: %w", err)
	}

	if destPath == "" {
		home := os.Getenv("HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}
		if home == "" {
			home = "."
		}
		baseDir := filepath.Join(home, ".config", "AntigravityWebAdapter", "screenshots")
		_ = os.MkdirAll(baseDir, 0755)
		destPath = filepath.Join(baseDir, fmt.Sprintf("screenshot_%d.%s", time.Now().UnixNano(), format))
	} else {
		_ = os.MkdirAll(filepath.Dir(destPath), 0755)
	}

	if err := os.WriteFile(destPath, data, 0644); err != nil {
		return "", nil, fmt.Errorf("failed to write screenshot to %s: %w", destPath, err)
	}

	return destPath, data, nil
}
