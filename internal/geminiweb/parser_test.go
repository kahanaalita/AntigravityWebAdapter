package geminiweb

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"agy-gemini-web-mcp/internal/config"
)

func TestBuildExtensionHeader(t *testing.T) {
	hdrLow := BuildExtensionHeader("hydrogen-4", config.TierLow)
	var arrLow []any
	if err := json.Unmarshal([]byte(hdrLow), &arrLow); err != nil {
		t.Fatalf("Unmarshal hdrLow failed: %v", err)
	}
	if arrLow[4] != "hydrogen-4" || arrLow[7] != float64(1) {
		t.Errorf("hdrLow mismatch: %s", hdrLow)
	}

	hdrHigh := BuildExtensionHeader("argon-4", config.TierHigh)
	var arrHigh []any
	if err := json.Unmarshal([]byte(hdrHigh), &arrHigh); err != nil {
		t.Fatalf("Unmarshal hdrHigh failed: %v", err)
	}
	if arrHigh[4] != "argon-4" || arrHigh[7] != float64(3) {
		t.Errorf("hdrHigh mismatch: %s", hdrHigh)
	}
}

func TestBuildStreamGenerateBody(t *testing.T) {
	conv := ConversationContext{
		ConversationID: "c_12345",
		ResponseID:     "r_67890",
		ChoiceID:       "rc_abcde",
	}

	attachments := []ScottyAttachment{
		{
			UploadRef: "/contrib_service/ttl_1d/test_ref",
			FileName:  "diagram.png",
			MimeType:  "image/png",
		},
	}

	body, err := BuildStreamGenerateBody("Hello world", attachments, conv, config.TierMiddle, "test_csrf_token")
	if err != nil {
		t.Fatalf("BuildStreamGenerateBody failed: %v", err)
	}

	values, err := url.ParseQuery(body)
	if err != nil {
		t.Fatalf("ParseQuery failed: %v", err)
	}

	if values.Get("at") != "test_csrf_token" {
		t.Errorf("Expected csrf token 'test_csrf_token', got %q", values.Get("at"))
	}

	freq := values.Get("f.req")
	if !strings.Contains(freq, "Hello world") || !strings.Contains(freq, "diagram.png") || !strings.Contains(freq, "c_12345") {
		t.Errorf("f.req missing content: %s", freq)
	}
}

func TestParseStream_WithThinkingAndText(t *testing.T) {
	// Mock Google RPC stream
	innerObj := []any{
		nil,
		[]any{"c_conv_001", "r_resp_002"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_choice_003",
				[]any{"Привет! Это финальный ответ модели."},
				map[string]any{"thought": "Анализирую входные данные...\nШаг 1: Проверяю параметры.\nШаг 2: Формирую ответ."},
			},
		},
	}
	innerBytes, _ := json.Marshal(innerObj)

	envelope := [][]any{
		{"wrb.fr", nil, string(innerBytes)},
	}
	envelopeBytes, _ := json.Marshal(envelope)

	rawStream := ")]}'\n" + string(envelopeBytes) + "\n"

	resp, err := ParseStream(strings.NewReader(rawStream))
	if err != nil {
		t.Fatalf("ParseStream failed: %v", err)
	}

	if resp.ConversationID != "c_conv_001" {
		t.Errorf("Expected ConversationID 'c_conv_001', got %q", resp.ConversationID)
	}
	if resp.ResponseID != "r_resp_002" {
		t.Errorf("Expected ResponseID 'r_resp_002', got %q", resp.ResponseID)
	}
	if resp.ChoiceID != "rc_choice_003" {
		t.Errorf("Expected ChoiceID 'rc_choice_003', got %q", resp.ChoiceID)
	}
	if !strings.Contains(resp.Text, "Привет! Это финальный ответ модели.") {
		t.Errorf("Unexpected text: %q", resp.Text)
	}
	if !strings.Contains(resp.Thought, "Анализирую входные данные...") {
		t.Errorf("Unexpected thought: %q", resp.Thought)
	}

	formatted := resp.FormattedOutput()
	if !strings.Contains(formatted, "<details>\n<summary>Thinking Process</summary>") {
		t.Errorf("Formatted output missing details: %s", formatted)
	}
	if !strings.Contains(formatted, "Привет! Это финальный ответ модели.") {
		t.Errorf("Formatted output missing text: %s", formatted)
	}
}

func TestParseStream_EmbeddedThoughtTags(t *testing.T) {
	innerObj := []any{
		nil,
		[]any{"c_999", "r_888"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_777",
				[]any{"<thought>Глубокие мысли внутри тега</thought>Ответ за пределами тега."},
			},
		},
	}
	innerBytes, _ := json.Marshal(innerObj)
	envelope := [][]any{{"wrb.fr", nil, string(innerBytes)}}
	envelopeBytes, _ := json.Marshal(envelope)
	rawStream := ")]}'\n" + string(envelopeBytes) + "\n"

	resp, err := ParseStream(strings.NewReader(rawStream))
	if err != nil {
		t.Fatalf("ParseStream failed: %v", err)
	}

	if resp.Thought != "Глубокие мысли внутри тега" {
		t.Errorf("Expected thought 'Глубокие мысли внутри тега', got %q", resp.Thought)
	}
	if resp.Text != "Ответ за пределами тега." {
		t.Errorf("Expected text 'Ответ за пределами тега.', got %q", resp.Text)
	}
}
