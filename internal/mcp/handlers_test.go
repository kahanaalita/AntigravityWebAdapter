package mcp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"agy-gemini-web-mcp/internal/config"
	"agy-gemini-web-mcp/internal/geminiweb"
	"agy-gemini-web-mcp/internal/media"
)

func TestHandlerRegistration(t *testing.T) {
	s := server.NewMCPServer("test-server", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithPromptCapabilities(true),
	)

	client := geminiweb.NewClient(nil)
	scotty := media.NewScottyUploader(nil)
	resolver := media.NewImageResolver(t.TempDir())

	handler := NewServerHandler(client, scotty, resolver, map[config.ModelType]string{
		config.ModelHydrogen4: "hex_h4",
		config.ModelArgon4:    "hex_a4",
	})

	handler.Register(s)

	// Test Prompt Template for Hydrogen
	promptRes, err := handler.handlePromptTemplate("hydrogen-4", "middle", mcp.GetPromptRequest{
		Params: mcp.GetPromptParams{
			Arguments: map[string]string{
				"prompt": "Test prompt text",
				"effort": "high",
			},
			Name: "hydrogen",
		},
	})
	if err != nil {
		t.Fatalf("handlePromptTemplate failed: %v", err)
	}

	if len(promptRes.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(promptRes.Messages))
	}

	textContent, ok := mcp.AsTextContent(promptRes.Messages[0].Content)
	if !ok {
		t.Fatalf("Expected TextContent")
	}

	if !strings.Contains(textContent.Text, "gemini_pro_web_ask") || !strings.Contains(textContent.Text, "high") {
		t.Errorf("Prompt message mismatch: %s", textContent.Text)
	}

	// Test Empty prompt error in executeAsk
	callRes, err := handler.executeAsk(context.Background(), "hydrogen-4", "middle", map[string]any{
		"prompt": "   ",
	})
	if err != nil {
		t.Fatalf("executeAsk failed: %v", err)
	}
	if !callRes.IsError {
		t.Errorf("Expected error for empty prompt")
	}
}

func TestPrepareAskRequest_NoTruncation(t *testing.T) {
	tempDir := t.TempDir()
	resolver := media.NewImageResolver(tempDir)
	handler := NewServerHandler(nil, nil, resolver, nil)

	// 1. Prompt starting with "argon" - MUST NOT TRUNCATE "argon"
	argonPrompt := "argon is an inert noble gas with atomic number 18"
	req, _, err := handler.prepareAskRequest("hydrogen-4", "middle", map[string]any{
		"prompt": argonPrompt,
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.Prompt != argonPrompt {
		t.Errorf("Prompt was truncated! Expected %q, got %q", argonPrompt, req.Prompt)
	}
	if req.ModelConfig.Model != config.ModelHydrogen4 {
		t.Errorf("Model should remain hydrogen-4, got %v", req.ModelConfig.Model)
	}

	// 2. Prompt starting with "hydrogen" - MUST NOT TRUNCATE "hydrogen"
	hydrogenPrompt := "hydrogen fuel cells produce clean electrical energy"
	req, _, err = handler.prepareAskRequest("argon-4", "medium", map[string]any{
		"prompt": hydrogenPrompt,
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.Prompt != hydrogenPrompt {
		t.Errorf("Prompt was truncated! Expected %q, got %q", hydrogenPrompt, req.Prompt)
	}
	if req.ModelConfig.Model != config.ModelArgon4 {
		t.Errorf("Model should remain argon-4, got %v", req.ModelConfig.Model)
	}

	// 3. Prompt starting with "low" - MUST NOT TRUNCATE "low"
	lowPrompt := "low latency memory caching strategies"
	req, _, err = handler.prepareAskRequest("argon-4", "high", map[string]any{
		"prompt": lowPrompt,
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.Prompt != lowPrompt {
		t.Errorf("Prompt was truncated! Expected %q, got %q", lowPrompt, req.Prompt)
	}
	if req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Effort should remain high, got %v", req.ModelConfig.Effort)
	}

	// 4. Prompt starting with "high" - MUST NOT TRUNCATE "high"
	highPrompt := "high throughput network pipelines"
	req, _, err = handler.prepareAskRequest("hydrogen-4", "middle", map[string]any{
		"prompt": highPrompt,
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.Prompt != highPrompt {
		t.Errorf("Prompt was truncated! Expected %q, got %q", highPrompt, req.Prompt)
	}
	if req.ModelConfig.Effort != config.EffortMiddle {
		t.Errorf("Effort should remain middle, got %v", req.ModelConfig.Effort)
	}
}

func TestPrepareAskRequest_SlashCommandRouting(t *testing.T) {
	tempDir := t.TempDir()
	resolver := media.NewImageResolver(tempDir)
	handler := NewServerHandler(nil, nil, resolver, map[config.ModelType]string{
		config.ModelHydrogen4: "h4_hex",
		config.ModelArgon4:    "a4_hex",
	})

	// 1. Two-token slash command: /argon high
	req, _, err := handler.prepareAskRequest("", "", map[string]any{
		"prompt": "/argon high Спроектируй распределенную систему хранения",
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelArgon4 {
		t.Errorf("Expected Argon4, got %v", req.ModelConfig.Model)
	}
	if req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected EffortHigh, got %v", req.ModelConfig.Effort)
	}
	if req.Prompt != "Спроектируй распределенную систему хранения" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}

	// 2. Two-token slash command: /hydrogen middle
	req, _, err = handler.prepareAskRequest("argon-4", "high", map[string]any{
		"prompt": "/hydrogen middle Реши алгоритмическую задачу на графах",
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelHydrogen4 {
		t.Errorf("Slash command /hydrogen should route to Hydrogen4, got %v", req.ModelConfig.Model)
	}
	if req.ModelConfig.Effort != config.EffortMiddle {
		t.Errorf("Expected EffortMiddle, got %v", req.ModelConfig.Effort)
	}
	if req.Prompt != "Реши алгоритмическую задачу на графах" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}

	// 3. Compound slash command: /a-argon-high
	req, _, err = handler.prepareAskRequest("", "", map[string]any{
		"prompt": "/a-argon-high Глубокий синтез архитектуры микросервисов",
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelArgon4 || req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected Argon4 High, got %v, %v", req.ModelConfig.Model, req.ModelConfig.Effort)
	}
	if req.Prompt != "Глубокий синтез архитектуры микросервисов" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}

	// 4. Default model fallback when no model specified and no slash command
	req, _, err = handler.prepareAskRequest("", "", map[string]any{
		"prompt": "Какая сегодня погода?",
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelHydrogen4 {
		t.Errorf("Expected default hydrogen-4, got %v", req.ModelConfig.Model)
	}
	if req.Prompt != "Какая сегодня погода?" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}
}

func TestPrepareAskRequest_MultimodalResolution(t *testing.T) {
	tempDir := t.TempDir()
	imgPath := filepath.Join(tempDir, "architecture.png")
	if err := os.WriteFile(imgPath, []byte("fake-png-bytes"), 0644); err != nil {
		t.Fatalf("Failed to create temp image: %v", err)
	}

	resolver := media.NewImageResolver(tempDir)
	handler := NewServerHandler(nil, nil, resolver, nil)

	req, resolved, err := handler.prepareAskRequest("argon-4", "high", map[string]any{
		"prompt": "/argon high @architecture.png Проанализируй узкие места",
	})
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}

	if req.ModelConfig.Model != config.ModelArgon4 || req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected Argon4 High, got %v, %v", req.ModelConfig.Model, req.ModelConfig.Effort)
	}
	if req.Prompt != "Проанализируй узкие места" {
		t.Errorf("Expected cleaned prompt 'Проанализируй узкие места', got %q", req.Prompt)
	}
	if len(resolved) != 1 || resolved[0].FileName != "architecture.png" {
		t.Errorf("Expected resolved image architecture.png, got %v", resolved)
	}
}
