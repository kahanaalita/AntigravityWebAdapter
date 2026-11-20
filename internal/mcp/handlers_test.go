package mcp

import (
	"context"
	"errors"
	"fmt"
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
		config.ModelNeon4:     "hex_n4",
	})

	handler.Register(s)

	expectedDescriptions := map[string]string{
		"hydrogen_low":    "Hydrogen 4 with low reasoning effort (fast response, 20m streaming timeout, 5m idle)",
		"hydrogen_middle": "Hydrogen 4 with middle reasoning effort (balanced analysis, 20m streaming timeout, 5m idle)",
		"hydrogen_high":   "Hydrogen 4 with high reasoning effort (deep analysis, 20m streaming timeout, 5m idle)",
		"neon_low":       "Neon 4 with low reasoning effort (fast response, 20m streaming timeout, 5m idle)",
		"neon_medium":    "Neon 4 with medium reasoning effort (balanced synthesis, 20m streaming timeout, 5m idle)",
		"neon_high":      "Neon 4 with high reasoning effort (deep synthesis, 20m streaming timeout, 5m idle)",
	}

	for toolName, expectedDesc := range expectedDescriptions {
		tTool := s.GetTool(toolName)
		if tTool == nil {
			t.Errorf("Tool %s not registered", toolName)
			continue
		}
		if tTool.Tool.Description != expectedDesc {
			t.Errorf("Tool %s description mismatch:\nexpected: %s\ngot:      %s", toolName, expectedDesc, tTool.Tool.Description)
		}
	}

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
	}, false)
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
	}, true)
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
	req, _, err = handler.prepareAskRequest("neon-4", "medium", map[string]any{
		"prompt": hydrogenPrompt,
	}, true)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.Prompt != hydrogenPrompt {
		t.Errorf("Prompt was truncated! Expected %q, got %q", hydrogenPrompt, req.Prompt)
	}
	if req.ModelConfig.Model != config.ModelNeon4 {
		t.Errorf("Model should remain neon-4, got %v", req.ModelConfig.Model)
	}

	// 3. Prompt starting with "low" - MUST NOT TRUNCATE "low"
	lowPrompt := "low latency memory caching strategies"
	req, _, err = handler.prepareAskRequest("neon-4", "high", map[string]any{
		"prompt": lowPrompt,
	}, true)
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
	}, true)
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
		config.ModelNeon4:     "n4_hex",
	})

	// 1. Two-token slash command: /neon high
	req, _, err := handler.prepareAskRequest("", "", map[string]any{
		"prompt": "/neon high Спроектируй распределенную систему хранения",
	}, true)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelNeon4 {
		t.Errorf("Expected Neon4, got %v", req.ModelConfig.Model)
	}
	if req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected EffortHigh, got %v", req.ModelConfig.Effort)
	}
	if req.Prompt != "Спроектируй распределенную систему хранения" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}

	// 2. Two-token slash command: /hydrogen middle
	req, _, err = handler.prepareAskRequest("neon-4", "high", map[string]any{
		"prompt": "/hydrogen middle Реши алгоритмическую задачу на графах",
	}, true)
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

	// 3. Compound slash command: /a-neon-high
	req, _, err = handler.prepareAskRequest("", "", map[string]any{
		"prompt": "/a-neon-high Глубокий синтез архитектуры микросервисов",
	}, true)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelNeon4 || req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected Neon4 High, got %v, %v", req.ModelConfig.Model, req.ModelConfig.Effort)
	}
	if req.Prompt != "Глубокий синтез архитектуры микросервисов" {
		t.Errorf("Prompt mismatch: %q", req.Prompt)
	}

	// 4. Default model fallback when no model specified and no slash command
	req, _, err = handler.prepareAskRequest("", "", map[string]any{
		"prompt": "Какая сегодня погода?",
	}, true)
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

	req, resolved, err := handler.prepareAskRequest("neon-4", "high", map[string]any{
		"prompt": "/neon high @architecture.png Проанализируй узкие места",
	}, true)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}

	if req.ModelConfig.Model != config.ModelNeon4 || req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Expected Neon4 High, got %v, %v", req.ModelConfig.Model, req.ModelConfig.Effort)
	}
	if req.Prompt != "Проанализируй узкие места" {
		t.Errorf("Expected cleaned prompt 'Проанализируй узкие места', got %q", req.Prompt)
	}
	if len(resolved) != 1 || resolved[0].FileName != "architecture.png" {
		t.Errorf("Expected resolved image architecture.png, got %v", resolved)
	}
}

func TestAtomicTool_PreventsSlashOverride(t *testing.T) {
	tempDir := t.TempDir()
	resolver := media.NewImageResolver(tempDir)
	handler := NewServerHandler(nil, nil, resolver, map[config.ModelType]string{
		config.ModelHydrogen4: "h4_hex",
		config.ModelNeon4:     "n4_hex",
	})

	// 1. Invoking atomic tool neon_high with prompt starting with /hydrogen low must NOT switch model or effort
	req, _, err := handler.prepareAskRequest("neon-4", "high", map[string]any{
		"prompt": "/hydrogen low test prompt",
	}, false)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if req.ModelConfig.Model != config.ModelNeon4 {
		t.Errorf("Model was overridden! Expected %v, got %v", config.ModelNeon4, req.ModelConfig.Model)
	}
	if req.ModelConfig.Effort != config.EffortHigh {
		t.Errorf("Effort was overridden! Expected %v, got %v", config.EffortHigh, req.ModelConfig.Effort)
	}
	if req.Prompt != "/hydrogen low test prompt" {
		t.Errorf("Prompt should be preserved as user prompt in atomic tool, got %q", req.Prompt)
	}

	// 2. Invoking atomic tool hydrogen_low with prompt starting with /neon high must NOT switch model or effort
	reqH, _, err := handler.prepareAskRequest("hydrogen-4", "low", map[string]any{
		"prompt": "/neon high complex analysis",
	}, false)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if reqH.ModelConfig.Model != config.ModelHydrogen4 {
		t.Errorf("Model was overridden! Expected %v, got %v", config.ModelHydrogen4, reqH.ModelConfig.Model)
	}
	if reqH.ModelConfig.Effort != config.EffortLow {
		t.Errorf("Effort was overridden! Expected %v, got %v", config.EffortLow, reqH.ModelConfig.Effort)
	}
	if reqH.Prompt != "/neon high complex analysis" {
		t.Errorf("Prompt should be preserved as user prompt in atomic tool, got %q", reqH.Prompt)
	}

	// 3. By contrast, unified ask (allowSlashOverride: true) DOES route slash command
	reqUnified, _, err := handler.prepareAskRequest("neon-4", "high", map[string]any{
		"prompt": "/hydrogen low test prompt",
	}, true)
	if err != nil {
		t.Fatalf("prepareAskRequest failed: %v", err)
	}
	if reqUnified.ModelConfig.Model != config.ModelHydrogen4 {
		t.Errorf("Unified ask should allow slash command to route model, got %v", reqUnified.ModelConfig.Model)
	}
	if reqUnified.ModelConfig.Effort != config.EffortLow {
		t.Errorf("Unified ask should allow slash command to route effort, got %v", reqUnified.ModelConfig.Effort)
	}
	if reqUnified.Prompt != "test prompt" {
		t.Errorf("Unified ask should strip slash command token, got %q", reqUnified.Prompt)
	}
}

func TestFormatAuthRequiredHelp(t *testing.T) {
	testErr := fmt.Errorf("%w: redirected to accounts.google.com", geminiweb.ErrAuthenticationRequired)
	msg := FormatAuthRequiredHelp(testErr)

	if !strings.Contains(msg, "AUTH_REQUIRED") {
		t.Errorf("Message missing AUTH_REQUIRED title: %s", msg)
	}
	if !strings.Contains(msg, "--remote-debugging-port=9222") {
		t.Errorf("Message missing CDP instructions: %s", msg)
	}
	if !strings.Contains(msg, "cookies.json") {
		t.Errorf("Message missing cookies.json instructions: %s", msg)
	}
	if !strings.Contains(msg, "redirected to accounts.google.com") {
		t.Errorf("Message missing original error detail: %s", msg)
	}
}

func TestExecuteAsk_AuthRequiredHandling(t *testing.T) {
	// A handler with nil session will fail Generate with auth error when invoked
	client := geminiweb.NewClient(nil)
	scotty := media.NewScottyUploader(nil)
	resolver := media.NewImageResolver(t.TempDir())
	handler := NewServerHandler(client, scotty, resolver, nil)

	// Direct call to isAuthError
	if !isAuthError(geminiweb.ErrAuthenticationRequired) {
		t.Errorf("isAuthError must recognize geminiweb.ErrAuthenticationRequired")
	}
	customAuthErr := errors.New("gemini session expired: redirected to accounts.google.com")
	if !isAuthError(customAuthErr) {
		t.Errorf("isAuthError must recognize session expired error")
	}
	nonAuthErr := errors.New("syntax error in prompt")
	if isAuthError(nonAuthErr) {
		t.Errorf("isAuthError must not match non-auth error")
	}

	// Calling executeAsk where client.Generate fails with auth error
	res, err := handler.executeAsk(context.Background(), "hydrogen-4", "low", map[string]any{
		"prompt": "Hello Gemini",
	}, false)
	if err != nil {
		t.Fatalf("executeAsk returned unexpected fatal error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("Expected IsError to be true")
	}

	textContent, ok := mcp.AsTextContent(res.Content[0])
	if !ok {
		t.Fatalf("Expected TextContent in tool result")
	}
	if !strings.Contains(textContent.Text, "AUTH_REQUIRED") && !strings.Contains(textContent.Text, "gemini web rpc generation") {
		t.Errorf("Unexpected error text: %s", textContent.Text)
	}
}
