package tests

import (
	"encoding/json"
	"strings"
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

func TestFixInvalidEscapes_WindowsPath(t *testing.T) {
	// Problem N9: Windows path ending with double backslash before closing quote
	input := `{"path": "C:\\Users\\admin\\"}`
	fixed := parser.FixInvalidEscapes(input)
	var m map[string]string
	if err := json.Unmarshal([]byte(fixed), &m); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v, fixed was: %s", err, fixed)
	}
	expected := `C:\Users\admin\`
	if m["path"] != expected {
		t.Errorf("got %q, want %q", m["path"], expected)
	}

	// Also verify unescaped single backslash Windows path is repaired
	input2 := `{"path": "C:\Program Files\App"}`
	fixed2 := parser.FixInvalidEscapes(input2)
	var m2 map[string]string
	if err := json.Unmarshal([]byte(fixed2), &m2); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v, fixed was: %s", err, fixed2)
	}
	if m2["path"] != `C:\Program Files\App` {
		t.Errorf("got %q, want %q", m2["path"], `C:\Program Files\App`)
	}
}

func TestParseToolCalls_MultipleSequential(t *testing.T) {
	tools := []parser.ToolSchema{
		{Name: "read_file", Description: "Read a file"},
		{Name: "run_command", Description: "Run bash command"},
	}
	input := "Let me check the files:\n" +
		"```tool_call\n{\n  \"name\": \"read_file\",\n  \"arguments\": {\"file_path\": \"first.txt\"}\n}\n```\n" +
		"And run the command:\n" +
		"```tool_call\n{\n  \"name\": \"run_command\",\n  \"arguments\": {\"command\": \"cat second.txt\"}\n}\n```"

	calls := parser.ParseToolCalls(input, tools)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
	if calls[0].Name != "read_file" {
		t.Errorf("call 0: got %s, want read_file", calls[0].Name)
	}
	if !strings.Contains(calls[0].Arguments, "first.txt") {
		t.Errorf("call 0: expected arguments to contain first.txt, got %s", calls[0].Arguments)
	}
	if calls[1].Name != "run_command" {
		t.Errorf("call 1: got %s, want run_command", calls[1].Name)
	}
	if !strings.Contains(calls[1].Arguments, "cat second.txt") {
		t.Errorf("call 1: expected arguments to contain cat second.txt, got %s", calls[1].Arguments)
	}
}

func TestStripToolCallBlocks(t *testing.T) {
	input := "# Summary\n\n" +
		"Here is the command to run:\n" +
		"```tool_call\n{\n  \"name\": \"run_command\",\n  \"arguments\": {\"command\": \"ls -la\"}\n}\n```\n\n" +
		"Here is a regular python snippet:\n" +
		"```python\nprint('hello world')\n```\n\n" +
		"<tool_call>\n{\"name\": \"read_file\", \"arguments\": {\"file_path\": \"foo.go\"}}\n</tool_call>\n\n" +
		"And some JSON data that is not a tool call:\n" +
		"```json\n{\"setting\": \"value\", \"count\": 42}\n```\n\n" +
		"All done!"

	stripped := parser.StripToolCallBlocks(input)

	if strings.Contains(stripped, "tool_call") {
		t.Errorf("stripped text still contains tool_call: %s", stripped)
	}
	if strings.Contains(stripped, "run_command") {
		t.Errorf("stripped text still contains run_command: %s", stripped)
	}
	if strings.Contains(stripped, "foo.go") {
		t.Errorf("stripped text still contains foo.go: %s", stripped)
	}

	// Verify retained non-tool content
	if !strings.Contains(stripped, "# Summary") {
		t.Errorf("stripped text lost # Summary")
	}
	if !strings.Contains(stripped, "print('hello world')") {
		t.Errorf("stripped text lost python block")
	}
	if !strings.Contains(stripped, `{"setting": "value", "count": 42}`) {
		t.Errorf("stripped text lost regular json block: %s", stripped)
	}
	if !strings.Contains(stripped, "All done!") {
		t.Errorf("stripped text lost All done!")
	}
}
