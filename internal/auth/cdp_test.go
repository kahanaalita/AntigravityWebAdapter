package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func setupMockCDPServer(t *testing.T, wsHandler func(*websocket.Conn)) (*httptest.Server, *SessionManager) {
	t.Helper()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json" || r.URL.Path == "/json/list" {
			wsURL := "ws://" + srv.Listener.Addr().String() + "/devtools/page/1"
			targets := []map[string]any{
				{
					"type":                 "page",
					"url":                  "https://gemini.google.com/app",
					"webSocketDebuggerUrl": wsURL,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(targets)
			return
		}
		if r.URL.Path == "/devtools/page/1" {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				return
			}
			defer conn.Close()
			wsHandler(conn)
			return
		}
		http.NotFound(w, r)
	}))

	tmpDir := t.TempDir()
	cookieFile := tmpDir + "/cookies.json"
	sm, err := NewSessionManager(cookieFile, srv.URL)
	if err != nil {
		t.Fatalf("NewSessionManager failed: %v", err)
	}
	return srv, sm
}

func TestBridgeGenerate_Success(t *testing.T) {
	srv, sm := setupMockCDPServer(t, func(conn *websocket.Conn) {
		var req cdpEvalRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}

		if req.ID != 201 || req.Method != "Runtime.evaluate" {
			t.Errorf("unexpected request: %+v", req)
			return
		}

		expr, _ := req.Params["expression"].(string)
		if !strings.Contains(expr, "_reqid=") {
			t.Errorf("expression missing _reqid: %s", expr)
		}
		if !strings.Contains(expr, "x-goog-ext-525001261-jspb") {
			t.Errorf("expression missing extension header key: %s", expr)
		}
		if !strings.Contains(expr, "test-ext-header") {
			t.Errorf("expression missing extension header value: %s", expr)
		}
		if !strings.Contains(expr, "test_bl_value") {
			t.Errorf("expression missing bl: %s", expr)
		}
		if !strings.Contains(expr, "test_sid_value") {
			t.Errorf("expression missing sid: %s", expr)
		}

		resp := cdpEvalResponse{
			ID: 201,
		}
		resp.Result.Result.Type = "string"
		resp.Result.Result.Value = ")]}'\n[[\"wrb.fr\",null,\"success_body\"]]"
		_ = conn.WriteJSON(resp)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	val, err := sm.BridgeGenerate(ctx, BridgeGenerateParams{
		Body:            "f.req=test_body",
		Bl:              "test_bl_value",
		Sid:             "test_sid_value",
		ExtensionHeader: "test-ext-header",
		Timeout:         30 * time.Second,
	})
	if err != nil {
		t.Fatalf("BridgeGenerate failed: %v", err)
	}
	if !strings.Contains(val, "success_body") {
		t.Errorf("unexpected return val: %s", val)
	}
}

func TestBridgeGenerate_ContextCancellation(t *testing.T) {
	srv, sm := setupMockCDPServer(t, func(conn *websocket.Conn) {
		// Hang forever without replying
		var req cdpEvalRequest
		_ = conn.ReadJSON(&req)
		time.Sleep(2 * time.Second)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sm.BridgeGenerate(ctx, BridgeGenerateParams{
		Body:            "f.req=test",
		Bl:              "bl",
		Sid:             "sid",
		ExtensionHeader: "hdr",
		Timeout:         60 * time.Second,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("expected context canceled error, got %v", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("BridgeGenerate took too long to cancel: %v (expected < 1s)", elapsed)
	}
}

func TestFetchWizDataViaCDP_ContextCancellation(t *testing.T) {
	srv, sm := setupMockCDPServer(t, func(conn *websocket.Conn) {
		var req cdpEvalRequest
		_ = conn.ReadJSON(&req)
		time.Sleep(2 * time.Second)
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := sm.FetchWizDataViaCDP(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("FetchWizDataViaCDP took too long to cancel: %v (expected < 1s)", elapsed)
	}
}

func TestFetchCookiesViaWebSocket_ContextCancellation(t *testing.T) {
	srv, _ := setupMockCDPServer(t, func(conn *websocket.Conn) {
		var req cdpRequest
		_ = conn.ReadJSON(&req)
		time.Sleep(2 * time.Second)
	})
	defer srv.Close()

	wsURL := "ws://" + srv.Listener.Addr().String() + "/devtools/page/1"
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := fetchCookiesViaWebSocket(ctx, wsURL)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
	if elapsed > 1*time.Second {
		t.Errorf("fetchCookiesViaWebSocket took too long to cancel: %v (expected < 1s)", elapsed)
	}
}
