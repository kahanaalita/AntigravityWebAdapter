package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

type cdpEvalRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

type cdpEvalResponse struct {
	ID     int `json:"id"`
	Result struct {
		Result struct {
			Type  string `json:"type"`
			Value string `json:"value"`
		} `json:"result"`
	} `json:"result"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// FetchWizDataViaCDP extracts WIZ_global_data (SNlM0e, cfb2h, FdrFJe) directly from Chrome page runtime.
func (s *SessionManager) FetchWizDataViaCDP(ctx context.Context) (*WizData, error) {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	wsURL, err := resolveWebSocketDebuggerURL(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("resolve cdp page target: %w", err)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial cdp ws: %w", err)
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

	jsExpr := `JSON.stringify({SNlM0e: window.WIZ_global_data?.SNlM0e || '', cfb2h: window.WIZ_global_data?.cfb2h || '', FdrFJe: window.WIZ_global_data?.FdrFJe || ''})`

	req := cdpEvalRequest{
		ID:     101,
		Method: "Runtime.evaluate",
		Params: map[string]any{
			"expression":    jsExpr,
			"returnByValue": true,
		},
	}

	if err := conn.WriteJSON(req); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("context canceled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("write Runtime.evaluate: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		var resp cdpEvalResponse
		if err := conn.ReadJSON(&resp); err != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("context canceled: %w", ctx.Err())
			}
			return nil, fmt.Errorf("read cdp eval response: %w", err)
		}
		if resp.ID == 101 {
			if resp.Error != nil {
				return nil, fmt.Errorf("cdp runtime error: %s", resp.Error.Message)
			}
			var data struct {
				SNlM0e string `json:"SNlM0e"`
				Cfb2h  string `json:"cfb2h"`
				FdrFJe string `json:"FdrFJe"`
			}
			if err := json.Unmarshal([]byte(resp.Result.Result.Value), &data); err != nil {
				return nil, fmt.Errorf("unmarshal wiz tokens: %w", err)
			}
			if data.SNlM0e == "" {
				return nil, errors.New("SNlM0e token is empty in page WIZ_global_data")
			}
			return &WizData{
				At:  data.SNlM0e,
				Bl:  data.Cfb2h,
				Sid: data.FdrFJe,
			}, nil
		}
	}
}
