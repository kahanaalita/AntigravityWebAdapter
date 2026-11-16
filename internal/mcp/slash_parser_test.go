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

	// Case 3: Non-slash prompt starting with "low" - MUST NOT TRUNCATE "low"
	res, err = ParseSlashCommand("low latency cache design", config.ModelArgon4, "medium")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 || res.Effort != config.EffortMedium {
		t.Errorf("Expected Argon4 Medium (fallback), got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "low latency cache design" {
		t.Errorf("Prompt was truncated! Expected 'low latency cache design', got %q", res.Prompt)
	}

	// Case 4: Non-slash prompt starting with "argon" - MUST NOT TRUNCATE "argon" or switch model
	res, err = ParseSlashCommand("argon is an inert gas with atomic number 18", config.ModelHydrogen4, "middle")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelHydrogen4 {
		t.Errorf("Expected fallback Hydrogen4, got %v", res.Model)
	}
	if res.Prompt != "argon is an inert gas with atomic number 18" {
		t.Errorf("Prompt was truncated! Expected 'argon is an inert gas with atomic number 18', got %q", res.Prompt)
	}

	// Case 5: Non-slash prompt starting with "hydrogen" - MUST NOT TRUNCATE "hydrogen"
	res, err = ParseSlashCommand("hydrogen fuel cells are clean and efficient", config.ModelArgon4, "medium")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 {
		t.Errorf("Expected fallback Argon4, got %v", res.Model)
	}
	if res.Prompt != "hydrogen fuel cells are clean and efficient" {
		t.Errorf("Prompt was truncated! Expected 'hydrogen fuel cells are clean and efficient', got %q", res.Prompt)
	}

	// Case 6: Non-slash prompt starting with "high" - MUST NOT TRUNCATE "high"
	res, err = ParseSlashCommand("high availability distributed consensus", config.ModelHydrogen4, "middle")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Effort != config.EffortMiddle {
		t.Errorf("Expected fallback effort Middle, got %v", res.Effort)
	}
	if res.Prompt != "high availability distributed consensus" {
		t.Errorf("Prompt was truncated! Expected 'high availability distributed consensus', got %q", res.Prompt)
	}

	// Case 7: Pure prompt without effort level
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

	// Case 8: Compound slash command /a-argon-high
	res, err = ParseSlashCommand("/a-argon-high Спроектируй Event Sourcing систему", "", "")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 || res.Effort != config.EffortHigh {
		t.Errorf("Expected Argon4 High, got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "Спроектируй Event Sourcing систему" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 9: Compound slash command /hydrogen-high
	res, err = ParseSlashCommand("/hydrogen-high Реши математическую задачу", "", "")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelHydrogen4 || res.Effort != config.EffortHigh {
		t.Errorf("Expected Hydrogen4 High, got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "Реши математическую задачу" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 10: Compound slash command /argon_low
	res, err = ParseSlashCommand("/argon_low Краткий ответ", "", "")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 || res.Effort != config.EffortLow {
		t.Errorf("Expected Argon4 Low, got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "Краткий ответ" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 11: Slash command without effort (defaults)
	res, err = ParseSlashCommand("/hydrogen Объясни алгоритм Paxos", "", "")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelHydrogen4 || res.Effort != config.EffortMiddle {
		t.Errorf("Expected Hydrogen4 Middle default, got %v, %v", res.Model, res.Effort)
	}
	if res.Prompt != "Объясни алгоритм Paxos" {
		t.Errorf("Prompt mismatch: %q", res.Prompt)
	}

	// Case 12: Preserving multi-line prompt formatting and code indents
	multiLine := "/argon high\nfunc Calculate() int {\n\treturn 42\n}\nЧто делает этот код?"
	res, err = ParseSlashCommand(multiLine, "", "")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Model != config.ModelArgon4 || res.Effort != config.EffortHigh {
		t.Errorf("Expected Argon4 High, got %v, %v", res.Model, res.Effort)
	}
	expectedCode := "func Calculate() int {\n\treturn 42\n}\nЧто делает этот код?"
	if res.Prompt != expectedCode {
		t.Errorf("Prompt multi-line corrupted.\nExpected:\n%s\nGot:\n%s", expectedCode, res.Prompt)
	}

	// Case 13: Non-model slash command (e.g. /commit, /status) - do not strip
	res, err = ParseSlashCommand("/deploy cluster to us-east-1", config.ModelHydrogen4, "middle")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Prompt != "/deploy cluster to us-east-1" {
		t.Errorf("Expected prompt intact, got %q", res.Prompt)
	}

	// Case 14: Empty string
	res, err = ParseSlashCommand("   ", config.ModelArgon4, "medium")
	if err != nil {
		t.Fatalf("ParseSlashCommand failed: %v", err)
	}
	if res.Prompt != "" {
		t.Errorf("Expected empty prompt, got %q", res.Prompt)
	}
}
