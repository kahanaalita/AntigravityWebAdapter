package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/models"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/multimodal"
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
	driver  *engine.Driver
	baseDir string
	reader  *bufio.Reader
	writer  io.Writer
}

// NewMCPServer creates a new stdio MCP server.
func NewMCPServer(driver *engine.Driver, r io.Reader, w io.Writer) *MCPServer {
	home, err := os.UserHomeDir()
	baseDir := filepath.Join(os.TempDir(), "gemini-web-adapter")
	if err == nil {
		baseDir = filepath.Join(home, ".dsh-gemini-web-adapter-go")
	}
	return &MCPServer{
		driver:  driver,
		baseDir: baseDir,
		reader:  bufio.NewReader(r),
		writer:  w,
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
	case "ping":
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  map[string]interface{}{},
		}

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
		atomicSchema := func(desc string) map[string]interface{} {
			return map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"prompt": map[string]interface{}{
						"type":        "string",
						"description": desc,
					},
					"images": map[string]interface{}{
						"type":        "array",
						"items":       map[string]interface{}{"type": "string"},
						"description": "Optional list of local image paths or base64 data URIs",
					},
				},
				"required": []string{"prompt"},
			}
		}

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
						"name":        "gemini_pro_web_ask",
						"description": "Unified invocation of Google AI Pro web models (Hydrogen 4 and Argon 4) with granular reasoning effort.",
						"inputSchema": map[string]interface{}{
							"type": "object",
							"properties": map[string]interface{}{
								"prompt": map[string]interface{}{"type": "string", "description": "Prompt text"},
								"model":  map[string]interface{}{"type": "string", "description": "Target model: 'hydrogen-4' or 'argon-4'"},
								"effort": map[string]interface{}{"type": "string", "description": "Effort level: low, medium, high"},
								"images": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
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
					{"name": "argon_high", "description": "Gemini 4 Argon with high reasoning effort (deep synthesis & code analysis)", "inputSchema": atomicSchema("Prompt for Argon 4 High")},
					{"name": "argon_medium", "description": "Gemini 4 Argon with medium reasoning effort (balanced code & analysis)", "inputSchema": atomicSchema("Prompt for Argon 4 Medium")},
					{"name": "argon_low", "description": "Gemini 4 Argon with low reasoning effort (fast syntax & short answers)", "inputSchema": atomicSchema("Prompt for Argon 4 Low")},
					{"name": "hydrogen_high", "description": "Gemini 4 Hydrogen with high reasoning effort (deep logic analysis)", "inputSchema": atomicSchema("Prompt for Hydrogen 4 High")},
					{"name": "hydrogen_middle", "description": "Gemini 4 Hydrogen with middle reasoning effort (balanced daily mode)", "inputSchema": atomicSchema("Prompt for Hydrogen 4 Middle")},
					{"name": "hydrogen_low", "description": "Gemini 4 Hydrogen with low reasoning effort (fast answers)", "inputSchema": atomicSchema("Prompt for Hydrogen 4 Low")},
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

		switch callParams.Name {
		case "gemini_web_status":
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

		case "ask_gemini_web":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			modelStr, _ := callParams.Arguments["model"].(string)
			effortStr, _ := callParams.Arguments["effort"].(string)
			return s.executeAsk(req.ID, promptStr, modelStr, effortStr, extractImages(callParams.Arguments))

		case "gemini_pro_web_ask":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			modelStr, _ := callParams.Arguments["model"].(string)
			effortStr, _ := callParams.Arguments["effort"].(string)
			return s.executeAsk(req.ID, promptStr, modelStr, effortStr, extractImages(callParams.Arguments))

		case "argon_high":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Argon", "high", extractImages(callParams.Arguments))

		case "argon_medium":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Argon", "medium", extractImages(callParams.Arguments))

		case "argon_low":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Argon", "low", extractImages(callParams.Arguments))

		case "hydrogen_high":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Hydrogen", "high", extractImages(callParams.Arguments))

		case "hydrogen_middle", "hydrogen_medium":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Hydrogen", "medium", extractImages(callParams.Arguments))

		case "hydrogen_low":
			promptStr, _ := callParams.Arguments["prompt"].(string)
			return s.executeAsk(req.ID, promptStr, "4 Hydrogen", "low", extractImages(callParams.Arguments))

		default:
			return &MCPResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Error: map[string]interface{}{
					"code":    -32601,
					"message": fmt.Sprintf("Method not found: %s", callParams.Name),
				},
			}
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

func (s *MCPServer) executeAsk(reqID interface{}, promptStr, modelStr, effortStr string, rawImages []string) *MCPResponse {
	tmpDir := filepath.Join(s.baseDir, "tmp")
	imageFiles, tempFiles, err := multimodal.ResolveImageInputs(rawImages, tmpDir)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      reqID,
			Result: map[string]interface{}{
				"isError": true,
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("Error resolving images: %v", err)},
				},
			},
		}
	}
	defer func() {
		for _, f := range tempFiles {
			_ = os.Remove(f)
		}
	}()

	domModel, eff := models.ResolveModel(modelStr, effortStr)
	askReq := engine.AskRequest{
		Question: promptStr,
		Model:    domModel,
		Effort:   eff,
		Files:    imageFiles,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	resp, err := s.driver.Ask(ctx, askReq, nil)
	if err != nil {
		return &MCPResponse{
			JSONRPC: "2.0",
			ID:      reqID,
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
		outText = fmt.Sprintf("<details>\n<summary>Thinking Process</summary>\n\n%s\n\n</details>\n\n%s", resp.Reasoning, outText)
	}

	return &MCPResponse{
		JSONRPC: "2.0",
		ID:      reqID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": outText},
			},
		},
	}
}

func extractImages(args map[string]interface{}) []string {
	var imageFiles []string
	parseList := func(raw interface{}) {
		if arr, ok := raw.([]interface{}); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok && s != "" {
					imageFiles = append(imageFiles, s)
				}
			}
		}
	}
	if raw, ok := args["image_paths"]; ok && raw != nil {
		parseList(raw)
	}
	if raw, ok := args["images"]; ok && raw != nil {
		parseList(raw)
	}
	return imageFiles
}

// RunStdio starts the MCP stdio handler using os.Stdin and os.Stdout.
func RunStdio(driver *engine.Driver) error {
	server := NewMCPServer(driver, os.Stdin, os.Stdout)
	return server.Run()
}
