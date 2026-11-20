package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// BridgeGenerateParams holds parameters for executing generation via CDP bridge.
type BridgeGenerateParams struct {
	Body            string
	Bl              string
	Sid             string
	ExtensionHeader string
	Timeout         time.Duration
}

// BridgeGenerate executes the Gemini Web RPC directly in the authenticated browser context via CDP Runtime.evaluate.
func (s *SessionManager) BridgeGenerate(ctx context.Context, params BridgeGenerateParams) (string, error) {
	if s.cdpSem != nil {
		select {
		case s.cdpSem <- struct{}{}:
			defer func() { <-s.cdpSem }()
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	// Strictly resolve to gemini.google.com tab
	wsURL, err := resolveGeminiTargetURL(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("resolve gemini cdp page target: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return "", fmt.Errorf("dial cdp ws: %w", err)
	}
	defer conn.Close()

	stopWatcher := make(chan struct{})
	defer close(stopWatcher)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopWatcher:
		}
	}()

	timeout := params.Timeout
	if timeout <= 0 {
		// Официальный таймаут стриминга Google Gemini 4 (20 минут)
		timeout = 20 * time.Minute
	}
	timeoutMs := timeout.Milliseconds()

	bodyJSON, err := json.Marshal(params.Body)
	if err != nil {
		return "", fmt.Errorf("marshal params body: %w", err)
	}

	jsExpr := fmt.Sprintf(`(async () => {
		const liveAt = window.WIZ_global_data?.SNlM0e || '';
		const bl = %q || window.WIZ_global_data?.cfb2h || '';
		const sid = %q || window.WIZ_global_data?.FdrFJe || '';
		const extHdr = %q;
		const reqID = Math.floor(100000 + Math.random() * 900000);
		const userMatch = window.location.pathname.match(/^\/u\/(\d+)\//);
		const userPrefix = userMatch ? '/u/' + userMatch[1] : '';
		let url = 'https://gemini.google.com' + userPrefix + '/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=' + encodeURIComponent(bl) + '&_reqid=' + reqID + '&rt=c';
		if (sid) {
			url += '&f.sid=' + encodeURIComponent(sid);
		}

		let bodyStr = %s;
		if (liveAt) {
			const atRegex = /(^|&)at=([^&]*)/;
			const match = bodyStr.match(atRegex);
			if (match) {
				const currentAt = decodeURIComponent(match[2]);
				if (currentAt !== liveAt) {
					bodyStr = bodyStr.replace(atRegex, '$1at=' + encodeURIComponent(liveAt));
				}
			} else {
				bodyStr += (bodyStr.length > 0 ? '&' : '') + 'at=' + encodeURIComponent(liveAt);
			}
		}

		const headers = {
			'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
			'X-Same-Domain': '1'
		};
		if (extHdr) {
			headers['x-goog-ext-525001261-jspb'] = extHdr;
		}

		const controller = new AbortController();
		const timeoutMs = %d;
		const timer = setTimeout(() => controller.abort('Fetch timeout in CDP'), timeoutMs);
		try {
			const r = await fetch(url, {
				method: 'POST',
				headers: headers,
				body: bodyStr,
				credentials: 'include',
				signal: controller.signal
			});
			clearTimeout(timer);
			if (!r.ok) {
				const errText = await r.text();
				throw new Error('HTTP ' + r.status + ' (' + r.statusText + '): ' + errText.slice(0, 300));
			}
			return await r.text();
		} catch (e) {
			clearTimeout(timer);
			throw e;
		}
	})()`, params.Bl, params.Sid, params.ExtensionHeader, string(bodyJSON), timeoutMs)

	req := cdpEvalRequest{
		ID:     201,
		Method: "Runtime.evaluate",
		Params: map[string]any{
			"expression":    jsExpr,
			"awaitPromise":  true,
			"returnByValue": true,
		},
	}

	if err := conn.WriteJSON(req); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("context canceled: %w", ctx.Err())
		}
		return "", fmt.Errorf("write Runtime.evaluate: %w", err)
	}

	// Устанавливаем дедлайн чтения с запасом 30 секунд над временем выполнения генерации
	_ = conn.SetReadDeadline(time.Now().Add(timeout + 30*time.Second))

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		var resp cdpEvalResponse
		if err := conn.ReadJSON(&resp); err != nil {
			if ctx.Err() != nil {
				return "", fmt.Errorf("context canceled: %w", ctx.Err())
			}
			return "", fmt.Errorf("read cdp eval response: %w", err)
		}

		if resp.ID == 201 {
			if resp.Error != nil {
				return "", fmt.Errorf("cdp protocol error: %s (code %d)", resp.Error.Message, resp.Error.Code)
			}
			if resp.Result.ExceptionDetails != nil {
				ex := resp.Result.ExceptionDetails
				desc := ex.Text
				if ex.Exception != nil && ex.Exception.Description != "" {
					desc = ex.Exception.Description
				}
				return "", fmt.Errorf("browser js exception at line %d: %s", ex.LineNumber, desc)
			}
			val := resp.Result.Result.Value
			if val == "" {
				return "", errors.New("empty response text from browser fetch")
			}
			return val, nil
		}
	}
}

// resolveGeminiTargetURL strictly finds the gemini.google.com tab in Chrome via CDP,
// giving priority to the active app tab (gemini.google.com/app).
func resolveGeminiTargetURL(ctx context.Context, endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}

	clean := strings.TrimRight(endpoint, "/")
	targetURL := clean + "/json"

	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("connect to CDP %s: %w", targetURL, err)
	}
	defer resp.Body.Close()

	var targets []struct {
		Type                 string `json:"type"`
		URL                  string `json:"url"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&targets); err != nil {
		return "", fmt.Errorf("decode cdp targets: %w", err)
	}

	// 1. Highest priority: active app page gemini.google.com/app
	for _, t := range targets {
		if t.Type == "page" && strings.Contains(t.URL, "gemini.google.com/app") && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}

	// 2. Secondary priority: any gemini.google.com page
	for _, t := range targets {
		if t.Type == "page" && strings.Contains(t.URL, "gemini.google.com") && t.WebSocketDebuggerURL != "" {
			return t.WebSocketDebuggerURL, nil
		}
	}

	return "", errors.New("no active gemini.google.com tab found in Chrome via CDP. Please open https://gemini.google.com in Chrome")
}
