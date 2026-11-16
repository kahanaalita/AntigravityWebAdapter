package geminiweb

import (
	"encoding/json"
	"errors"
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

func TestBuildStreamGenerateBody_Locale(t *testing.T) {
	conv := ConversationContext{}

	// Default locale should be "ru"
	t.Setenv("GEMINI_LOCALE", "")
	bodyRu, err := BuildStreamGenerateBody("Test", nil, conv, config.TierLow, "token")
	if err != nil {
		t.Fatalf("BuildStreamGenerateBody failed: %v", err)
	}
	valsRu, _ := url.ParseQuery(bodyRu)
	freqRu := valsRu.Get("f.req")
	if !strings.Contains(freqRu, `[\"ru\"]`) {
		t.Errorf("Expected [\\\"ru\\\"] in default payload, got %s", freqRu)
	}

	// Custom locale
	t.Setenv("GEMINI_LOCALE", "en")
	bodyEn, err := BuildStreamGenerateBody("Test", nil, conv, config.TierLow, "token")
	if err != nil {
		t.Fatalf("BuildStreamGenerateBody failed: %v", err)
	}
	valsEn, _ := url.ParseQuery(bodyEn)
	freqEn := valsEn.Get("f.req")
	if !strings.Contains(freqEn, `[\"en\"]`) {
		t.Errorf("Expected [\\\"en\\\"] in custom payload, got %s", freqEn)
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

func TestParseStream_ThoughtBlocks(t *testing.T) {
	innerObj := []any{
		nil,
		[]any{"c_block_01", "r_block_02"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_block_03",
				[]any{":::thought\nРазмышления внутри блока :::thought\nШаг 1\n:::\nОсновной текст ответа."},
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

	expectedThought := "Размышления внутри блока :::thought\nШаг 1"
	if resp.Thought != expectedThought {
		t.Errorf("Expected thought %q, got %q", expectedThought, resp.Thought)
	}
	if resp.Text != "Основной текст ответа." {
		t.Errorf("Expected text 'Основной текст ответа.', got %q", resp.Text)
	}
}

func TestParseStream_ReasoningAndThoughtTraceKeys(t *testing.T) {
	innerObj := []any{
		nil,
		[]any{"c_key_01", "r_key_02"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_key_03",
				[]any{"Финальный ответ"},
				map[string]any{"reasoning": "Логический вывод через reasoning ключ"},
			},
			[]any{
				"rc_key_04",
				[]any{"Финальный ответ 2"},
				map[string]any{"thought_trace": "Трассировка через thought_trace ключ"},
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

	if !strings.Contains(resp.Thought, "Логический вывод через reasoning ключ") {
		t.Errorf("Expected reasoning in thought, got: %q", resp.Thought)
	}
	if !strings.Contains(resp.Thought, "Трассировка через thought_trace ключ") {
		t.Errorf("Expected thought_trace in thought, got: %q", resp.Thought)
	}
}

func TestParseStream_MixedThoughtTagAndBlock(t *testing.T) {
	innerObj := []any{
		nil,
		[]any{"c_mix_01", "r_mix_02"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_mix_03",
				[]any{":::thought\nБлок рассуждений 1\n:::\n<thought>Тег рассуждений 2</thought>\nИтоговый результат."},
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

	if !strings.Contains(resp.Thought, "Блок рассуждений 1") {
		t.Errorf("Missing block thought, got: %q", resp.Thought)
	}
	if !strings.Contains(resp.Thought, "Тег рассуждений 2") {
		t.Errorf("Missing tag thought, got: %q", resp.Thought)
	}
	if resp.Text != "Итоговый результат." {
		t.Errorf("Expected text 'Итоговый результат.', got %q", resp.Text)
	}
}

func TestParseStream_EmptyOrInvalidReturnsErrEmptyParsedResponse(t *testing.T) {
	testCases := []struct {
		name  string
		input string
	}{
		{
			name:  "completely empty input",
			input: "",
		},
		{
			name:  "html error page 403",
			input: "<!DOCTYPE html><html><head><title>403 Forbidden</title></head><body>Access Denied</body></html>",
		},
		{
			name:  "magic prefix only",
			input: ")]}'\n[]\n",
		},
		{
			name:  "rpc envelope with empty inner array",
			input: ")]}'\n[[\"wrb.fr\",null,\"[]\"]]\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := ParseStream(strings.NewReader(tc.input))
			if err == nil {
				t.Fatalf("Expected error for %s, got nil (resp: %+v)", tc.name, resp)
			}
			if !errors.Is(err, ErrEmptyParsedResponse) {
				t.Errorf("Expected ErrEmptyParsedResponse, got: %v", err)
			}
		})
	}
}

func TestParseStream_PrimaryCandidateNotOverwritten(t *testing.T) {
	// Candidate 0 should be chosen, candidate 1 (draft 2) should NOT overwrite choice_id or text
	innerObj := []any{
		nil,
		[]any{"c_multi_01", "r_multi_02"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_primary_draft_01",
				[]any{"Основной черновик (кандидат 0)."},
			},
			[]any{
				"rc_secondary_draft_02",
				[]any{"Вторичный черновик (кандидат 1, должен быть проигнорирован)."},
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

	if resp.ChoiceID != "rc_primary_draft_01" {
		t.Errorf("Expected ChoiceID 'rc_primary_draft_01', got %q", resp.ChoiceID)
	}
	if resp.Text != "Основной черновик (кандидат 0)." {
		t.Errorf("Expected primary candidate text, got %q", resp.Text)
	}
}

func TestParseStream_CandidateAccumulatesAllParts(t *testing.T) {
	// Candidate with multiple text parts in cand[1]
	innerObj := []any{
		nil,
		[]any{"c_parts_01", "r_parts_02"},
		nil,
		nil,
		[]any{
			[]any{
				"rc_parts_03",
				[]any{
					"Часть 1. ",
					"Часть 2. ",
					[]any{"Подчасть 3. ", "Подчасть 4."},
				},
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

	expected := "Часть 1. Часть 2. Подчасть 3. Подчасть 4."
	if resp.Text != expected {
		t.Errorf("Expected accumulated text %q, got %q", expected, resp.Text)
	}
}
