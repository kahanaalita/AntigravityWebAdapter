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

// CDPVersionInfo holds response from http://<host>:<port>/json/version
type CDPVersionInfo struct {
	Browser              string `json:"Browser"`
	ProtocolVersion      string `json:"Protocol-Version"`
	UserAgent            string `json:"User-Agent"`
	V8Version            string `json:"V8-Version"`
	WebKitVersion        string `json:"WebKit-Version"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

// CDPCookie represents a cookie returned by CDP Network.getCookies.
type CDPCookie struct {
	Name     string  `json:"name"`
	Value    string  `json:"value"`
	Domain   string  `json:"domain"`
	Path     string  `json:"path"`
	Expires  float64 `json:"expires"`
	Size     int     `json:"size"`
	HTTPOnly bool    `json:"httpOnly"`
	Secure   bool    `json:"secure"`
	Session  bool    `json:"session"`
}

type cdpRequest struct {
	ID     int            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params,omitempty"`
}

type cdpResponse struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpError       `json:"error,omitempty"`
}

type cdpError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type getCookiesResult struct {
	Cookies []CDPCookie `json:"cookies"`
}

// HarvestCookiesFromCDP connects to Chrome DevTools Protocol and extracts session cookies.
func (s *SessionManager) HarvestCookiesFromCDP(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.harvestCookiesFromCDPLocked(ctx)
}

func (s *SessionManager) harvestCookiesFromCDPLocked(ctx context.Context) error {
	endpoint := s.cdpURL
	if endpoint == "" {
		endpoint = "http://127.0.0.1:9222"
	}

	wsURL, err := resolveWebSocketDebuggerURL(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("resolve cdp debugger url: %w", err)
	}

	cookies, err := fetchCookiesViaWebSocket(ctx, wsURL)
	if err != nil {
		return fmt.Errorf("fetch cookies via cdp ws: %w", err)
	}

	var foundCount int
	for _, c := range cookies {
		// We are interested in Google auth cookies
		if strings.HasPrefix(c.Name, "__Secure-") || c.Name == "SID" || c.Name == "HSID" || c.Name == "SSID" {
			s.store.Set(c.Name, c.Value)
			foundCount++
		}
	}

	if foundCount == 0 {
		return errors.New("no Google authentication cookies found in Chrome session via CDP")
	}

	_ = s.store.Save()
	return nil
}

func resolveWebSocketDebuggerURL(ctx context.Context, endpoint string) (string, error) {
	if strings.HasPrefix(endpoint, "ws://") || strings.HasPrefix(endpoint, "wss://") {
		return endpoint, nil
	}

	clean := strings.TrimRight(endpoint, "/")
	reqURL := clean + "/json/version"

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return "", err
	}

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request %s: %w", reqURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status from %s: %d", reqURL, resp.StatusCode)
	}

	var ver CDPVersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&ver); err != nil {
		return "", fmt.Errorf("decode json/version: %w", err)
	}

	if ver.WebSocketDebuggerURL == "" {
		return "", errors.New("webSocketDebuggerUrl is empty in json/version response")
	}

	return ver.WebSocketDebuggerURL, nil
}

func fetchCookiesViaWebSocket(ctx context.Context, wsURL string) ([]CDPCookie, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
	}

	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial ws %s: %w", wsURL, err)
	}
	defer conn.Close()

	// 1. Enable Network domain
	reqEnable := cdpRequest{
		ID:     1,
		Method: "Network.enable",
	}
	if err := conn.WriteJSON(reqEnable); err != nil {
		return nil, fmt.Errorf("write Network.enable: %w", err)
	}

	// 2. Request cookies for google targets
	reqCookies := cdpRequest{
		ID:     2,
		Method: "Network.getCookies",
		Params: map[string]any{
			"urls": []string{
				"https://gemini.google.com",
				"https://accounts.google.com",
				"https://google.com",
			},
		},
	}
	if err := conn.WriteJSON(reqCookies); err != nil {
		return nil, fmt.Errorf("write Network.getCookies: %w", err)
	}

	// Read responses until we get response for ID 2
	deadline := time.Now().Add(10 * time.Second)
	_ = conn.SetReadDeadline(deadline)

	for {
		var resp cdpResponse
		if err := conn.ReadJSON(&resp); err != nil {
			return nil, fmt.Errorf("read cdp response: %w", err)
		}

		if resp.ID == 2 {
			if resp.Error != nil {
				return nil, fmt.Errorf("cdp error: %s (code %d)", resp.Error.Message, resp.Error.Code)
			}

			var res getCookiesResult
			if err := json.Unmarshal(resp.Result, &res); err != nil {
				return nil, fmt.Errorf("unmarshal cookies result: %w", err)
			}

			return res.Cookies, nil
		}
	}
}
