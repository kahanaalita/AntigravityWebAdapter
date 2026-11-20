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

// isEffortToken checks if candidate is a recognized effort keyword or tier.
func isEffortToken(candidate string) bool {
	switch strings.ToLower(strings.TrimSpace(candidate)) {
	case "low", "middle", "medium", "high", "med", "mid", "max", "1", "2", "3":
		return true
	default:
		return false
	}
}

// parseCompoundCommand parses single compound tokens like "a-neon-high", "neon-high", "hydrogen_middle", etc.
func parseCompoundCommand(cmd string) (config.ModelType, string, bool) {
	clean := strings.ToLower(strings.TrimSpace(cmd))
	clean = strings.TrimPrefix(clean, "a-")

	for _, sep := range []string{"-", "_"} {
		idx := strings.LastIndex(clean, sep)
		if idx > 0 && idx < len(clean)-1 {
			modelPart := clean[:idx]
			effortPart := clean[idx+1:]
			if isEffortToken(effortPart) {
				if m, err := config.ParseModel(modelPart); err == nil {
					return m, effortPart, true
				}
			}
		}
	}
	return "", "", false
}

// ParseSlashCommand parses user prompt text which might start with /hydrogen or /neon and effort level.
// Slash commands are only routed when the first token explicitly begins with a forward slash ('/').
// If the prompt begins with a normal word (e.g. "argon is...", "hydrogen fuel...", "low latency..."),
// no tokens are trimmed and the entire prompt text is preserved without truncation.
func ParseSlashCommand(input string, fallbackModel config.ModelType, fallbackEffort string) (*SlashCommandResult, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		var effortLevel config.EffortLevel
		var tier config.EffortTier
		if fallbackModel != "" {
			var err error
			effortLevel, tier, _, err = config.CanonicalEffort(fallbackModel, fallbackEffort)
			if err != nil {
				return nil, err
			}
		}
		return &SlashCommandResult{
			Model:  fallbackModel,
			Effort: effortLevel,
			Tier:   tier,
			Prompt: "",
		}, nil
	}

	tokens := strings.Fields(trimmed)
	if len(tokens) == 0 {
		return &SlashCommandResult{
			Model:  fallbackModel,
			Prompt: "",
		}, nil
	}

	first := tokens[0]
	// Slash commands MUST strictly begin with a forward slash ('/')
	if !strings.HasPrefix(first, "/") {
		var effortLevel config.EffortLevel
		var tier config.EffortTier
		if fallbackModel != "" {
			var err error
			effortLevel, tier, _, err = config.CanonicalEffort(fallbackModel, fallbackEffort)
			if err != nil {
				return nil, err
			}
		}
		return &SlashCommandResult{
			Model:  fallbackModel,
			Effort: effortLevel,
			Tier:   tier,
			Prompt: trimmed,
		}, nil
	}

	// First token begins with '/'. Parse the command identifier.
	cmd := strings.TrimPrefix(first, "/")
	cmdLower := strings.ToLower(cmd)

	var model config.ModelType
	var rawEffort string
	consumedTokens := 0

	// 1. Check if first token is a compound command like /a-neon-high, /neon-high, /hydrogen_middle
	if m, eff, ok := parseCompoundCommand(cmdLower); ok {
		model = m
		rawEffort = eff
		consumedTokens = 1
	} else if m, err := config.ParseModel(strings.TrimPrefix(cmdLower, "a-")); err == nil {
		// 2. Direct model command like /hydrogen, /neon, /a-neon, /a-hydrogen, /h4, /n4
		model = m
		consumedTokens = 1

		// Check if next token is an explicit effort level
		if len(tokens) > 1 && isEffortToken(tokens[1]) {
			rawEffort = strings.ToLower(tokens[1])
			consumedTokens = 2
		} else {
			rawEffort = fallbackEffort
		}
	} else {
		// First token begins with '/' but is not a recognized model command (e.g. /status, /help).
		// Preserve entire input without stripping.
		var effortLevel config.EffortLevel
		var tier config.EffortTier
		if fallbackModel != "" {
			var err error
			effortLevel, tier, _, err = config.CanonicalEffort(fallbackModel, fallbackEffort)
			if err != nil {
				return nil, err
			}
		}
		return &SlashCommandResult{
			Model:  fallbackModel,
			Effort: effortLevel,
			Tier:   tier,
			Prompt: trimmed,
		}, nil
	}

	// Compute remaining prompt without collapsing newlines/spaces
	var prompt string
	if consumedTokens == 1 {
		prompt = strings.TrimSpace(trimmed[len(tokens[0]):])
	} else if consumedTokens == 2 {
		afterTok0 := trimmed[len(tokens[0]):]
		pos := strings.Index(afterTok0, tokens[1])
		if pos >= 0 {
			prompt = strings.TrimSpace(afterTok0[pos+len(tokens[1]):])
		} else {
			prompt = strings.TrimSpace(strings.Join(tokens[2:], " "))
		}
	}

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
