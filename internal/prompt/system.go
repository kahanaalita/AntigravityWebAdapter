package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolDefinition represents an OpenAI tool schema.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

// ChatMessage represents a message in the conversation.
type ChatMessage struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

// ExtractText extracts text representation from content (string or slice of parts).
func ExtractText(content interface{}) string {
	if content == nil {
		return ""
	}
	switch v := content.(type) {
	case string:
		return v
	case []interface{}:
		var sb strings.Builder
		for _, part := range v {
			if m, ok := part.(map[string]interface{}); ok {
				pType, _ := m["type"].(string)
				if pType == "text" {
					if txt, ok := m["text"].(string); ok {
						if sb.Len() > 0 {
							sb.WriteString("\n")
						}
						sb.WriteString(txt)
					}
				}
			}
		}
		return sb.String()
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// BuildToolsPrompt constructs system instructions instructing the model to generate ```tool_call blocks.
func BuildToolsPrompt(tools []ToolDefinition) string {
	if len(tools) == 0 {
		return ""
	}

	lines := []string{
		"Ты подключен к среде DeepSeek Harness (DSH).",
		"Ты можешь вызывать внешние инструменты для выполнения задач пользователя.",
		"",
		"КРИТИЧЕСКИ ВАЖНО (Anti-Canvas & Anti-Sandbox):",
		"- НЕ используй встроенный в веб-интерфейс Gemini интерпретатор Python.",
		"- НЕ открывай панель Canvas.",
		"- НЕ вызывай расширения Google Workspace (@Gmail, @Drive, @Docs и т.д.).",
		"- Все действия с файлами, кодом, терминалом, поиском и системой должны выполняться ИСКЛЮЧИТЕЛЬНО путем генерации текстового блока ```tool_call для внешней среды исполнения.",
		"",
		"Формат вызова инструмента:",
		"Когда тебе нужно вызвать инструмент, твой ответ должен содержать ТОЛЬКО следующий блок кода (без лишней прозы вокруг):",
		"```tool_call",
		"{",
		`  "name": "имя_инструмента",`,
		`  "arguments": {`,
		`    "параметр": "значение"`,
		"  }",
		"}",
		"```",
		"",
		"Правила:",
		"1. Вызывай за один раз один инструмент.",
		"2. Поле arguments должно быть валидным JSON-объектом.",
		"3. После получения результата выполнения инструмента продолжи решение задачи или выдай финальный ответ.",
		"",
		"Доступные инструменты:",
	}

	for _, t := range tools {
		fn := t.Function
		lines = append(lines, fmt.Sprintf("- %s: %s", fn.Name, fn.Description))
		if props, ok := fn.Parameters["properties"].(map[string]interface{}); ok {
			var paramList []string
			var reqList []string
			if reqs, ok := fn.Parameters["required"].([]interface{}); ok {
				for _, r := range reqs {
					if rs, ok := r.(string); ok {
						reqList = append(reqList, rs)
					}
				}
			}
			for k := range props {
				isRequired := false
				for _, r := range reqList {
					if r == k {
						isRequired = true
						break
					}
				}
				item := k
				if isRequired {
					item += " (обязательный)"
				}
				paramList = append(paramList, item)
			}
			if len(paramList) > 0 {
				lines = append(lines, fmt.Sprintf("  Параметры: %s", strings.Join(paramList, ", ")))
			}
		}
	}

	return strings.Join(lines, "\n")
}

// BuildIncrementalContext formats only the newest message for continuation.
func BuildIncrementalContext(messages []ChatMessage) string {
	if len(messages) == 0 {
		return ""
	}
	last := messages[len(messages)-1]
	text := ExtractText(last.Content)

	if last.Role == "tool" {
		return fmt.Sprintf("[Результат выполнения инструмента %s]:\n%s\n\nПродолжай выполнение задачи. Если нужны еще инструменты — используй формат ```tool_call.", last.ToolCallID, text)
	}

	return text
}

// BuildFullContext formats the entire conversation history.
func BuildFullContext(messages []ChatMessage, tools []ToolDefinition) string {
	var parts []string

	toolsPrompt := BuildToolsPrompt(tools)
	if toolsPrompt != "" {
		parts = append(parts, toolsPrompt)
	}

	for _, m := range messages {
		text := ExtractText(m.Content)
		if text == "" && m.Role != "tool" {
			continue
		}

		switch m.Role {
		case "user":
			parts = append(parts, fmt.Sprintf("[Пользователь]\n%s", text))
		case "assistant":
			parts = append(parts, fmt.Sprintf("[Ассистент]\n%s", text))
		case "tool":
			parts = append(parts, fmt.Sprintf("[Результат инструмента %s]:\n%s", m.ToolCallID, text))
		case "system":
			parts = append(parts, fmt.Sprintf("[Инструкция]\n%s", text))
		default:
			parts = append(parts, fmt.Sprintf("[%s]\n%s", m.Role, text))
		}
	}

	return strings.Join(parts, "\n\n")
}
