package mcp

import (
	"testing"

	"agy-gemini-web-mcp/internal/config"
)

func TestParseSlashCommand(t *testing.T) {
	// Case 1: Full /hydrogen middle ...
	res, err := ParseSlashCommand("/hydrogen middle Проанализируй архитектуру @diagram.png", config.ModelHydrogen4, "middle")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelHydrogen4 {
		t.Errorf("Expected Hydrogen4, got %v", res.Model)
	}
	if res.Effort != config.EffortMiddle || res.Tier != config.TierMiddle {
		t.Errorf("Expected Middle tier, got %v, %v", res.Effort, res.Tier)
	}
	if res.Prompt != "Проанализируй архитектуру @diagram.png" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 2: /argon high ...
	res, err = ParseSlashCommand("/argon high @screens/bug.png Что не так?", config.ModelArgon4, "medium")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 {
		t.Errorf("Expected Argon4, got %v", res.Model)
	}
	if res.Effort != config.EffortHigh || res.Tier != config.TierHigh {
		t.Errorf("Expected High tier, got %v, %v", res.Effort, res.Tier)
	}
	if res.Prompt != "@screens/bug.png Что не так?" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 3: No slash command, but effort level specified
	res, err = ParseSlashCommand("low Быстрый ответ", config.ModelArgon4, "medium")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 || res.Effort != config.EffortLow {
		t.Errorf("Expected Argon4 Low, got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "Быстрый ответ" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 4: Pure prompt without effort level
	res, err = ParseSlashCommand("Просто обычный вопрос", config.ModelHydrogen4, "middle")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Effort != config.EffortMiddle {
		t.Errorf("Expected default Middle, got %v", res.Effort)
	}
	if res.Prompt != "Просто обычный вопрос" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}
}
