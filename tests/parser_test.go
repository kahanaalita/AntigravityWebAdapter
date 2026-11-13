package tests

import (
	"testing"

	"github.com/kahanaalita/AntigravityWebAdapter/internal/parser"
)

func TestParseToolCalls(t *testing.T) {
	tools := []parser.ToolSchema{
		{
			Name:        "read_file",
			Description: "Read a file from disk",
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{
					"file_path": map[string]interface{}{"type": "string"},
				},
			},
		},
		{
			Name:        "run_command",
			Description: "Run bash command",
			Parameters: map[string]interface{}{
				"properties": map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
				},
			},
		},
	}

	tests := []struct {
		name     string
		input    string
		wantName string
	}{
		{
			name:     "Markdown tool_call code block",
			input:    "Here is the command:\n```tool_call\n{\n  \"name\": \"run_command\",\n  \"arguments\": {\"command\": \"ls -la\"}\n}\n```",
			wantName: "run_command",
		},
		{
			name:     "Tool name inferred from alias path->file_path",
			input:    "```json\n{\"path\": \"/etc/hosts\"}\n```",
			wantName: "read_file",
		},
		{
			name:     "Unescaped newline in JSON string",
			input:    "```tool_call\n{\n  \"name\": \"run_command\",\n  \"arguments\": {\"command\": \"echo 'line 1\nline 2'\"}\n}\n```",
			wantName: "run_command",
		},
		{
			name:     "Trailing comma and single quote comment",
			input:    "```tool_call\n{\n  // executing command\n  \"name\": \"run_command\",\n  \"arguments\": {\"command\": \"pwd\",},\n}\n```",
			wantName: "run_command",
		},
		{
			name:     "Thoughts tag with tool call outside",
			input:    "<model-thoughts>\nThinking about reading file...\n</model-thoughts>\n```tool_call\n{\"name\": \"read_file\", \"arguments\": {\"file_path\": \"a.txt\"}}\n```",
			wantName: "read_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := parser.ParseToolCalls(tt.input, tools)
			if len(calls) == 0 {
				t.Fatalf("expected tool call, got 0")
			}
			if calls[0].Name != tt.wantName {
				t.Errorf("got name %q, want %q", calls[0].Name, tt.wantName)
			}
		})
	}
}
