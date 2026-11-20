package geminiweb

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"regexp"
	"strings"
)

var (
	ErrEmptyParsedResponse = errors.New("empty response or failed to parse Gemini stream")
	magicPrefix            = []byte(")]}'\n")
	reThoughtTag           = regexp.MustCompile(`(?is)<thought(?:\s+[^>]*)?>(.*?)</thought>`)
	reThoughtBlock         = regexp.MustCompile(`(?is):::thought\s*(.*?)(?:\n\s*:::|\s+:::)(?:[\r\n\s]+|$)`)
)

// thoughtCollector manages deduplication and ordered aggregation of reasoning blocks.
type thoughtCollector struct {
	seen  map[string]struct{}
	parts []string
}

func newThoughtCollector() *thoughtCollector {
	return &thoughtCollector{seen: make(map[string]struct{})}
}

func (tc *thoughtCollector) Add(thought string) {
	trimmed := strings.TrimSpace(thought)
	if trimmed == "" {
		return
	}
	if _, exists := tc.seen[trimmed]; exists {
		return
	}
	tc.seen[trimmed] = struct{}{}

	// Check if this new thought extends an existing part or is subsumed
	for i, existing := range tc.parts {
		if strings.HasPrefix(trimmed, existing) && len(trimmed) > len(existing) {
			tc.parts[i] = trimmed
			return
		}
		if strings.Contains(existing, trimmed) {
			return
		}
	}

	tc.parts = append(tc.parts, trimmed)
}

func (tc *thoughtCollector) String() string {
	return strings.Join(tc.parts, "\n\n")
}

func (tc *thoughtCollector) Reset() {
	tc.seen = make(map[string]struct{})
	tc.parts = nil
}

// textAccumulator prevents data loss during chunk streaming by distinguishing
// cumulative replacements from incremental delta segments (such as Code Execution or Grounding).
type textAccumulator struct {
	committed      string // completed, immutable segments
	currentSegment string // active cumulative segment
}

func (ta *textAccumulator) Append(candText string) {
	if candText == "" {
		return
	}

	fullCurrent := ta.String()
	if fullCurrent == "" {
		ta.currentSegment = candText
		return
	}

	// 1. Fully consolidated replacement: candText contains full accumulated text as prefix
	if strings.HasPrefix(candText, fullCurrent) {
		ta.committed = ""
		ta.currentSegment = candText
		return
	}

	// 2. Cumulative chunk within current segment: candText extends currentSegment
	if strings.HasPrefix(candText, ta.currentSegment) {
		ta.currentSegment = candText
		return
	}

	// 3. Subsumed chunk: already present in full text or current segment
	if strings.HasPrefix(fullCurrent, candText) || strings.HasPrefix(ta.currentSegment, candText) {
		return
	}

	// 4. Delta or new fragment (e.g., Code Execution, Grounding, or multi-stage response):
	// Commit current segment and start a new one, avoiding blindly resetting the buffer.
	ta.committed = fullCurrent
	ta.currentSegment = candText
}

func (ta *textAccumulator) String() string {
	if ta.committed == "" {
		return ta.currentSegment
	}
	if ta.currentSegment == "" {
		return ta.committed
	}
	return ta.committed + ta.currentSegment
}

// ParsedResponse holds the consolidated output from StreamGenerate.
type ParsedResponse struct {
	ConversationID string
	ResponseID     string
	ChoiceID       string
	Thought        string
	Text           string
	RawChunks      []string
	Truncated      bool
	Warning        string
}

// FormattedOutput renders the markdown output suitable for Antigravity,
// keeping the reasoning trace cleanly folded inside a <details> block.
func (r *ParsedResponse) FormattedOutput() string {
	var b strings.Builder
	thought := strings.TrimSpace(r.Thought)
	if thought != "" {
		b.WriteString("<details>\n<summary>Thinking Process</summary>\n\n")
		b.WriteString(thought)
		b.WriteString("\n\n</details>\n\n")
	}

	text := strings.TrimSpace(r.Text)
	if text != "" {
		b.WriteString(text)
	} else if thought != "" {
		b.WriteString("*(Ответ модели сформирован в блоке рассуждений выше)*")
	}

	if r.Truncated {
		b.WriteString("\n\n> ⚠️ **Предупреждение:** Ответ модели был прерван до полного завершения генерации.")
		if r.Warning != "" {
			b.WriteString(fmt.Sprintf(" (%s)", r.Warning))
		}
	}

	return b.String()
}

// ParseStream reads a StreamGenerate response body and extracts text, thinking trace, and IDs.
func ParseStream(reader io.Reader) (*ParsedResponse, error) {
	resp := &ParsedResponse{}

	bufReader := bufio.NewReader(reader)

	// Discard leading magic prefix )]}' and any whitespace/newlines
	for {
		b, err := bufReader.ReadByte()
		if err != nil {
			break
		}
		if b == '[' || b == '{' || (b >= '0' && b <= '9') {
			_ = bufReader.UnreadByte()
			break
		}
	}

	tc := newThoughtCollector()
	ta := &textAccumulator{}
	var googleAPIError error
	var decodeErr error
	var sawFinalChunk bool

	dec := json.NewDecoder(bufReader)
	for {
		var token any
		err := dec.Decode(&token)
		if err == io.EOF {
			break
		}
		if err != nil {
			decodeErr = err
			break
		}

		switch v := token.(type) {
		case float64:
			// chunk length or numeric meta, ignore
		case map[string]any:
			if errVal, exists := v["error"]; exists {
				if errMap, ok := errVal.(map[string]any); ok {
					code := errMap["code"]
					msg := errMap["message"]
					status := errMap["status"]
					googleAPIError = fmt.Errorf("google api error %v [%v]: %v", code, status, msg)
				}
			}
		case []any:
			if rawBytes, err := json.Marshal(v); err == nil {
				resp.RawChunks = append(resp.RawChunks, string(rawBytes))
			}
			processEnvelopeToken(v, resp, tc, ta, &googleAPIError, &sawFinalChunk)
		}
	}

	// Final extraction: check if text has embedded :::thought ... ::: blocks or <thought>...</thought> tags
	fullText := ta.String()
	hadUnclosedThought := false

	if matches := reThoughtBlock.FindAllStringSubmatch(fullText, -1); len(matches) > 0 {
		for _, m := range matches {
			if len(m) > 1 {
				t := strings.TrimSpace(m[1])
				if t != "" {
					tc.Add(t)
				}
			}
		}
		fullText = reThoughtBlock.ReplaceAllString(fullText, "")
	}

	if matches := reThoughtTag.FindAllStringSubmatch(fullText, -1); len(matches) > 0 {
		for _, m := range matches {
			if len(m) > 1 {
				t := strings.TrimSpace(m[1])
				if t != "" {
					tc.Add(t)
				}
			}
		}
		fullText = reThoughtTag.ReplaceAllString(fullText, "")
	}

	// Handle unclosed :::thought block if present
	if strings.Contains(fullText, ":::thought") && !reThoughtBlock.MatchString(fullText) {
		hadUnclosedThought = true
		idx := strings.Index(fullText, ":::thought")
		thoughtPart := strings.TrimSpace(strings.TrimPrefix(fullText[idx:], ":::thought"))
		if thoughtPart != "" {
			tc.Add(thoughtPart)
		}
		fullText = strings.TrimSpace(fullText[:idx])
	}

	// Handle unclosed <thought> tag if present
	if strings.Contains(fullText, "<thought>") && !reThoughtTag.MatchString(fullText) {
		hadUnclosedThought = true
		idx := strings.Index(fullText, "<thought>")
		thoughtPart := strings.TrimSpace(strings.TrimPrefix(fullText[idx:], "<thought>"))
		if thoughtPart != "" {
			tc.Add(thoughtPart)
		}
		fullText = strings.TrimSpace(fullText[:idx])
	}

	resp.Thought = tc.String()
	resp.Text = strings.TrimSpace(fullText)

	// Stream integrity validation
	if decodeErr != nil {
		resp.Truncated = true
		resp.Warning = fmt.Sprintf("stream interrupted prematurely: %v", decodeErr)
		log.Printf("[WARN] Gemini StreamGenerate interrupted prematurely: %v", decodeErr)
	} else if hadUnclosedThought {
		resp.Truncated = true
		resp.Warning = "stream cut off while model was generating reasoning trace"
		log.Printf("[WARN] Gemini StreamGenerate cut off inside thinking block")
	} else if !sawFinalChunk && (resp.ResponseID == "" || resp.ChoiceID == "") && (resp.Text != "" || resp.Thought != "") {
		resp.Truncated = true
		resp.Warning = "stream ended before final response envelope was received"
		log.Printf("[WARN] Gemini StreamGenerate ended before final response envelope")
	}

	if resp.Text == "" && resp.Thought == "" {
		if googleAPIError != nil {
			return nil, googleAPIError
		}
		if decodeErr != nil {
			return nil, fmt.Errorf("%w: %v", ErrEmptyParsedResponse, decodeErr)
		}
		return nil, ErrEmptyParsedResponse
	}

	return resp, nil
}

func processEnvelopeToken(envelope []any, resp *ParsedResponse, tc *thoughtCollector, ta *textAccumulator, apiErr *error, sawFinal *bool) {
	for _, itemAny := range envelope {
		item, ok := itemAny.([]any)
		if !ok || len(item) < 3 {
			continue
		}

		rpcName, _ := item[0].(string)
		if rpcName != "wrb.fr" {
			if rpcName == "er" {
				if innerStr, ok := item[2].(string); ok && innerStr != "" {
					var errMap map[string]any
					if err := json.Unmarshal([]byte(innerStr), &errMap); err == nil {
						if errVal, exists := errMap["error"]; exists {
							if em, ok := errVal.(map[string]any); ok {
								if apiErr != nil && *apiErr == nil {
									*apiErr = fmt.Errorf("google api error %v [%v]: %v", em["code"], em["status"], em["message"])
								}
							}
						}
					}
				}
			}
			continue
		}

		innerStr, ok := item[2].(string)
		if !ok || innerStr == "" {
			continue
		}

		var innerData []any
		if err := json.Unmarshal([]byte(innerStr), &innerData); err != nil {
			// Check if innerStr is an error map
			var errMap map[string]any
			if err2 := json.Unmarshal([]byte(innerStr), &errMap); err2 == nil {
				if errVal, exists := errMap["error"]; exists {
					if em, ok := errVal.(map[string]any); ok {
						if apiErr != nil && *apiErr == nil {
							*apiErr = fmt.Errorf("google api error %v [%v]: %v", em["code"], em["status"], em["message"])
						}
					}
				}
			}
			continue
		}

		extractFromInnerData(innerData, resp, tc, ta, sawFinal)
	}
}

func extractFromInnerData(inner []any, resp *ParsedResponse, tc *thoughtCollector, ta *textAccumulator, sawFinal *bool) {
	// 1. Conversation ID & Response ID from inner[1]
	if len(inner) > 1 {
		if convInfo, ok := inner[1].([]any); ok {
			if len(convInfo) > 0 {
				if cid, ok := convInfo[0].(string); ok && cid != "" {
					resp.ConversationID = cid
				}
			}
			if len(convInfo) > 1 {
				if rid, ok := convInfo[1].(string); ok && rid != "" {
					resp.ResponseID = rid
				}
			}
		}
	}

	// 2. Candidates from inner[4]: process primary candidate (candidates[0])
	if len(inner) > 4 {
		if candidates, ok := inner[4].([]any); ok && len(candidates) > 0 {
			// Process only candidate 0 for ChoiceID and Text
			if cand, ok := candidates[0].([]any); ok && len(cand) > 0 {
				if choiceID, ok := cand[0].(string); ok && choiceID != "" {
					resp.ChoiceID = choiceID
				}

				if len(cand) > 1 {
					if parts, ok := cand[1].([]any); ok {
						var candText strings.Builder
						for _, pAny := range parts {
							if pStr, ok := pAny.(string); ok && pStr != "" {
								candText.WriteString(pStr)
							} else if pArr, ok := pAny.([]any); ok {
								for _, sub := range pArr {
									if subStr, ok := sub.(string); ok && subStr != "" {
										candText.WriteString(subStr)
									}
								}
							}
						}
						if candText.Len() > 0 {
							ta.Append(candText.String())
						}
					}
				}

				for i := 2; i < len(cand); i++ {
					inspectThinkingTrace(cand[i], tc)
				}

				// If ChoiceID and ResponseID are present, mark finalizing chunk as seen
				if resp.ChoiceID != "" && resp.ResponseID != "" && sawFinal != nil {
					*sawFinal = true
				}
			}

			// Also inspect thinking traces in other candidates if present
			for cIdx := 1; cIdx < len(candidates); cIdx++ {
				if cand, ok := candidates[cIdx].([]any); ok {
					for i := 2; i < len(cand); i++ {
						inspectThinkingTrace(cand[i], tc)
					}
				}
			}
		}
	}

	// Check any additional root slots for thought traces or extra metadata
	if len(inner) > 5 {
		for i := 5; i < len(inner); i++ {
			inspectThinkingTrace(inner[i], tc)
		}
	}
}

func inspectThinkingTrace(val any, tc *thoughtCollector) {
	if val == nil {
		return
	}

	switch v := val.(type) {
	case string:
		// If string contains thinking indicator
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "Thinking Process:") || strings.Contains(v, "Thinking Trace") {
			tc.Add(v)
		} else if strings.HasPrefix(trimmed, ":::thought") && strings.HasSuffix(trimmed, ":::") {
			content := strings.TrimPrefix(trimmed, ":::thought")
			content = strings.TrimSuffix(content, ":::")
			tc.Add(content)
		} else if strings.HasPrefix(trimmed, "<thought>") && strings.HasSuffix(trimmed, "</thought>") {
			content := strings.TrimPrefix(trimmed, "<thought>")
			content = strings.TrimSuffix(content, "</thought>")
			tc.Add(content)
		}
	case []any:
		for _, item := range v {
			inspectThinkingTrace(item, tc)
		}
	case map[string]any:
		found := false
		for _, key := range []string{"thought", "thinking", "reasoning", "thought_trace"} {
			if thoughtVal, exists := v[key]; exists {
				if tStr, ok := thoughtVal.(string); ok && tStr != "" {
					tc.Add(tStr)
					found = true
					break
				} else {
					inspectThinkingTrace(thoughtVal, tc)
					found = true
				}
			}
		}
		if !found {
			for _, val := range v {
				if _, isMap := val.(map[string]any); isMap {
					inspectThinkingTrace(val, tc)
				} else if _, isArr := val.([]any); isArr {
					inspectThinkingTrace(val, tc)
				}
			}
		}
	}
}

func extractGeneric(items []any, resp *ParsedResponse, tc *thoughtCollector, textB *strings.Builder) {
	for _, item := range items {
		if arr, ok := item.([]any); ok {
			extractGeneric(arr, resp, tc, textB)
		} else if str, ok := item.(string); ok {
			if strings.HasPrefix(str, "c_") && resp.ConversationID == "" {
				resp.ConversationID = str
			} else if strings.HasPrefix(str, "r_") && resp.ResponseID == "" {
				resp.ResponseID = str
			} else if strings.HasPrefix(str, "rc_") && resp.ChoiceID == "" {
				resp.ChoiceID = str
			}
		}
	}
}
