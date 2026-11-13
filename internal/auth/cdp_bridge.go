package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

// BridgeGenerate executes the Gemini Web RPC directly in the authenticated browser context via CDP Runtime.evaluate.
func (s *SessionManager) BridgeGenerate(ctx context.Context, body, bl, sid string) (string, error) {
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

	jsExpr := fmt.Sprintf(`(async () => {
		const at = window.WIZ_global_data?.SNlM0e || '';
		const bl = %q || window.WIZ_global_data?.cfb2h || '';
		const sid = %q || window.WIZ_global_data?.FdrFJe || '';
		const url = 'https://gemini.google.com/_/BardChatUi/data/assistant.lamda.BardFrontendService/StreamGenerate?bl=' + bl + '&rt=c&f.sid=' + sid;
		const r = await fetch(url, {
			method: 'POST',
			headers: {'Content-Type': 'application/x-www-form-urlencoded;charset=UTF-8'},
			body: %q
		});
		return await r.text();
	})()`, bl, sid, body)

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
		return "", fmt.Errorf("write Runtime.evaluate: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	for {
		var resp cdpEvalResponse
		if err := conn.ReadJSON(&resp); err != nil {
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
