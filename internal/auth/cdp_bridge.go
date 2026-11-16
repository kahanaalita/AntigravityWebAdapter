package auth

import (
	"context"
	"errors"
	"fmt"
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
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	wsURL, err := resolveWebSocketDebuggerURL(ctx, endpoint)
	if err != nil {
		return "", fmt.Errorf("resolve cdp page target: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
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

	jsExpr := fmt.Sprintf(`(async () => {
		const at = window.WIZ_global_data?.SNlM0e || '';
		const bl = %q || window.WIZ_global_data?.cfb2h || '';
		const sid = %q || window.WIZ_global_data?.FdrFJe || '';
		const extHdr = %q;
		const reqID = Math.floor(100000 + Math.random() * 900000);
		const url = 'https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=' + bl + '&_reqid=' + reqID + '&rt=c&f.sid=' + sid;
		const headers = {
			'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8',
			'x-goog-ext-525001261-jspb': extHdr
		};
		if (!extHdr) {
			delete headers['x-goog-ext-525001261-jspb'];
		}
		const r = await fetch(url, {
			method: 'POST',
			headers: headers,
			body: %q
		});
		return await r.text();
	})()`, params.Bl, params.Sid, params.ExtensionHeader, params.Body)

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

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout + 15*time.Second))

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
				return "", fmt.Errorf("cdp runtime error: %s", resp.Error.Message)
			}
			val := resp.Result.Result.Value
			if val == "" {
				return "", errors.New("empty response from browser fetch")
			}
			return val, nil
		}
	}
}
