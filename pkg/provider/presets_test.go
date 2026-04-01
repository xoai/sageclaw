package provider

import (
	"testing"
)

func TestGeneratePresetCombos_FullSet(t *testing.T) {
	models := []DiscoveredModelInfo{
		{ModelID: "claude-opus-4-20250514", Provider: "anthropic", OutputCost: 75.0, ContextWindow: 200000},
		{ModelID: "claude-sonnet-4-20250514", Provider: "anthropic", OutputCost: 15.0, ContextWindow: 200000},
		{ModelID: "claude-haiku-4-5-20251001", Provider: "anthropic", OutputCost: 4.0, ContextWindow: 200000},
		{ModelID: "gpt-4o", Provider: "openai", OutputCost: 10.0, ContextWindow: 128000},
		{ModelID: "gpt-4o-mini", Provider: "openai", OutputCost: 0.6, ContextWindow: 128000},
		{ModelID: "o3", Provider: "openai", OutputCost: 40.0, ContextWindow: 200000},
		{ModelID: "gemini-2.5-pro", Provider: "gemini", OutputCost: 10.0, ContextWindow: 1000000},
		{ModelID: "gemini-2.5-flash", Provider: "gemini", OutputCost: 1.5, ContextWindow: 1000000},
		{ModelID: "llama3.2:3b", Provider: "ollama", OutputCost: 0, ContextWindow: 128000},
		{ModelID: "qwen2.5:7b", Provider: "ollama", OutputCost: 0, ContextWindow: 32000},
	}
	connected := []string{"anthropic", "openai", "gemini", "ollama"}

	presets := GeneratePresetCombos(models, connected)

	// strong: top by output_cost DESC, one per provider → opus(anthropic), o3(openai), gemini-pro(gemini)
	if strong, ok := presets["strong"]; !ok {
		t.Fatal("missing strong preset")
	} else {
		if len(strong.Models) != 3 {
			t.Errorf("strong: got %d models, want 3", len(strong.Models))
		}
		if strong.Models[0].ModelID != "claude-opus-4-20250514" {
			t.Errorf("strong[0]: got %s, want claude-opus-4-20250514", strong.Models[0].ModelID)
		}
		if strong.Models[1].ModelID != "o3" {
			t.Errorf("strong[1]: got %s, want o3", strong.Models[1].ModelID)
		}
		if len(strong.Models) > 2 && strong.Models[2].ModelID != "gemini-2.5-pro" {
			t.Errorf("strong[2]: got %s, want gemini-2.5-pro", strong.Models[2].ModelID)
		}
	}

	// fast: cheapest with context >= 100K, one per provider → gpt-4o-mini(openai), gemini-flash(gemini), haiku(anthropic)
	if fast, ok := presets["fast"]; !ok {
		t.Fatal("missing fast preset")
	} else {
		if len(fast.Models) != 3 {
			t.Errorf("fast: got %d models, want 3", len(fast.Models))
		}
		if fast.Models[0].ModelID != "gpt-4o-mini" {
			t.Errorf("fast[0]: got %s, want gpt-4o-mini", fast.Models[0].ModelID)
		}
		if len(fast.Models) > 1 && fast.Models[1].ModelID != "gemini-2.5-flash" {
			t.Errorf("fast[1]: got %s, want gemini-2.5-flash", fast.Models[1].ModelID)
		}
	}

	// balanced: exclude top/bottom 20%, sort by context DESC
	if balanced, ok := presets["balanced"]; !ok {
		t.Fatal("missing balanced preset")
	} else {
		if len(balanced.Models) == 0 {
			t.Error("balanced: no models")
		}
	}

	// local: ollama only, by context DESC
	if local, ok := presets["local"]; !ok {
		t.Fatal("missing local preset")
	} else {
		if len(local.Models) != 2 {
			t.Errorf("local: got %d models, want 2", len(local.Models))
		}
		if local.Models[0].ModelID != "llama3.2:3b" {
			t.Errorf("local[0]: got %s, want llama3.2:3b (128K context)", local.Models[0].ModelID)
		}
	}
}

func TestGeneratePresetCombos_UnconnectedFiltered(t *testing.T) {
	models := []DiscoveredModelInfo{
		{ModelID: "claude-sonnet-4-20250514", Provider: "anthropic", OutputCost: 15.0, ContextWindow: 200000},
		{ModelID: "gpt-4o", Provider: "openai", OutputCost: 10.0, ContextWindow: 128000},
		{ModelID: "gemini-2.5-pro", Provider: "gemini", OutputCost: 10.0, ContextWindow: 1000000},
	}
	// Only anthropic connected.
	connected := []string{"anthropic"}

	presets := GeneratePresetCombos(models, connected)

	if strong, ok := presets["strong"]; !ok {
		t.Fatal("missing strong")
	} else {
		if len(strong.Models) != 1 {
			t.Errorf("strong: got %d models, want 1 (only anthropic connected)", len(strong.Models))
		}
		if strong.Models[0].Provider != "anthropic" {
			t.Errorf("strong[0].Provider: got %s, want anthropic", strong.Models[0].Provider)
		}
	}

	if _, ok := presets["local"]; ok {
		t.Error("local preset should not exist without ollama")
	}
}

func TestGeneratePresetCombos_Empty(t *testing.T) {
	presets := GeneratePresetCombos(nil, nil)
	if len(presets) != 0 {
		t.Errorf("expected empty presets, got %d", len(presets))
	}
}

func TestGeneratePresetCombos_OllamaOnly(t *testing.T) {
	models := []DiscoveredModelInfo{
		{ModelID: "llama3.2:3b", Provider: "ollama", OutputCost: 0, ContextWindow: 128000},
		{ModelID: "qwen2.5:7b", Provider: "ollama", OutputCost: 0, ContextWindow: 32000},
	}
	connected := []string{"ollama"}

	presets := GeneratePresetCombos(models, connected)

	// No cloud models → no strong/fast/balanced.
	if _, ok := presets["strong"]; ok {
		t.Error("strong should not exist with ollama-only")
	}
	if _, ok := presets["fast"]; ok {
		t.Error("fast should not exist with ollama-only")
	}
	if _, ok := presets["balanced"]; ok {
		t.Error("balanced should not exist with ollama-only")
	}

	if local, ok := presets["local"]; !ok {
		t.Fatal("missing local preset")
	} else if len(local.Models) != 2 {
		t.Errorf("local: got %d models, want 2", len(local.Models))
	}
}

func TestGeneratePresetCombos_FastFiltersSmallContext(t *testing.T) {
	models := []DiscoveredModelInfo{
		{ModelID: "small-model", Provider: "openai", OutputCost: 0.5, ContextWindow: 8000},
		{ModelID: "big-model", Provider: "openai", OutputCost: 2.0, ContextWindow: 200000},
	}
	connected := []string{"openai"}

	presets := GeneratePresetCombos(models, connected)

	fast := presets["fast"]
	if len(fast.Models) != 1 {
		t.Errorf("fast: got %d models, want 1 (small-model filtered by context)", len(fast.Models))
	}
	if len(fast.Models) > 0 && fast.Models[0].ModelID != "big-model" {
		t.Errorf("fast[0]: got %s, want big-model", fast.Models[0].ModelID)
	}
}
