package provider

import (
	"testing"
)

func TestFindModelPricing_FromCache(t *testing.T) {
	pc := NewPricingCache()
	pc.mu.Lock()
	pc.models["claude-sonnet-4-20250514"] = &ModelPricing{
		InputCost: 3.5, OutputCost: 16.0, CacheCost: 0.35,
	}
	pc.mu.Unlock()

	p := pc.FindModelPricing("claude-sonnet-4-20250514")
	if p == nil {
		t.Fatal("expected pricing from cache")
	}
	if p.InputCost != 3.5 {
		t.Errorf("expected InputCost=3.5, got %v", p.InputCost)
	}
}

func TestFindModelPricing_FallbackToKnownModels(t *testing.T) {
	pc := NewPricingCache() // Empty cache.

	// Should fall back to KnownModels.
	p := pc.FindModelPricing("claude-sonnet-4-20250514")
	if p == nil {
		t.Fatal("expected pricing from KnownModels fallback")
	}
	if p.InputCost != 3.0 {
		t.Errorf("expected InputCost=3.0 from KnownModels, got %v", p.InputCost)
	}
}

func TestFindModelPricing_UnknownModel(t *testing.T) {
	pc := NewPricingCache()
	p := pc.FindModelPricing("unknown-model-xyz")
	if p != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestFindModelPricing_NilCache(t *testing.T) {
	var pc *PricingCache
	// Should fall back to KnownModels even with nil cache.
	p := pc.FindModelPricing("gpt-4o")
	if p == nil {
		t.Fatal("expected pricing from KnownModels with nil cache")
	}
}

func TestParseOpenRouterResponse(t *testing.T) {
	pc := NewPricingCache()

	// Simulated OpenRouter response.
	body := []byte(`{
		"data": [
			{
				"id": "anthropic/claude-sonnet-4-20250514",
				"pricing": {"prompt": "0.000003", "completion": "0.000015"}
			},
			{
				"id": "google/gemini-2.5-pro-preview",
				"pricing": {"prompt": "0.00000125", "completion": "0.00001"}
			},
			{
				"id": "openai/gpt-4o",
				"pricing": {"prompt": "0.0000025", "completion": "0.00001"}
			},
			{
				"id": "free/model",
				"pricing": {"prompt": "0", "completion": "0"}
			}
		]
	}`)

	if err := pc.parseOpenRouterResponse(body); err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Check direct model ID lookup.
	p := pc.FindModelPricing("claude-sonnet-4-20250514")
	if p == nil {
		t.Fatal("expected pricing for claude-sonnet-4-20250514")
	}
	if p.InputCost != 3.0 {
		t.Errorf("expected InputCost=3.0, got %v", p.InputCost)
	}
	if p.OutputCost != 15.0 {
		t.Errorf("expected OutputCost=15.0, got %v", p.OutputCost)
	}

	// Check GPT-4o.
	p = pc.FindModelPricing("gpt-4o")
	if p == nil {
		t.Fatal("expected pricing for gpt-4o")
	}
	if p.InputCost != 2.5 {
		t.Errorf("expected InputCost=2.5, got %v", p.InputCost)
	}

	// Free model should be skipped.
	if pc.models["model"] != nil {
		t.Error("expected free models to be skipped")
	}
}

func TestMatchToKnownModel_ExactMatch(t *testing.T) {
	result := matchToKnownModel("anthropic/claude-sonnet-4-20250514")
	if result != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude-sonnet-4-20250514, got %q", result)
	}
}

func TestMatchToKnownModel_FuzzyGeminiPreview(t *testing.T) {
	// OpenRouter uses "google/gemini-2.5-pro-preview", SageClaw uses "gemini-2.5-pro".
	result := matchToKnownModel("google/gemini-2.5-pro-preview")
	if result != "gemini-2.5-pro" {
		t.Errorf("expected gemini-2.5-pro, got %q", result)
	}
}

func TestMatchToKnownModel_DirectModelID(t *testing.T) {
	result := matchToKnownModel("openai/gpt-4o")
	if result != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", result)
	}
}

func TestMatchToKnownModel_Unknown(t *testing.T) {
	result := matchToKnownModel("unknown/random-model")
	if result != "" {
		t.Errorf("expected empty for unknown model, got %q", result)
	}
}

func TestStripModelSuffix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"gemini-2.5-pro-preview", "gemini-2.5-pro"},
		{"claude-3.5-sonnet-latest", "claude-3.5-sonnet"},
		{"gpt-4o", "gpt-4o"},
		{"model-20250514", "model"}, // Date suffix stripped.
		{"claude-sonnet-4-20250514", "claude-sonnet-4"}, // Date suffix stripped.
	}

	for _, tt := range tests {
		got := stripModelSuffix(tt.input)
		if got != tt.want {
			t.Errorf("stripModelSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEffectiveThinkingCost(t *testing.T) {
	p := &ModelPricing{OutputCost: 15.0, ThinkingCost: 0}
	if p.EffectiveThinkingCost() != 15.0 {
		t.Errorf("expected fallback to OutputCost, got %v", p.EffectiveThinkingCost())
	}

	p.ThinkingCost = 20.0
	if p.EffectiveThinkingCost() != 20.0 {
		t.Errorf("expected explicit ThinkingCost, got %v", p.EffectiveThinkingCost())
	}
}
