package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/models"
)

// MCPRequest represents standard JSON-RPC 2.0 request used by Model Context Protocol.
type MCPRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// MCPResponse represents JSON-RPC 2.0 response.
type MCPResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

// MCPServer serves MCP requests over stdio (stdin/stdout).
type MCPServer struct {
	driver *engine.Driver
	reader *bufio.Reader
	writer io.Writer
}

// NewMCPServer creates a new stdio MCP server.
func NewMCPServer(driver *engine.Driver, r io.Reader, w io.Writer) *MCPServer {
	return &MCPServer{
		driver: driver,
		reader: bufio.NewReader(r),
		writer: w,
	}
}

// Run starts the stdio read loop until EOF.
func (s *MCPServer) Run() error {
	for {
		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		if len(line) == 0 {
			continue
		}

		var req MCPRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}

		resp := s.handleRequest(&req)
		if resp != nil && req.ID != nil {
			data, _ := json.Marshal(resp)
			_, _ = s.writer.Write(append(data, '\n'))
		}
	}
}

func (s *MCPServer) handleRequest(req *MCPRequest) *MCPResponse {
	switch req.Method {
	case "initialize":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"serverInfo": map[string]string{
					"name":    "gemini-web-adapter-mcp",
					"version": "1.0.0",
				},
				"capabilities": map[string]interface{}{
					"tools": map[string]bool{
						"listChanged": false,
					},
				},
			},
		}

	case "notifications/initialized":
		return nil

	case "tools/list":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "ask_gemini_web",
						"description": "Send a prompt to Google Gemini Web (4 Hydrogen, 3.5 Flash-Lite, 3.1 Pro, 4 Argon) with deep reasoning and multimodal vision support.",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"prompt": map[string]interface{}{
									"type":        "string",
									"description": "The user prompt or query to send to Gemini.",
								},
								"model": map[string]interface{}{
									"type":        "string",
									"description": "Target model: '4 Hydrogen' (or 'gemini-4-hydrogen-high'), '3.5 Flash-Lite', '3.1 Pro', '4 Argon'.",
									"default":     "4 Hydrogen",
								},
								"effort": map[string]interface{}{
									"type":        "string",
									"description": "Reasoning effort: 'low', 'medium', 'high'.",
									"default":     "medium",
								},
								"image_paths": map[string]interface{}{
									"type": "array",
									"items": map[string]interface{}{
										"type": "string",
									},
									"description": "Optional list of local image file paths to attach via synthetic paste.",
								},
							},
							"required": []string{"prompt"},
						},
					},
					{
						"name":        "gemini_web_status",
						"description": "Check if Gemini Web session is active and user is logged in.",
						"inputSchema": map[string]interface{}{
							"type":       "object",
							"properties": map[string]interface{}{},
						},
					},
				},
			},
		}

	case "tools/call":
		var callParams struct {
			Name      string                 `json:"name"`
			Arguments map[string]interface{} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &callParams); err != nil {
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32602,
					"message": "Invalid params",
				},
			}
		}

		if callParams.Name == "gemini_web_status" {
			loggedIn, err := s.driver.CheckLoginStatus()
			text := fmt.Sprintf("LoggedIn: %v, Error: %v", loggedIn, err)
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": text},
					},
				},
			}
		}

		if callParams.Name == "ask_gemini_web" {
			promptStr, _ := callParams.Arguments["prompt"].(string)
			modelStr, _ := callParams.Arguments["model"].(string)
			effortStr, _ := callParams.Arguments["effort"].(string)

			domModel, eff := models.ResolveModel(modelStr, effortStr)
			var imageFiles []string
			if rawImgs, ok := callParams.Arguments["image_paths"].([]interface{}); ok {
				for _, img := range rawImgs {
					if s, ok := img.(string); ok && s != "" {
						imageFiles = append(imageFiles, s)
					}
				}
			}

			askReq := engine.AskRequest{
				Question: promptStr,
				Model:    domModel,
				Effort:   eff,
				Files:    imageFiles,
			}

			resp, err := s.driver.Ask(context.Background(), askReq, nil)
			if err != nil {
				return &MCPResponse{
					JSONRPC: "2.0",
					ID:      req.ID,
					Result: map[string]interface{}{
						"isError": true,
						"content": []map[string]interface{}{
							{"type": "text", "text": fmt.Sprintf("Error: %v", err)},
						},
					},
				}
			}

			outText := resp.Content
			if resp.Reasoning != "" {
				outText = fmt.Sprintf("<thought>\n%s\n</thought>\n\n%s", resp.Reasoning, outText)
			}

			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"content": []map[string]interface{}{
						{"type": "text", "text": outText},
					},
				},
			}
		}

		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Method not found: %s", callParams.Name),
			},
		}

	default:
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error: map[string]interface{}{
				"code":    -32601,
				"message": fmt.Sprintf("Method not found: %s", req.Method),
			},
		}
	}
}

// RunStdio starts the MCP stdio handler using os.Stdin and os.Stdout.
func RunStdio(driver *engine.Driver) error {
	server := NewMCPServer(driver, os.Stdin, os.Stdout)
	return server.Run()
}
