package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/bundler"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/models"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/multimodal"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/parser"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/prompt"
)

// Server encapsulates the HTTP gateway.
type Server struct {
	driver  *engine.Driver
	baseDir string
	port    int
	mux     *http.ServeMux
}

// NewServer initializes HTTP routes and server dependencies.
func NewServer(driver *engine.Driver, baseDir string, port int) *Server {
	s := &Server{
		driver:  driver,
		baseDir: baseDir,
		port:    port,
		mux:     http.NewServeMux(),
	}

	s.mux.HandleFunc("/v1/models", s.handleModels)
	s.mux.HandleFunc("/v1/chat/completions", s.handleChatCompletions)
	s.mux.HandleFunc("/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/v1/screenshot", s.handleScreenshot)
	s.mux.HandleFunc("/login", s.handleLogin)
	s.mux.HandleFunc("/login-status", s.handleLoginStatus)
	s.mux.HandleFunc("/health", s.handleHealth)

	return s
}

func (s *Server) ListenAndServe() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	log.Printf("[server] Go Gemini Web Adapter listening on http://%s", addr)
	return http.ListenAndServe(addr, s.mux)
}

// ServeHTTP dispatches the request to the internal router.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"backend": "go",
		"version": "1.0.0",
	})
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	var data []map[string]interface{}
	for id, spec := range models.Registry {
		data = append(data, map[string]interface{}{
			"id":                id,
			"object":            "model",
			"created":           1700000000,
			"owned_by":          "google",
			"name":              spec.Name,
			"input_modalities":  spec.InputModalities,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}

func (s *Server) handleLoginStatus(w http.ResponseWriter, r *http.Request) {
	loggedIn, err := s.driver.CheckLoginStatus()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"loggedIn": loggedIn,
		"error":    err != nil,
	})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	err := s.driver.EnsureReady()
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "browser opened"})
}

type ChatCompletionRequest struct {
	Model           string                  `json:"model"`
	Messages        []prompt.ChatMessage   `json:"messages"`
	Stream          bool                    `json:"stream"`
	ReasoningEffort string                  `json:"reasoning_effort,omitempty"`
	Tools           []prompt.ToolDefinition `json:"tools,omitempty"`
	Files           []string                `json:"files,omitempty"`
	ProjectDir      string                  `json:"project_dir,omitempty"`
	ConversationID  string                  `json:"conversation_id,omitempty"`
}

func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	var req ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, fmt.Sprintf("Invalid JSON payload: %v", err), http.StatusBadRequest)
		return
	}

	domModel, effort := models.ResolveModel(req.Model, req.ReasoningEffort)

	// Extract images
	tmpDir := filepath.Join(s.baseDir, "tmp")
	var imageFiles []string
	var tempFiles []string
	for _, m := range req.Messages {
		if parts, ok := m.Content.([]interface{}); ok {
			imgs, temps := multimodal.ExtractImagesFromPartsWithTemp(parts, tmpDir)
			imageFiles = append(imageFiles, imgs...)
			tempFiles = append(tempFiles, temps...)
		}
	}
	defer func() {
		for _, f := range tempFiles {
			_ = os.Remove(f)
		}
	}()

	if len(req.Files) > 0 {
		imageFiles = append(imageFiles, req.Files...)
	}

	// Format prompt context
	isIncremental := len(req.Messages) > 2 && req.Messages[len(req.Messages)-1].Role == "tool"
	var question string
	if isIncremental {
		question = prompt.BuildIncrementalContext(req.Messages)
	} else {
		question = prompt.BuildFullContext(req.Messages, req.Tools)
	}

	var tempBundleFile string
	if req.ProjectDir != "" {
		b := bundler.New(bundler.Config{RootDir: req.ProjectDir})
		dump, stats, err := b.Bundle()
		if err == nil {
			base := filepath.Base(req.ProjectDir)
			_ = os.MkdirAll(tmpDir, 0755)
			tmpFile := filepath.Join(tmpDir, fmt.Sprintf("bundle_%s_%d.txt", base, time.Now().UnixNano()))
			if err := os.WriteFile(tmpFile, []byte(dump), 0644); err == nil {
				tempBundleFile = tmpFile
				imageFiles = append(imageFiles, tmpFile)
				question = fmt.Sprintf("[Прикреплен проект: %s (%d файлов, ~%d токенов)]\n%s", base, stats.TotalFiles, stats.EstimatedTokens, question)
			}
		}
	}
	if tempBundleFile != "" {
		defer os.Remove(tempBundleFile)
	}

	askReq := engine.AskRequest{
		Question:       question,
		Model:          domModel,
		Effort:         effort,
		Files:          imageFiles,
		Tools:          req.Tools,
		IsNewChat:      !isIncremental,
		ConversationID: req.ConversationID,
	}

	created := time.Now().Unix()
	chatCmplID := fmt.Sprintf("chatcmpl-%d-%x", created, rand.Int31())

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)

		toolFilter := newStreamToolFilter(len(req.Tools) > 0)

		// Callback for stream deltas
		onDelta := func(rDelta, cDelta string) {
			if rDelta != "" {
				chunk := map[string]interface{}{
					"id":      chatCmplID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   req.Model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"reasoning_content": rDelta,
							},
						},
					},
				}
				b, _ := json.Marshal(chunk)
				fmt.Fprintf(w, "data: %s\n\n", string(b))
				if ok {
					flusher.Flush()
				}
			}

			if cDelta != "" {
				filtered := toolFilter.Feed(cDelta)
				if filtered != "" {
					chunk := map[string]interface{}{
						"id":      chatCmplID,
						"object":  "chat.completion.chunk",
						"created": created,
						"model":   req.Model,
						"choices": []map[string]interface{}{
							{
								"index": 0,
								"delta": map[string]interface{}{
									"content": filtered,
								},
							},
						},
					}
					b, _ := json.Marshal(chunk)
					fmt.Fprintf(w, "data: %s\n\n", string(b))
					if ok {
						flusher.Flush()
					}
				}
			}
		}

		resp, err := s.driver.Ask(r.Context(), askReq, onDelta)
		if err != nil {
			errChunk := map[string]interface{}{
				"error": map[string]string{
					"message": err.Error(),
				},
			}
			b, _ := json.Marshal(errChunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			fmt.Fprintf(w, "data: [DONE]\n\n")
			if ok {
				flusher.Flush()
			}
			return
		}

		// Flush any remaining non-tool text
		if remaining := toolFilter.Flush(); remaining != "" {
			chunk := map[string]interface{}{
				"id":      chatCmplID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": remaining,
						},
					},
				},
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			if ok {
				flusher.Flush()
			}
		}

		// If tool calls were parsed, stream tool_calls delta chunk
		if len(resp.ToolCalls) > 0 {
			var tcList []map[string]interface{}
			for i, tc := range resp.ToolCalls {
				tcID := fmt.Sprintf("call_%d_%d", created, i)
				tcList = append(tcList, map[string]interface{}{
					"index": i,
					"id":    tcID,
					"type":  "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": tc.Arguments,
					},
				})
			}

			tcChunk := map[string]interface{}{
				"id":      chatCmplID,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   req.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"tool_calls": tcList,
						},
					},
				},
			}
			b, _ := json.Marshal(tcChunk)
			fmt.Fprintf(w, "data: %s\n\n", string(b))
			if ok {
				flusher.Flush()
			}
		}

		// Send proper final SSE chunk with finish_reason: "tool_calls" or "stop"
		finishReason := "stop"
		if len(resp.ToolCalls) > 0 {
			finishReason = "tool_calls"
		}
		finalChunk := map[string]interface{}{
			"id":      chatCmplID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   req.Model,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]interface{}{},
					"finish_reason": finishReason,
				},
			},
		}
		b, _ := json.Marshal(finalChunk)
		fmt.Fprintf(w, "data: %s\n\n", string(b))

		fmt.Fprintf(w, "data: [DONE]\n\n")
		if ok {
			flusher.Flush()
		}
		return
	}

	// Non-streaming response
	resp, err := s.driver.Ask(r.Context(), askReq, nil)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{
				"message": err.Error(),
			},
		})
		return
	}

	finishReason := "stop"
	content := resp.Content
	if len(resp.ToolCalls) > 0 {
		finishReason = "tool_calls"
		content = parser.StripToolCallBlocks(content)
	}

	message := map[string]interface{}{
		"role":    "assistant",
		"content": content,
	}

	if resp.Reasoning != "" {
		message["reasoning_content"] = resp.Reasoning
	}

	if len(resp.ToolCalls) > 0 {
		var tcList []map[string]interface{}
		for i, tc := range resp.ToolCalls {
			tcID := fmt.Sprintf("call_%d_%d", created, i)
			tcList = append(tcList, map[string]interface{}{
				"id":   tcID,
				"type": "function",
				"function": map[string]interface{}{
					"name":      tc.Name,
					"arguments": tc.Arguments,
				},
			})
		}
		message["tool_calls"] = tcList
	}

	out := map[string]interface{}{
		"id":              chatCmplID,
		"object":          "chat.completion",
		"created":         created,
		"model":           req.Model,
		"conversation_id": resp.ConversationID,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": finishReason,
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     len(question) / 4,
			"completion_tokens": len(content) / 4,
			"total_tokens":      (len(question) + len(content)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// streamToolFilter buffers and suppresses raw tool_call blocks from user-facing stream deltas.
type streamToolFilter struct {
	enabled     bool
	inToolBlock bool
	pending     string
}

func newStreamToolFilter(enabled bool) *streamToolFilter {
	return &streamToolFilter{enabled: enabled}
}

func (f *streamToolFilter) Feed(chunk string) string {
	if !f.enabled || chunk == "" {
		return chunk
	}

	text := f.pending + chunk
	f.pending = ""

	var output strings.Builder
	toolMarkers := []string{"```tool_call", "<tool_call"}

	for len(text) > 0 {
		if !f.inToolBlock {
			// Check if text ends with a partial prefix of any tool marker
			hasPartial := false
			for _, marker := range toolMarkers {
				for l := len(marker) - 1; l >= 1; l-- {
					if strings.HasSuffix(text, marker[:l]) {
						f.pending = marker[:l]
						text = text[:len(text)-l]
						hasPartial = true
						break
					}
				}
				if hasPartial {
					break
				}
			}

			// Look for tool block start
			idxTool := strings.Index(text, "```tool_call")
			idxXML := strings.Index(text, "<tool_call")

			startIdx := -1
			startLen := 0
			if idxTool != -1 && (idxXML == -1 || idxTool < idxXML) {
				startIdx = idxTool
				startLen = len("```tool_call")
			} else if idxXML != -1 {
				startIdx = idxXML
				startLen = len("<tool_call")
			}

			if startIdx != -1 {
				output.WriteString(text[:startIdx])
				text = text[startIdx+startLen:]
				f.inToolBlock = true
			} else {
				output.WriteString(text)
				text = ""
			}
		} else {
			// Look for tool block end
			idxCloseTick := strings.Index(text, "```")
			idxCloseXML := strings.Index(text, "</tool_call>")

			endIdx := -1
			endLen := 0
			if idxCloseTick != -1 && (idxCloseXML == -1 || idxCloseTick < idxCloseXML) {
				endIdx = idxCloseTick
				endLen = len("```")
			} else if idxCloseXML != -1 {
				endIdx = idxCloseXML
				endLen = len("</tool_call>")
			}

			if endIdx != -1 {
				text = text[endIdx+endLen:]
				f.inToolBlock = false
			} else {
				text = ""
			}
		}
	}

	return output.String()
}

func (f *streamToolFilter) Flush() string {
	if !f.enabled {
		return ""
	}
	res := ""
	if !f.inToolBlock {
		res = f.pending
	}
	f.pending = ""
	return res
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		sessions, err := s.driver.ListChats()
		if err != nil {
			http.Error(w, fmt.Sprintf("Failed to list chats: %v", err), http.StatusInternalServerError)
			return
		}
		if sessions == nil {
			sessions = []engine.ChatSession{}
		}
		curID := s.driver.CurrentConversationID()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"current_conversation_id": curID,
			"sessions":                sessions,
		})

	case http.MethodPost:
		var req struct {
			ConversationID string `json:"conversation_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ConversationID == "" {
			http.Error(w, "JSON body with 'conversation_id' is required", http.StatusBadRequest)
			return
		}
		if err := s.driver.SwitchChat(req.ConversationID); err != nil {
			http.Error(w, fmt.Sprintf("Failed to switch chat: %v", err), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"status":                  "ok",
			"current_conversation_id": req.ConversationID,
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	selector := r.URL.Query().Get("selector")
	fullPage := r.URL.Query().Get("full_page") == "true"
	raw := r.URL.Query().Get("raw") == "true"

	savedPath, data, err := s.driver.CaptureScreenshot("", engine.ScreenshotOptions{
		Selector: selector,
		FullPage: fullPage,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("Screenshot failed: %v", err), http.StatusInternalServerError)
		return
	}

	if raw {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"path":   savedPath,
		"size":   len(data),
	})
}
