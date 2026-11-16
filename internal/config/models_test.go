package config

import (
	"os"
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
	if err != nil || eff != EffortMiddle || tier != TierMiddle || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort(Hydrogen, middle) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	eff, tier, timeout, err = CanonicalEffort(ModelHydrogen4, "medium")
	if err != nil || eff != EffortMiddle || tier != TierMiddle || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort cross-alias to Hydrogen failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test Argon with medium and middle
	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "medium")
	if err != nil || eff != EffortMedium || tier != TierMiddle || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort(Argon, medium) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "middle")
	if err != nil || eff != EffortMedium || tier != TierMiddle || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort cross-alias to Argon failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test High
	eff, tier, timeout, err = CanonicalEffort(ModelHydrogen4, "high")
	if err != nil || eff != EffortHigh || tier != TierHigh || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort(Hydrogen, high) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test Low
	eff, tier, timeout, err = CanonicalEffort(ModelArgon4, "low")
	if err != nil || eff != EffortLow || tier != TierLow || timeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort(Argon, low) failed: %v, %v, %v, %v", eff, tier, timeout, err)
	}

	// Test Defaults
	eff, tier, defTimeout, err := CanonicalEffort(ModelHydrogen4, "")
	if err != nil || eff != EffortMiddle || defTimeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort default Hydrogen should be middle with default timeout: %v, %v", eff, defTimeout)
	}

	eff, tier, defTimeout, err = CanonicalEffort(ModelArgon4, "")
	if err != nil || eff != EffortMedium || defTimeout != DefaultStreamingTimeout {
		t.Errorf("CanonicalEffort default Argon should be medium with default timeout: %v, %v", eff, defTimeout)
	}

	// Test Invalid
	_, _, _, err = CanonicalEffort(ModelHydrogen4, "ultra")
	if err == nil {
		t.Errorf("Expected error for invalid effort level")
	}
}

func TestCanonicalEffort_UnifiedTimeout(t *testing.T) {
	levels := []string{"low", "middle", "medium", "high", "max"}
	for _, lvl := range levels {
		_, _, timeout, err := CanonicalEffort(ModelArgon4, lvl)
		if err != nil {
			t.Fatalf("CanonicalEffort failed for %s: %v", lvl, err)
		}
		if timeout != DefaultStreamingTimeout {
			t.Errorf("Level %s: expected timeout %v, got %v", lvl, DefaultStreamingTimeout, timeout)
		}
	}
}

func TestCanonicalEffort_EnvOverride(t *testing.T) {
	os.Setenv("DEFAULT_STREAMING_TIMEOUT_SECONDS", "900")
	defer os.Unsetenv("DEFAULT_STREAMING_TIMEOUT_SECONDS")

	_, _, timeout, err := CanonicalEffort(ModelHydrogen4, "high")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if timeout != 900*time.Second {
		t.Errorf("expected overridden timeout 900s, got %v", timeout)
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
	if cfg.Timeout != DefaultStreamingTimeout {
		t.Errorf("Expected %v, got %v", DefaultStreamingTimeout, cfg.Timeout)
	}
}
