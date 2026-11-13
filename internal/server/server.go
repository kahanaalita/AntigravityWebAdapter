package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"path/filepath"
	"time"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/models"
	"github.com/kahanaalita/AntigravityWebAdapter/internal/multimodal"
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
	for _, m := range req.Messages {
		if parts, ok := m.Content.([]interface{}); ok {
			imgs := multimodal.ExtractImagesFromParts(parts, tmpDir)
			imageFiles = append(imageFiles, imgs...)
		}
	}

	// Format prompt context
	isIncremental := len(req.Messages) > 2 && req.Messages[len(req.Messages)-1].Role == "tool"
	var question string
	if isIncremental {
		question = prompt.BuildIncrementalContext(req.Messages)
	} else {
		question = prompt.BuildFullContext(req.Messages, req.Tools)
	}

	askReq := engine.AskRequest{
		Question: question,
		Model:    domModel,
		Effort:   effort,
		Files:    imageFiles,
		Tools:    req.Tools,
	}

	created := time.Now().Unix()
	chatCmplID := fmt.Sprintf("chatcmpl-%d-%x", created, rand.Int31())

	if req.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		flusher, ok := w.(http.Flusher)

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
				chunk := map[string]interface{}{
					"id":      chatCmplID,
					"object":  "chat.completion.chunk",
					"created": created,
					"model":   req.Model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"content": cDelta,
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
		}

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

	message := map[string]interface{}{
		"role":    "assistant",
		"content": resp.Content,
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
		"id":      chatCmplID,
		"object":  "chat.completion",
		"created": created,
		"model":   req.Model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"message":       message,
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     len(question) / 4,
			"completion_tokens": len(resp.Content) / 4,
			"total_tokens":      (len(question) + len(resp.Content)) / 4,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}
