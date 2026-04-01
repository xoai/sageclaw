package provider

import (
	"testing"
)

func TestParseOpenRouterPricing(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"id": "anthropic/claude-sonnet-4-20250514",
				"pricing": {
					"prompt": "0.000003",
					"completion": "0.000015",
					"input_cache_read": "0.0000003",
					"input_cache_write": "0.00000375"
				}
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

	updates, err := parseOpenRouterPricing(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	byID := make(map[string]PricingUpdate)
	for _, u := range updates {
		byID[u.ModelID] = u
	}

	// Claude Sonnet 4 — should have real cache pricing from API.
	if p, ok := byID["claude-sonnet-4-20250514"]; !ok {
		t.Error("expected claude-sonnet-4-20250514 in updates")
	} else {
		if p.InputCost != 3.0 {
			t.Errorf("InputCost = %v, want 3.0", p.InputCost)
		}
		if p.OutputCost != 15.0 {
			t.Errorf("OutputCost = %v, want 15.0", p.OutputCost)
		}
		// Cache read: 0.0000003 * 1M = 0.3
		if p.CacheCost != 0.3 {
			t.Errorf("CacheCost = %v, want 0.3 (from input_cache_read)", p.CacheCost)
		}
		// Cache creation: 0.00000375 * 1M = 3.75
		if p.CacheCreationCost != 3.75 {
			t.Errorf("CacheCreationCost = %v, want 3.75 (from input_cache_write)", p.CacheCreationCost)
		}
	}

	// GPT-4o — no cache fields, should fall back to 10% estimate.
	if p, ok := byID["gpt-4o"]; !ok {
		t.Error("expected gpt-4o in updates")
	} else {
		if p.InputCost != 2.5 {
			t.Errorf("InputCost = %v, want 2.5", p.InputCost)
		}
		// Fallback: 2.5 * 0.1 = 0.25
		if p.CacheCost != 0.25 {
			t.Errorf("CacheCost = %v, want 0.25 (10%% fallback)", p.CacheCost)
		}
		// No cache write field → 0.
		if p.CacheCreationCost != 0 {
			t.Errorf("CacheCreationCost = %v, want 0 (no field)", p.CacheCreationCost)
		}
	}

	// Free model should be skipped.
	if _, ok := byID["model"]; ok {
		t.Error("expected free models to be skipped")
	}
}

func TestParseOpenRouterPricing_DeduplicatesKnownModelMatch(t *testing.T) {
	body := []byte(`{
		"data": [
			{
				"id": "google/gemini-2.5-pro-preview",
				"pricing": {"prompt": "0.00000125", "completion": "0.00001"}
			}
		]
	}`)

	updates, err := parseOpenRouterPricing(body)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}

	// Should have both the normalized ID and the KnownModels ID.
	byID := make(map[string]PricingUpdate)
	for _, u := range updates {
		byID[u.ModelID] = u
	}
	if _, ok := byID["gemini-2.5-pro-preview"]; !ok {
		t.Error("expected gemini-2.5-pro-preview (normalized)")
	}
	if _, ok := byID["gemini-2.5-pro"]; !ok {
		t.Error("expected gemini-2.5-pro (KnownModel match)")
	}
}

func TestMatchToKnownModel_ExactMatch(t *testing.T) {
	result := matchToKnownModel("anthropic/claude-sonnet-4-20250514")
	if result != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude-sonnet-4-20250514, got %q", result)
	}
}

func TestMatchToKnownModel_FuzzyGeminiPreview(t *testing.T) {
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
		{"model-20250514", "model"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
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

func TestEffectiveCacheCreationCost(t *testing.T) {
	p := &ModelPricing{InputCost: 3.0, CacheCreationCost: 0}
	if p.EffectiveCacheCreationCost() != 3.75 { // 3.0 * 1.25
		t.Errorf("expected fallback to InputCost*1.25, got %v", p.EffectiveCacheCreationCost())
	}

	p.CacheCreationCost = 4.0
	if p.EffectiveCacheCreationCost() != 4.0 {
		t.Errorf("expected explicit CacheCreationCost, got %v", p.EffectiveCacheCreationCost())
	}
}

