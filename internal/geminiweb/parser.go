package geminiweb

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
)

var (
	ErrEmptyParsedResponse = errors.New("empty response or failed to parse Gemini stream")
	magicPrefix            = []byte(")]}'\n")
	reThoughtTag           = regexp.MustCompile(`(?is)<thought(?:\s+[^>]*)?>(.*?)</thought>`)
	reThoughtBlock         = regexp.MustCompile(`(?is):::thought\s*(.*?)(?:\n\s*:::|\s+:::)(?:[\r\n\s]+|$)`)
)

// ParsedResponse holds the consolidated output from StreamGenerate.
type ParsedResponse struct {
	ConversationID string
	ResponseID     string
	ChoiceID       string
	Thought        string
	Text           string
	RawChunks      []string
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

	var thoughtBuilder strings.Builder
	var textBuilder strings.Builder

	dec := json.NewDecoder(bufReader)
	for {
		var token any
		err := dec.Decode(&token)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		switch v := token.(type) {
		case float64:
			// chunk length or numeric meta, ignore
		case []any:
			if rawBytes, err := json.Marshal(v); err == nil {
				resp.RawChunks = append(resp.RawChunks, string(rawBytes))
			}
			processEnvelopeToken(v, resp, &thoughtBuilder, &textBuilder)
		}
	}

	// Final extraction: check if text has embedded :::thought ... ::: blocks or <thought>...</thought> tags
	fullText := textBuilder.String()

	if matches := reThoughtBlock.FindAllStringSubmatch(fullText, -1); len(matches) > 0 {
		for _, m := range matches {
			if len(m) > 1 {
				t := strings.TrimSpace(m[1])
				if t != "" {
					if thoughtBuilder.Len() > 0 {
						thoughtBuilder.WriteString("\n\n")
					}
					thoughtBuilder.WriteString(t)
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
					if thoughtBuilder.Len() > 0 {
						thoughtBuilder.WriteString("\n\n")
					}
					thoughtBuilder.WriteString(t)
				}
			}
		}
		fullText = reThoughtTag.ReplaceAllString(fullText, "")
	}

	// Handle unclosed :::thought block if present
	if strings.Contains(fullText, ":::thought") && !reThoughtBlock.MatchString(fullText) {
		idx := strings.Index(fullText, ":::thought")
		thoughtPart := strings.TrimSpace(strings.TrimPrefix(fullText[idx:], ":::thought"))
		if thoughtPart != "" {
			if thoughtBuilder.Len() > 0 {
				thoughtBuilder.WriteString("\n\n")
			}
			thoughtBuilder.WriteString(thoughtPart)
		}
		fullText = strings.TrimSpace(fullText[:idx])
	}

	// Handle unclosed <thought> tag if present
	if strings.Contains(fullText, "<thought>") && !reThoughtTag.MatchString(fullText) {
		idx := strings.Index(fullText, "<thought>")
		thoughtPart := strings.TrimSpace(strings.TrimPrefix(fullText[idx:], "<thought>"))
		if thoughtPart != "" {
			if thoughtBuilder.Len() > 0 {
				thoughtBuilder.WriteString("\n\n")
			}
			thoughtBuilder.WriteString(thoughtPart)
		}
		fullText = strings.TrimSpace(fullText[:idx])
	}

	resp.Thought = strings.TrimSpace(thoughtBuilder.String())
	resp.Text = strings.TrimSpace(fullText)

	if resp.Text == "" && resp.Thought == "" {
		return nil, ErrEmptyParsedResponse
	}

	return resp, nil
}

func processEnvelopeToken(envelope []any, resp *ParsedResponse, thoughtB *strings.Builder, textB *strings.Builder) {
	for _, itemAny := range envelope {
		item, ok := itemAny.([]any)
		if !ok || len(item) < 3 {
			continue
		}

		rpcName, _ := item[0].(string)
		if rpcName != "wrb.fr" {
			continue
		}

		innerStr, ok := item[2].(string)
		if !ok || innerStr == "" {
			continue
		}

		var innerData []any
		if err := json.Unmarshal([]byte(innerStr), &innerData); err != nil {
			continue
		}

		extractFromInnerData(innerData, resp, thoughtB, textB)
	}
}

func extractFromInnerData(inner []any, resp *ParsedResponse, thoughtB *strings.Builder, textB *strings.Builder) {
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
						// In StreamGenerate, each chunk contains the full accumulated text of the candidate
						if candText.Len() > 0 {
							textB.Reset()
							textB.WriteString(candText.String())
						}
					}
				}

				for i := 2; i < len(cand); i++ {
					inspectThinkingTrace(cand[i], thoughtB)
				}
			}

			// Also inspect thinking traces in other candidates if present
			for cIdx := 1; cIdx < len(candidates); cIdx++ {
				if cand, ok := candidates[cIdx].([]any); ok {
					for i := 2; i < len(cand); i++ {
						inspectThinkingTrace(cand[i], thoughtB)
					}
				}
			}
		}
	}

	// Check any additional root slots for thought traces or extra metadata
	if len(inner) > 5 {
		for i := 5; i < len(inner); i++ {
			inspectThinkingTrace(inner[i], thoughtB)
		}
	}
}

func inspectThinkingTrace(val any, thoughtB *strings.Builder) {
	if val == nil {
		return
	}

	switch v := val.(type) {
	case string:
		// If string contains thinking indicator
		trimmed := strings.TrimSpace(v)
		if strings.HasPrefix(trimmed, "Thinking Process:") || strings.Contains(v, "Thinking Trace") {
			if thoughtB.Len() > 0 {
				thoughtB.WriteString("\n\n")
			}
			thoughtB.WriteString(v)
		} else if strings.HasPrefix(trimmed, ":::thought") && strings.HasSuffix(trimmed, ":::") {
			content := strings.TrimPrefix(trimmed, ":::thought")
			content = strings.TrimSuffix(content, ":::")
			content = strings.TrimSpace(content)
			if content != "" {
				if thoughtB.Len() > 0 {
					thoughtB.WriteString("\n\n")
				}
				thoughtB.WriteString(content)
			}
		} else if strings.HasPrefix(trimmed, "<thought>") && strings.HasSuffix(trimmed, "</thought>") {
			content := strings.TrimPrefix(trimmed, "<thought>")
			content = strings.TrimSuffix(content, "</thought>")
			content = strings.TrimSpace(content)
			if content != "" {
				if thoughtB.Len() > 0 {
					thoughtB.WriteString("\n\n")
				}
				thoughtB.WriteString(content)
			}
		}
	case []any:
		for _, item := range v {
			inspectThinkingTrace(item, thoughtB)
		}
	case map[string]any:
		found := false
		for _, key := range []string{"thought", "thinking", "reasoning", "thought_trace"} {
			if thoughtVal, exists := v[key]; exists {
				if tStr, ok := thoughtVal.(string); ok && tStr != "" {
					if thoughtB.Len() > 0 {
						thoughtB.WriteString("\n\n")
					}
					thoughtB.WriteString(tStr)
					found = true
					break
				} else {
					inspectThinkingTrace(thoughtVal, thoughtB)
					found = true
				}
			}
		}
		if !found {
			for _, val := range v {
				if _, isMap := val.(map[string]any); isMap {
					inspectThinkingTrace(val, thoughtB)
				} else if _, isArr := val.([]any); isArr {
					inspectThinkingTrace(val, thoughtB)
				}
			}
		}
	}
}

func extractGeneric(items []any, resp *ParsedResponse, thoughtB *strings.Builder, textB *strings.Builder) {
	for _, item := range items {
		if arr, ok := item.([]any); ok {
			extractGeneric(arr, resp, thoughtB, textB)
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
