package mcp

import (
	"strings"

	"agy-gemini-web-mcp/internal/config"
)

// SlashCommandResult represents parsed components from a slash invocation.
type SlashCommandResult struct {
	Model      config.ModelType
	Effort     config.EffortLevel
	Tier       config.EffortTier
	Prompt     string
	ImagePaths []string
}

// ParseSlashCommand parses user prompt text which might start with /hydrogen or /argon and effort level.
func ParseSlashCommand(input string, fallbackModel config.ModelType, fallbackEffort string) (*SlashCommandResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return &SlashCommandResult{
			Model:  fallbackModel,
			Prompt: "",
		}, nil
	}

	model := fallbackModel
	rawEffort := fallbackEffort

	// Check if starts with /hydrogen or /argon
	tokens := strings.Fields(trimmed)
	idx := 0

	if len(tokens) > 0 {
		first := strings.ToLower(tokens[0])
		if strings.HasPrefix(first, "/") {
			cmd := strings.TrimPrefix(first, "/")
			if m, err := config.ParseModel(cmd); err == nil {
				model = m
				idx++
			}
		} else if m, err := config.ParseModel(first); err == nil {
			model = m
			idx++
		}
	}

	// Check if next token is effort level: low, middle, medium, high, etc.
	if idx < len(tokens) {
		candidate := strings.ToLower(tokens[idx])
		switch candidate {
		case "low", "middle", "medium", "high", "med", "mid", "max", "1", "2", "3":
			rawEffort = candidate
			idx++
		}
	}

	// Remaining tokens are the prompt
	prompt := strings.Join(tokens[idx:], " ")

	effortLevel, tier, _, err := config.CanonicalEffort(model, rawEffort)
	if err != nil {
		return nil, err
	}

	return &SlashCommandResult{
		Model:  model,
		Effort: effortLevel,
		Tier:   tier,
		Prompt: prompt,
	}, nil
}
