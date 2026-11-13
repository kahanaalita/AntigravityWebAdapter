package mcp

import (
	"context"
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
