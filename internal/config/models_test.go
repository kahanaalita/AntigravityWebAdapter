package config

import (
	"testing"
	"time"
)

func TestParseModel(t *testing.T) {
	tests := []struct {
		input    string
		expected ModelType
		wantErr  bool
	}{
		{"hydrogen-4", ModelHydrogen4, false},
		{"Hydrogen", ModelHydrogen4, false},
		{"h4", ModelHydrogen4, false},
		{"argon-4", ModelArgon4, false},
		{"ARGON", ModelArgon4, false},
		{"a4", ModelArgon4, false},
		{"unknown-model", "", true},
	}

	for _, tc := range tests {
		got, err := ParseModel(tc.input)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseModel(%q) error = %v, wantErr %v", tc.input, err, tc.wantErr)
			continue
		}
		if got != tc.expected {
			t.Errorf("ParseModel(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestCanonicalEffort(t *testing.T) {
	// Test Hydrogen with middle and medium
	eff, tier, timeout, err := CanonicalEffort(ModelHydrogen4, "middle")
	if err != nil || eff != EffortMiddle || tier != TierMiddle || timeout != 300*time.Second {
		t.Errorf("CanonicalEffort(Hydrogen, middle) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	eff, tier, timeout, err = CanonicalEffort(ModelHydrogen4, "medium")
	if err != nil || eff != EffortMiddle || tier != TierMiddle {
		t.Errorf("CanonicalEffort cross-alias to Hydrogen failed: %v, %v, %v", eff, tier, err)
	}

	// Test Argon with medium and middle
	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "medium")
	if err != nil || eff != EffortMedium || tier != TierMiddle || timeout != 300*time.Second {
		t.Errorf("CanonicalEffort(Argon, medium) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "middle")
	if err != nil || eff != EffortMedium || tier != TierMiddle {
		t.Errorf("CanonicalEffort cross-alias to Argon failed: %v, %v, %v", eff, tier, err)
	}

	// Test High
	eff, tier, timeout, err = CanonicalEffort(ModelHydrogen4, "high")
	if err != nil || eff != EffortHigh || tier != TierHigh || timeout != 600*time.Second {
		t.Errorf("CanonicalEffort(Hydrogen, high) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test Low
	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "low")
	if err != nil || eff != EffortLow || tier != TierLow || timeout != 120*time.Second {
		t.Errorf("CanonicalEffort(Argon, low) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test Defaults
	eff, tier, _, err = CanonicalEffort(ModelHydrogen4, "")
	if err != nil || eff != EffortMiddle {
		t.Errorf("CanonicalEffort default Hydrogen should be middle: %v", eff)
	}

	eff, tier, _, err = CanonicalEffort(ModelArgon4, "")
	if err != nil || eff != EffortMedium {
		t.Errorf("CanonicalEffort default Argon should be medium: %v", eff)
	}

	// Test Invalid
	_, _, _, err = CanonicalEffort(ModelHydrogen4, "ultra")
	if err == nil {
		t.Errorf("Expected error for invalid effort level")
	}
}

func TestResolveModelConfig(t *testing.T) {
	cfg, err := ResolveModelConfig("argon", "high", map[ModelType]string{
		ModelArgon4: "hex_argon_pro_2026",
	})
	if err != nil {
		t.Fatalf("ResolveModelConfig failed: %v", err)
	}

	if cfg.Model != ModelArgon4 {
		t.Errorf("Expected %v, got %v", ModelArgon4, cfg.Model)
	}
	if cfg.Effort != EffortHigh || cfg.EffortTier != TierHigh {
		t.Errorf("Expected High tier, got %v, %v", cfg.Effort, cfg.EffortTier)
	}
	if cfg.ModelHexID != "hex_argon_pro_2026" {
		t.Errorf("Expected custom hex ID, got %q", cfg.ModelHexID)
	}
	if cfg.Timeout != 600*time.Second {
		t.Errorf("Expected 600s, got %v", cfg.Timeout)
	}
}
