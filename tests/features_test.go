package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/mcp"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/server"
)

func newTestTempDir() (string, func()) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("test_adapter_%d", time.Now().UnixNano()))
	_ = os.MkdirAll(dir, 0755)
	return dir, func() {
		_ = os.RemoveAll(dir)
	}
}

func TestSessionsHTTPEndpoints(t *testing.T) {
	tempDir, cleanup := newTestTempDir()
	defer cleanup()
	driver := engine.NewDriver(engine.DriverConfig{BaseDir: tempDir})
	defer driver.Close()
	srv := server.NewServer(driver, tempDir, 0)

	// 1. GET /v1/sessions
	{
		req := httptest.NewRequest(http.MethodGet, "/v1/sessions", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			var res map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("Failed to parse GET /v1/sessions JSON: %v", err)
			}
			if _, ok := res["current_conversation_id"]; !ok {
				t.Errorf("Expected current_conversation_id field in response")
			}
			if _, ok := res["sessions"]; !ok {
				t.Errorf("Expected sessions field in response")
			}
		} else if rec.Code != http.StatusInternalServerError {
			t.Fatalf("GET /v1/sessions expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 2. POST /v1/sessions (valid)
	{
		body := `{"conversation_id": "test-chat-abc"}`
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			var res map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("Failed to parse POST /v1/sessions JSON: %v", err)
			}
			if res["status"] != "ok" {
				t.Errorf("Expected status 'ok', got %v", res["status"])
			}
			if res["current_conversation_id"] != "test-chat-abc" {
				t.Errorf("Expected current_conversation_id 'test-chat-abc', got %v", res["current_conversation_id"])
			}
		} else if rec.Code != http.StatusInternalServerError {
			t.Fatalf("POST /v1/sessions expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3. POST /v1/sessions (invalid body)
	{
		body := `{}`
		req := httptest.NewRequest(http.MethodPost, "/v1/sessions", strings.NewReader(body))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("POST /v1/sessions with empty body expected 400, got %d", rec.Code)
		}
	}

	// 4. Method not allowed
	{
		req := httptest.NewRequest(http.MethodDelete, "/v1/sessions", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("DELETE /v1/sessions expected 405, got %d", rec.Code)
		}
	}
}

func TestScreenshotHTTPEndpoint(t *testing.T) {
	tempDir, cleanup := newTestTempDir()
	defer cleanup()
	driver := engine.NewDriver(engine.DriverConfig{BaseDir: tempDir})
	defer driver.Close()
	srv := server.NewServer(driver, tempDir, 0)

	// 1. GET /v1/screenshot (JSON mode)
	{
		req := httptest.NewRequest(http.MethodGet, "/v1/screenshot?selector=.app&full_page=true", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			contentType := rec.Header().Get("Content-Type")
			if !strings.Contains(contentType, "application/json") {
				t.Errorf("Expected application/json, got %s", contentType)
			}

			var res map[string]interface{}
			if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
				t.Fatalf("Failed to parse GET /v1/screenshot JSON: %v", err)
			}

			if res["status"] != "ok" {
				t.Errorf("Expected status 'ok', got %v", res["status"])
			}
		} else if rec.Code != http.StatusInternalServerError {
			t.Fatalf("GET /v1/screenshot expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 2. GET /v1/screenshot?raw=true (raw image mode)
	{
		req := httptest.NewRequest(http.MethodGet, "/v1/screenshot?raw=true", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code == http.StatusOK {
			contentType := rec.Header().Get("Content-Type")
			if contentType != "image/png" {
				t.Errorf("Expected image/png, got %s", contentType)
			}
		} else if rec.Code != http.StatusInternalServerError {
			t.Fatalf("GET /v1/screenshot?raw=true expected 200 or 500, got %d: %s", rec.Code, rec.Body.String())
		}
	}

	// 3. Method not allowed
	{
		req := httptest.NewRequest(http.MethodPost, "/v1/screenshot", nil)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST /v1/screenshot expected 405, got %d", rec.Code)
		}
	}
}

func TestMCPToolsListAndSchema(t *testing.T) {
	tempDir, cleanup := newTestTempDir()
	defer cleanup()
	driver := engine.NewDriver(engine.DriverConfig{BaseDir: tempDir})
	defer driver.Close()

	inPipeR, inPipeW := io.Pipe()
	var outBuf bytes.Buffer

	srv := mcp.NewMCPServer(driver, inPipeR, &outBuf)

	go func() {
		// Send tools/list request
		reqJSON := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}` + "\n"
		_, _ = inPipeW.Write([]byte(reqJSON))
		_ = inPipeW.Close()
	}()

	if err := srv.Run(); err != nil {
		t.Fatalf("MCPServer.Run error: %v", err)
	}

	var resp struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Result  struct {
			Tools []struct {
				Name        string                 `json:"name"`
				Description string                 `json:"description"`
				InputSchema map[string]interface{} `json:"inputSchema"`
			} `json:"tools"`
		} `json:"result"`
	}

	if err := json.Unmarshal(outBuf.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse MCP response: %v\nOutput: %s", err, outBuf.String())
	}

	toolMap := make(map[string]map[string]interface{})
	for _, tool := range resp.Result.Tools {
		toolMap[tool.Name] = tool.InputSchema
	}

	// Check gemini_web_sessions
	sessSchema, ok := toolMap["gemini_web_sessions"]
	if !ok {
		t.Fatalf("gemini_web_sessions tool not found in MCP tools list")
	}
	props, _ := sessSchema["properties"].(map[string]interface{})
	if _, ok := props["action"]; !ok {
		t.Errorf("gemini_web_sessions missing 'action' property")
	}
	if _, ok := props["conversation_id"]; !ok {
		t.Errorf("gemini_web_sessions missing 'conversation_id' property")
	}

	// Check gemini_web_screenshot
	scSchema, ok := toolMap["gemini_web_screenshot"]
	if !ok {
		t.Fatalf("gemini_web_screenshot tool not found in MCP tools list")
	}
	scProps, _ := scSchema["properties"].(map[string]interface{})
	if _, ok := scProps["selector"]; !ok {
		t.Errorf("gemini_web_screenshot missing 'selector' property")
	}
	if _, ok := scProps["full_page"]; !ok {
		t.Errorf("gemini_web_screenshot missing 'full_page' property")
	}
	if _, ok := scProps["output_path"]; !ok {
		t.Errorf("gemini_web_screenshot missing 'output_path' property")
	}

	// Check gemini_web_status
	if _, ok := toolMap["gemini_web_status"]; !ok {
		t.Fatalf("gemini_web_status tool not found in MCP tools list")
	}

	// Check ask_gemini_web has project_dir and conversation_id
	askSchema, ok := toolMap["ask_gemini_web"]
	if !ok {
		t.Fatalf("ask_gemini_web tool not found in MCP tools list")
	}
	askProps, _ := askSchema["properties"].(map[string]interface{})
	if _, ok := askProps["project_dir"]; !ok {
		t.Errorf("ask_gemini_web missing 'project_dir' property")
	}
	if _, ok := askProps["conversation_id"]; !ok {
		t.Errorf("ask_gemini_web missing 'conversation_id' property")
	}

	// Check gemini_pro_web_ask has project_dir and conversation_id
	proSchema, ok := toolMap["gemini_pro_web_ask"]
	if !ok {
		t.Fatalf("gemini_pro_web_ask tool not found in MCP tools list")
	}
	proProps, _ := proSchema["properties"].(map[string]interface{})
	if _, ok := proProps["project_dir"]; !ok {
		t.Errorf("gemini_pro_web_ask missing 'project_dir' property")
	}
	if _, ok := proProps["conversation_id"]; !ok {
		t.Errorf("gemini_pro_web_ask missing 'conversation_id' property")
	}
}

func TestMCPToolCallSessionsAndScreenshot(t *testing.T) {
	tempDir, cleanup := newTestTempDir()
	defer cleanup()
	driver := engine.NewDriver(engine.DriverConfig{BaseDir: tempDir})
	defer driver.Close()

	inPipeR, inPipeW := io.Pipe()
	var outBuf bytes.Buffer

	srv := mcp.NewMCPServer(driver, inPipeR, &outBuf)

	go func() {
		// 1. Call gemini_web_sessions with action='current'
		req1 := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"gemini_web_sessions","arguments":{"action":"current"}}}` + "\n"
		_, _ = inPipeW.Write([]byte(req1))

		// 2. Call gemini_web_sessions with missing conversation_id for switch
		req2 := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"gemini_web_sessions","arguments":{"action":"switch"}}}` + "\n"
		_, _ = inPipeW.Write([]byte(req2))

		// 3. Call gemini_web_sessions with unknown action
		req3 := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"gemini_web_sessions","arguments":{"action":"invalid_action"}}}` + "\n"
		_, _ = inPipeW.Write([]byte(req3))

		// 4. Call gemini_web_status
		req4 := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"gemini_web_status","arguments":{}}}` + "\n"
		_, _ = inPipeW.Write([]byte(req4))

		// 5. Ping
		req5 := `{"jsonrpc":"2.0","id":5,"method":"ping"}` + "\n"
		_, _ = inPipeW.Write([]byte(req5))

		_ = inPipeW.Close()
	}()

	if err := srv.Run(); err != nil {
		t.Fatalf("MCPServer.Run error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(outBuf.String()), "\n")
	if len(lines) != 5 {
		t.Fatalf("Expected 5 responses, got %d. Output:\n%s", len(lines), outBuf.String())
	}

	// Verify resp 1 (action=current) -> succeeds
	var resp1 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[0]), &resp1)
	res1, _ := resp1["result"].(map[string]interface{})
	content1, _ := res1["content"].([]interface{})
	if len(content1) == 0 {
		t.Errorf("Empty content for action=current: %s", lines[0])
	}

	// Verify resp 2 (action=switch with missing conversation_id) -> -32602 error
	var resp2 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[1]), &resp2)
	err2, _ := resp2["error"].(map[string]interface{})
	if err2 == nil || int(err2["code"].(float64)) != -32602 {
		t.Errorf("Expected code -32602 for missing conversation_id: %s", lines[1])
	}

	// Verify resp 3 (action=invalid_action) -> -32602 error
	var resp3 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[2]), &resp3)
	err3, _ := resp3["error"].(map[string]interface{})
	if err3 == nil || int(err3["code"].(float64)) != -32602 {
		t.Errorf("Expected code -32602 for invalid action: %s", lines[2])
	}

	// Verify resp 4 (gemini_web_status) -> returns content
	var resp4 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[3]), &resp4)
	res4, _ := resp4["result"].(map[string]interface{})
	content4, _ := res4["content"].([]interface{})
	if len(content4) == 0 {
		t.Errorf("Empty content for gemini_web_status: %s", lines[3])
	}

	// Verify resp 5 (ping)
	var resp5 map[string]interface{}
	_ = json.Unmarshal([]byte(lines[4]), &resp5)
	if _, ok := resp5["result"]; !ok {
		t.Errorf("Expected result in ping response: %s", lines[4])
	}
}
