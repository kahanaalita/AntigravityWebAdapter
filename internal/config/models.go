package config

import (
	"fmt"
	"strings"
	"time"
)

// ModelType identifies the AI model family.
type ModelType string

const (
	ModelHydrogen4 ModelType = "hydrogen-4"
	ModelArgon4    ModelType = "argon-4"
)

// EffortLevel specifies the reasoning effort.
type EffortLevel string

const (
	EffortLow    EffortLevel = "low"
	EffortMiddle EffortLevel = "middle"
	EffortMedium EffortLevel = "medium"
	EffortHigh   EffortLevel = "high"
)

// EffortTier represents the internal numeric ID for Google Web RPC.
type EffortTier int

const (
	TierLow    EffortTier = 1
	TierMiddle EffortTier = 2
	TierHigh   EffortTier = 3
)

// ModelConfig holds settings for a specific model invocation.
type ModelConfig struct {
	Model       ModelType
	Effort      EffortLevel
	EffortTier  EffortTier
	Timeout     time.Duration
	ModelHexID  string
}

// CanonicalEffort converts aliases into normalized string representation.
func CanonicalEffort(model ModelType, effort string) (EffortLevel, EffortTier, time.Duration, error) {
	clean := strings.ToLower(strings.TrimSpace(effort))
	if clean == "" {
		if model == ModelHydrogen4 {
			clean = "middle"
		} else {
			clean = "medium"
		}
	}

	switch clean {
	case "low", "1":
		return EffortLow, TierLow, 120 * time.Second, nil
	case "middle", "medium", "med", "mid", "2":
		// Hydrogen uses "middle", Argon uses "medium", but cross-aliasing is supported
		if model == ModelHydrogen4 {
			return EffortMiddle, TierMiddle, 300 * time.Second, nil
		}
		return EffortMedium, TierMiddle, 300 * time.Second, nil
	case "high", "max", "3":
		return EffortHigh, TierHigh, 600 * time.Second, nil
	default:
		return "", 0, 0, fmt.Errorf("unknown effort level: %q (supported: low, middle, medium, high)", effort)
	}
}

// ParseModel resolves model aliases into standard ModelType.
func ParseModel(input string) (ModelType, error) {
	clean := strings.ToLower(strings.TrimSpace(input))
	switch clean {
	case "hydrogen", "hydrogen-4", "hydrogen4", "h4", "h-4":
		return ModelHydrogen4, nil
	case "argon", "argon-4", "argon4", "a4", "a-4":
		return ModelArgon4, nil
	default:
		return "", fmt.Errorf("unknown model: %q (supported: hydrogen-4, argon-4)", input)
	}
}

// ResolveModelConfig builds a full configuration for a prompt request.
func ResolveModelConfig(rawModel, rawEffort string, customHexIDs map[ModelType]string) (*ModelConfig, error) {
	model, err := ParseModel(rawModel)
	if err != nil {
		return nil, err
	}

	effort, tier, timeout, err := CanonicalEffort(model, rawEffort)
	if err != nil {
		return nil, err
	}

	hexID := ""
	if customHexIDs != nil {
		hexID = customHexIDs[model]
	}
	if hexID == "" {
		if model == ModelHydrogen4 {
			hexID = "hydrogen-4"
		} else {
			hexID = "argon-4"
		}
	}

	return &ModelConfig{
		Model:      model,
		Effort:     effort,
		EffortTier: tier,
		Timeout:    timeout,
		ModelHexID: hexID,
	}, nil
}
