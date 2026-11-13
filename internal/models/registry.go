package models

import "strings"

// ModelSpec defines metadata and DOM selector attributes for a Gemini Web model.
type ModelSpec struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	DOMModel        string   `json:"model"`
	DefaultEffort   string   `json:"default_effort,omitempty"`
	Effort          string   `json:"effort,omitempty"`
	InputModalities []string `json:"input_modalities"`
}

var Registry = map[string]ModelSpec{
	// Gemini 4 Hydrogen (Thinking / Pro)
	"gemini-4-hydrogen-low": {
		ID:              "gemini-4-hydrogen-low",
		Name:            "Gemini 4 Hydrogen (Низкий)",
		DOMModel:        "4 Hydrogen",
		Effort:          "low",
		InputModalities: []string{"text", "image"},
	},
	"gemini-4-hydrogen-medium": {
		ID:              "gemini-4-hydrogen-medium",
		Name:            "Gemini 4 Hydrogen (Средний)",
		DOMModel:        "4 Hydrogen",
		Effort:          "medium",
		InputModalities: []string{"text", "image"},
	},
	"gemini-4-hydrogen-high": {
		ID:              "gemini-4-hydrogen-high",
		Name:            "Gemini 4 Hydrogen (Высокий)",
		DOMModel:        "4 Hydrogen",
		Effort:          "high",
		InputModalities: []string{"text", "image"},
	},

	// Gemini 3.5 Flash-Lite (Быстрые ответы)
	"gemini-3.5-flash-lite": {
		ID:              "gemini-3.5-flash-lite",
		Name:            "Gemini 3.5 Flash-Lite",
		DOMModel:        "3.5 Flash-Lite",
		InputModalities: []string{"text", "image"},
	},

	// Gemini 3.1 Pro (Исследования и глубокий анализ)
	"gemini-3.1-pro-low": {
		ID:              "gemini-3.1-pro-low",
		Name:            "Gemini 3.1 Pro (Низкий)",
		DOMModel:        "3.1 Pro",
		Effort:          "low",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.1-pro-medium": {
		ID:              "gemini-3.1-pro-medium",
		Name:            "Gemini 3.1 Pro (Средний)",
		DOMModel:        "3.1 Pro",
		Effort:          "medium",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.1-pro-high": {
		ID:              "gemini-3.1-pro-high",
		Name:            "Gemini 3.1 Pro (Высокий)",
		DOMModel:        "3.1 Pro",
		Effort:          "high",
		InputModalities: []string{"text", "image"},
	},

	// Gemini 4 Argon (Код и анализ)
	"gemini-4-argon-low": {
		ID:              "gemini-4-argon-low",
		Name:            "Gemini 4 Argon (Низкий)",
		DOMModel:        "4 Argon",
		Effort:          "low",
		InputModalities: []string{"text", "image"},
	},
	"gemini-4-argon-medium": {
		ID:              "gemini-4-argon-medium",
		Name:            "Gemini 4 Argon (Средний)",
		DOMModel:        "4 Argon",
		Effort:          "medium",
		InputModalities: []string{"text", "image"},
	},
	"gemini-4-argon-high": {
		ID:              "gemini-4-argon-high",
		Name:            "Gemini 4 Argon (Высокий)",
		DOMModel:        "4 Argon",
		Effort:          "high",
		InputModalities: []string{"text", "image"},
	},

	// Base Aliases
	"gemini-4-hydrogen": {
		ID:              "gemini-4-hydrogen",
		Name:            "Gemini 4 Hydrogen",
		DOMModel:        "4 Hydrogen",
		DefaultEffort:   "medium",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.1-pro": {
		ID:              "gemini-3.1-pro",
		Name:            "Gemini 3.1 Pro",
		DOMModel:        "3.1 Pro",
		DefaultEffort:   "high",
		InputModalities: []string{"text", "image"},
	},
	"gemini-4-argon": {
		ID:              "gemini-4-argon",
		Name:            "Gemini 4 Argon",
		DOMModel:        "4 Argon",
		DefaultEffort:   "high",
		InputModalities: []string{"text", "image"},
	},

	// Backward compatibility
	"gemini-3.8-flash-low": {
		ID:              "gemini-3.8-flash-low",
		Name:            "Gemini 4 Hydrogen (Низкий)",
		DOMModel:        "4 Hydrogen",
		Effort:          "low",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.8-flash-medium": {
		ID:              "gemini-3.8-flash-medium",
		Name:            "Gemini 4 Hydrogen (Средний)",
		DOMModel:        "4 Hydrogen",
		Effort:          "medium",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.8-flash-high": {
		ID:              "gemini-3.8-flash-high",
		Name:            "Gemini 4 Hydrogen (Высокий)",
		DOMModel:        "4 Hydrogen",
		Effort:          "high",
		InputModalities: []string{"text", "image"},
	},
	"gemini-3.8-flash": {
		ID:              "gemini-3.8-flash",
		Name:            "Gemini 4 Hydrogen",
		DOMModel:        "4 Hydrogen",
		DefaultEffort:   "medium",
		InputModalities: []string{"text", "image"},
	},
}

// ResolveModel maps incoming model string and reasoning_effort to DOM model name and effort level.
func ResolveModel(requestedModel, reasoningEffort string) (domModel string, effort string) {
	req := strings.TrimSpace(requestedModel)
	if req == "" {
		req = "gemini-4-hydrogen-high"
	}

	spec, ok := Registry[req]
	if !ok {
		// Default fallback
		return "4 Hydrogen", "high"
	}

	if spec.Effort != "" {
		return spec.DOMModel, spec.Effort
	}

	if reasoningEffort != "" {
		return spec.DOMModel, reasoningEffort
	}

	if spec.DefaultEffort != "" {
		return spec.DOMModel, spec.DefaultEffort
	}

	return spec.DOMModel, "medium"
}
