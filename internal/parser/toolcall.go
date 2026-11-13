package parser

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ParamAlias maps commonly hallucinated or abbreviated parameter names to their standard schemas.
var ParamAlias = map[string]string{
	"path":     "file_path",
	"file":     "file_path",
	"filepath": "file_path",
	"filename": "file_path",
	"cmd":      "command",
	"code":     "command",
	"script":   "command",
	"text":     "content",
	"data":     "content",
	"body":     "content",
}

// ToolCall represents an OpenAI-compatible function tool call.
type ToolCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSchema represents a tool definition passed to the model.
type ToolSchema struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// StripJSComments removes JS-style single-line comments outside quoted strings.
func StripJSComments(s string) string {
	var inStr bool
	var quote rune
	var esc bool
	var sb strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inStr {
			sb.WriteRune(c)
			if esc {
				esc = false
			} else if c == '\\' {
				esc = true
			} else if c == quote {
				inStr = false
			}
		} else {
			if c == '"' || c == '\'' {
				inStr = true
				quote = c
				sb.WriteRune(c)
			} else if c == '/' && i+1 < len(runes) && runes[i+1] == '/' {
				for i < len(runes) && runes[i] != '\n' {
					i++
				}
				if i < len(runes) {
					sb.WriteRune(runes[i])
				}
			} else {
				sb.WriteRune(c)
			}
		}
	}
	return sb.String()
}

// FixUnescapedNewlinesInStrings fixes raw newlines inside JSON string literals.
func FixUnescapedNewlinesInStrings(s string) string {
	var inStr bool
	var esc bool
	var sb strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if inStr {
			if esc {
				esc = false
				sb.WriteRune(c)
			} else if c == '\\' {
				esc = true
				sb.WriteRune(c)
			} else if c == '"' {
				inStr = false
				sb.WriteRune(c)
			} else if c == '\n' {
				sb.WriteString("\\n")
			} else if c == '\r' {
				// skip carriage return
			} else if c == '\t' {
				sb.WriteString("\\t")
			} else {
				sb.WriteRune(c)
			}
		} else {
			if c == '"' {
				inStr = true
			}
			sb.WriteRune(c)
		}
	}
	return sb.String()
}

// FixInvalidEscapes replaces single backslashes that are not followed by valid JSON escape sequences with double backslashes.
func FixInvalidEscapes(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	runes := []rune(s)
	n := len(runes)
	inString := false

	for i := 0; i < n; i++ {
		ch := runes[i]
		if ch == '"' && (i == 0 || runes[i-1] != '\\') {
			inString = !inString
		}

		if inString && ch == '\\' {
			if i+1 >= n {
				buf.WriteString(`\\`)
				continue
			}
			next := runes[i+1]
			switch next {
			case '\\', '"', '/', 'b', 'f', 'n', 'r', 't':
				buf.WriteRune(ch)
				buf.WriteRune(next)
				i++
				continue
			case 'u':
				if i+5 < n && isHex(runes[i+2]) && isHex(runes[i+3]) && isHex(runes[i+4]) && isHex(runes[i+5]) {
					buf.WriteString(string(runes[i : i+6]))
					i += 5
					continue
				}
				buf.WriteString(`\\`)
				continue
			default:
				buf.WriteString(`\\`)
				continue
			}
		}
		buf.WriteRune(ch)
	}
	return buf.String()
}

func isHex(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

// JSONParseTolerant attempts progressive recovery to parse imperfect JSON from LLMs.
func JSONParseTolerant(s string) (interface{}, error) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return nil, nil
	}

	// Level 1: Standard JSON parse
	var res interface{}
	if err := json.Unmarshal([]byte(raw), &res); err == nil {
		return res, nil
	}

	// Level 2: Strip comments
	cleaned := StripJSComments(raw)
	if err := json.Unmarshal([]byte(cleaned), &res); err == nil {
		return res, nil
	}

	// Level 3: Fix unescaped newlines in strings
	cleaned = FixUnescapedNewlinesInStrings(cleaned)
	if err := json.Unmarshal([]byte(cleaned), &res); err == nil {
		return res, nil
	}

	// Level 4: Fix single backslashes (e.g. Windows paths C:\Users)
	fixedBackslash := FixInvalidEscapes(cleaned)
	if err := json.Unmarshal([]byte(fixedBackslash), &res); err == nil {
		return res, nil
	}

	// Level 5: Fix trailing commas and unquoted keys
	reTrailingComma := regexp.MustCompile(`,\s*([}\]])`)
	fixedStructure := reTrailingComma.ReplaceAllString(fixedBackslash, "$1")
	reUnquotedKey := regexp.MustCompile(`([{,]\s*)([a-zA-Z0-9_$]+)\s*:`)
	fixedStructure = reUnquotedKey.ReplaceAllString(fixedStructure, `$1"$2":`)
	if err := json.Unmarshal([]byte(fixedStructure), &res); err == nil {
		return res, nil
	}

	return nil, nil
}

// ExtractBalancedObjects scans text and returns all substrings that are balanced {...} JSON blocks.
func ExtractBalancedObjects(text string) []string {
	var out []string
	runes := []rune(text)
	n := len(runes)

	for i := 0; i < n; i++ {
		if runes[i] != '{' {
			continue
		}
		depth := 0
		inStr := false
		esc := false
		for j := i; j < n; j++ {
			c := runes[j]
			if inStr {
				if esc {
					esc = false
				} else if c == '\\' {
					esc = true
				} else if c == '"' {
					inStr = false
				}
			} else {
				if c == '"' {
					inStr = true
				} else if c == '{' {
					depth++
				} else if c == '}' {
					depth--
					if depth == 0 {
						out = append(out, string(runes[i:j+1]))
						break
					}
				}
			}
		}
	}
	return out
}

// MatchToolByParams scores available tools against parameter keys to deduce a missing tool name.
func MatchToolByParams(rawArgs map[string]interface{}, tools []ToolSchema) string {
	if len(rawArgs) == 0 || len(tools) == 0 {
		return ""
	}

	var best string
	var bestScore float64

	for _, t := range tools {
		if t.Name == "" {
			continue
		}
		props, _ := t.Parameters["properties"].(map[string]interface{})
		if len(props) == 0 {
			continue
		}

		score := 0.0
		hit := 0

		for k := range rawArgs {
			if k == "name" || k == "tool" || k == "arguments" || k == "args" {
				continue
			}
			if _, ok := props[k]; ok {
				score += 2.0
				hit++
			} else if alias, ok := ParamAlias[k]; ok {
				if _, ok := props[alias]; ok {
					score += 2.0
					hit++
				}
			} else {
				score -= 1.5
			}
		}

		if hit > 0 {
			score += (float64(hit) / float64(len(props))) * 2.0
			if score > bestScore {
				bestScore = score
				best = t.Name
			}
		}
	}

	if bestScore >= 2.0 {
		return best
	}
	return ""
}

// ExtractThoughts separates <model-thoughts> tags from response content.
func ExtractThoughts(rawText string) (thoughts string, content string) {
	re := regexp.MustCompile(`(?s)<model-thoughts>(.*?)</model-thoughts>`)
	matches := re.FindAllStringSubmatch(rawText, -1)
	if len(matches) > 0 {
		var tb strings.Builder
		for _, m := range matches {
			if len(m) > 1 {
				tb.WriteString(strings.TrimSpace(m[1]))
				tb.WriteString("\n")
			}
		}
		thoughts = strings.TrimSpace(tb.String())
		content = strings.TrimSpace(re.ReplaceAllString(rawText, ""))
		return thoughts, content
	}
	return "", strings.TrimSpace(rawText)
}

// ParseToolCalls extracts valid and repaired ToolCalls from text.
func ParseToolCalls(text string, tools []ToolSchema) []ToolCall {
	var calls []ToolCall
	t := strings.TrimSpace(text)
	if t == "" {
		return calls
	}

	// Remove thoughts to avoid false positives inside thoughts
	_, contentWithoutThoughts := ExtractThoughts(t)
	if contentWithoutThoughts != "" {
		t = contentWithoutThoughts
	} else if strings.Contains(text, "<model-thoughts>") {
		return calls
	}

	pushObj := func(raw interface{}) bool {
		m, ok := raw.(map[string]interface{})
		if !ok || m == nil {
			return len(calls) > 0
		}

		// Check array wrapper: {"tool_calls": [...]}
		if arr, ok := m["tool_calls"].([]interface{}); ok && len(arr) > 0 {
			any := false
			for _, item := range arr {
				if pushObjFromMap(item, tools, &calls) {
					any = true
				}
			}
			return any
		}

		// Check single wrapper: {"tool_call": {...}}
		if inner, ok := m["tool_call"].(map[string]interface{}); ok {
			m = inner
		}

		return pushObjFromMap(m, tools, &calls)
	}

	// Try extracting code blocks first
	codeBlockPatterns := []*regexp.Regexp{
		regexp.MustCompile("(?s)```(?:tool_call|json)\\s*\\n([\\s\\S]*?)```"),
		regexp.MustCompile("(?s)```\\s*\\n([\\s\\S]*?)```"),
		regexp.MustCompile(`(?s)<tool_call\b[^>]*>([\s\S]*?)(?:<\/tool_call>|$)`),
	}

	for _, re := range codeBlockPatterns {
		matches := re.FindAllStringSubmatch(t, -1)
		for i := len(matches) - 1; i >= 0; i-- {
			block := strings.TrimSpace(matches[i][1])
			if parsed, _ := JSONParseTolerant(block); parsed != nil {
				if pushObj(parsed) {
					return calls
				}
			}
			// Try balanced objects inside block
			for _, bo := range ExtractBalancedObjects(block) {
				if parsed, _ := JSONParseTolerant(bo); parsed != nil {
					if pushObj(parsed) {
						return calls
					}
				}
			}
		}
	}

	// Try scanning balanced objects in full text (from end to start)
	balanced := ExtractBalancedObjects(t)
	for i := len(balanced) - 1; i >= 0; i-- {
		bo := balanced[i]
		if parsed, _ := JSONParseTolerant(bo); parsed != nil {
			if pushObj(parsed) {
				return calls
			}
		}
	}

	return calls
}

func pushObjFromMap(m interface{}, tools []ToolSchema, calls *[]ToolCall) bool {
	obj, ok := m.(map[string]interface{})
	if !ok || obj == nil {
		return false
	}

	var name string
	if n, ok := obj["name"].(string); ok {
		name = n
	} else if n, ok := obj["tool"].(string); ok {
		name = n
	} else if fn, ok := obj["function"].(map[string]interface{}); ok {
		if n, ok := fn["name"].(string); ok {
			name = n
		}
	}

	var argsObj map[string]interface{}
	var argsStr string

	if rawArgs, ok := obj["arguments"]; ok {
		if s, ok := rawArgs.(string); ok {
			argsStr = s
			if parsed, _ := JSONParseTolerant(s); parsed != nil {
				if po, ok := parsed.(map[string]interface{}); ok {
					argsObj = po
				}
			}
		} else if po, ok := rawArgs.(map[string]interface{}); ok {
			argsObj = po
			b, _ := json.Marshal(po)
			argsStr = string(b)
		}
	} else if rawArgs, ok := obj["args"]; ok {
		if po, ok := rawArgs.(map[string]interface{}); ok {
			argsObj = po
			b, _ := json.Marshal(po)
			argsStr = string(b)
		}
	} else if rawArgs, ok := obj["parameters"]; ok {
		if po, ok := rawArgs.(map[string]interface{}); ok {
			argsObj = po
			b, _ := json.Marshal(po)
			argsStr = string(b)
		}
	} else if name == "" {
		argsObj = obj
		b, _ := json.Marshal(obj)
		argsStr = string(b)
	} else {
		argsObj = make(map[string]interface{})
		argsStr = "{}"
	}

	nameKnown := false
	if name != "" {
		for _, t := range tools {
			if t.Name == name {
				nameKnown = true
				break
			}
		}
	}

	finalName := name
	if !nameKnown {
		if matched := MatchToolByParams(argsObj, tools); matched != "" {
			finalName = matched
		}
	}

	if finalName != "" {
		// Normalize parameter aliases
		if argsObj != nil {
			modified := false
			normalized := make(map[string]interface{})
			for k, v := range argsObj {
				if alias, ok := ParamAlias[k]; ok {
					normalized[alias] = v
					modified = true
				} else {
					normalized[k] = v
				}
			}
			if modified {
				b, _ := json.Marshal(normalized)
				argsStr = string(b)
			}
		}

		if argsStr == "" {
			argsStr = "{}"
		}

		*calls = append(*calls, ToolCall{
			Name:      finalName,
			Arguments: argsStr,
		})
		return true
	}

	return false
}

// LooksLikeToolCall checks if the string has any markers of a tool call.
func LooksLikeToolCall(text string) bool {
	return strings.Contains(text, "tool_call") ||
		strings.Contains(text, "```tool_call") ||
		strings.Contains(text, "<tool_call") ||
		strings.Contains(text, `"name":`) ||
		strings.Contains(text, `"tool":`)
}
