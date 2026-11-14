package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/engine"
)

func TestCleanConversationID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "bare ID",
			input:    "c_12345abc",
			expected: "c_12345abc",
		},
		{
			name:     "with whitespace",
			input:    "   c_12345abc   ",
			expected: "c_12345abc",
		},
		{
			name:     "with /app/ prefix",
			input:    "/app/c_12345abc",
			expected: "c_12345abc",
		},
		{
			name:     "with trailing slash",
			input:    "/app/c_12345abc/",
			expected: "c_12345abc",
		},
		{
			name:     "full Gemini URL",
			input:    "https://gemini.google.com/app/c_12345abc",
			expected: "c_12345abc",
		},
		{
			name:     "full Gemini URL with trailing slash",
			input:    "https://gemini.google.com/app/c_12345abc/",
			expected: "c_12345abc",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "only whitespace",
			input:    "    ",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := engine.CleanConversationID(tc.input)
			if actual != tc.expected {
				t.Fatalf("CleanConversationID(%q) = %q; want %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestParseConversationIDFromPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "standard app path",
			path:     "/app/c_987654321",
			expected: "c_987654321",
		},
		{
			name:     "app path with trailing slash",
			path:     "/app/c_987654321/",
			expected: "c_987654321",
		},
		{
			name:     "app path with subresource",
			path:     "/app/c_987654321/view",
			expected: "c_987654321",
		},
		{
			name:     "just /app",
			path:     "/app",
			expected: "",
		},
		{
			name:     "root path",
			path:     "/",
			expected: "",
		},
		{
			name:     "non-app path",
			path:     "/u/0/settings",
			expected: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := engine.ParseConversationIDFromPath(tc.path)
			if actual != tc.expected {
				t.Fatalf("ParseConversationIDFromPath(%q) = %q; want %q", tc.path, actual, tc.expected)
			}
		})
	}
}

func TestChatSession_Serialization(t *testing.T) {
	session := engine.ChatSession{
		ID:    "c_abc123xyz",
		Title: "Refactoring Go Architecture",
		URL:   "https://gemini.google.com/app/c_abc123xyz",
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("failed to marshal ChatSession: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal into map: %v", err)
	}

	if jsonMap["id"] != session.ID {
		t.Errorf("expected json id %q, got %q", session.ID, jsonMap["id"])
	}
	if jsonMap["title"] != session.Title {
		t.Errorf("expected json title %q, got %q", session.Title, jsonMap["title"])
	}
	if jsonMap["url"] != session.URL {
		t.Errorf("expected json url %q, got %q", session.URL, jsonMap["url"])
	}

	var roundtrip engine.ChatSession
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to roundtrip unmarshal: %v", err)
	}

	if roundtrip != session {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", roundtrip, session)
	}
}

func TestParseChatSessionsFromRaw(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		res := engine.ParseChatSessionsFromRaw(nil)
		if res != nil {
			t.Fatalf("expected nil result, got %v", res)
		}
	})

	t.Run("invalid type input", func(t *testing.T) {
		res := engine.ParseChatSessionsFromRaw("not a slice")
		if res != nil {
			t.Fatalf("expected nil result, got %v", res)
		}
	})

	t.Run("valid and malformed items", func(t *testing.T) {
		raw := []interface{}{
			map[string]interface{}{
				"id":    "c_1",
				"title": "Chat 1",
				"url":   "https://gemini.google.com/app/c_1",
			},
			"invalid item type",
			map[string]interface{}{
				"id":    "",
				"title": "Empty ID",
				"url":   "https://gemini.google.com/app/",
			},
			map[string]interface{}{
				"id":    "c_2",
				"title": "",
				"url":   "https://gemini.google.com/app/c_2",
			},
			map[string]interface{}{
				"id":    "c_3",
				"title": "Chat 3",
				"url":   "https://gemini.google.com/app/c_3",
			},
		}

		sessions := engine.ParseChatSessionsFromRaw(raw)
		if len(sessions) != 2 {
			t.Fatalf("expected 2 valid sessions, got %d", len(sessions))
		}

		if sessions[0].ID != "c_1" || sessions[0].Title != "Chat 1" {
			t.Errorf("session[0] mismatch: %+v", sessions[0])
		}
		if sessions[1].ID != "c_3" || sessions[1].Title != "Chat 3" {
			t.Errorf("session[1] mismatch: %+v", sessions[1])
		}
	})
}

func TestScreenshotOptions_Serialization(t *testing.T) {
	opts := engine.ScreenshotOptions{
		Selector: ".main-content",
		FullPage: true,
		Format:   "jpeg",
		Quality:  90,
	}

	data, err := json.Marshal(opts)
	if err != nil {
		t.Fatalf("failed to marshal ScreenshotOptions: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(data, &jsonMap); err != nil {
		t.Fatalf("failed to unmarshal into map: %v", err)
	}

	if jsonMap["selector"] != opts.Selector {
		t.Errorf("expected selector %q, got %q", opts.Selector, jsonMap["selector"])
	}
	if jsonMap["full_page"] != opts.FullPage {
		t.Errorf("expected full_page %v, got %v", opts.FullPage, jsonMap["full_page"])
	}
	if jsonMap["format"] != opts.Format {
		t.Errorf("expected format %q, got %q", opts.Format, jsonMap["format"])
	}
	if int(jsonMap["quality"].(float64)) != opts.Quality {
		t.Errorf("expected quality %d, got %v", opts.Quality, jsonMap["quality"])
	}

	// Test omitempty on empty options
	emptyOpts := engine.ScreenshotOptions{}
	emptyData, err := json.Marshal(emptyOpts)
	if err != nil {
		t.Fatalf("failed to marshal empty ScreenshotOptions: %v", err)
	}
	if string(emptyData) != "{}" {
		t.Errorf("expected '{}', got %s", string(emptyData))
	}
}

func TestAskResponse_ConversationID(t *testing.T) {
	resp := engine.AskResponse{
		Content:        "Hello world",
		Reasoning:      "Thinking step",
		ConversationID: "c_session_456",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal AskResponse: %v", err)
	}

	var roundtrip engine.AskResponse
	if err := json.Unmarshal(data, &roundtrip); err != nil {
		t.Fatalf("failed to unmarshal AskResponse: %v", err)
	}

	if roundtrip.ConversationID != "c_session_456" {
		t.Errorf("expected conversation_id 'c_session_456', got %q", roundtrip.ConversationID)
	}
}
