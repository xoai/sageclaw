package sqlite

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/store"
)

func TestUpsertAndListModelPricingOverrides(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	override := store.ModelPricingOverride{
		ModelID:           "ollama/llama3.2:3b",
		Provider:          "ollama",
		InputCost:         0,
		OutputCost:        0,
		CacheCost:         0,
		ThinkingCost:      0,
		CacheCreationCost: 0,
	}
	if err := s.UpsertModelPricingOverride(ctx, override); err != nil {
		t.Fatalf("UpsertModelPricingOverride: %v", err)
	}

	overrides, err := s.ListModelPricingOverrides(ctx)
	if err != nil {
		t.Fatalf("ListModelPricingOverrides: %v", err)
	}
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override, got %d", len(overrides))
	}
	if overrides[0].ModelID != "ollama/llama3.2:3b" {
		t.Errorf("unexpected model_id: %s", overrides[0].ModelID)
	}
	if overrides[0].Provider != "ollama" {
		t.Errorf("unexpected provider: %s", overrides[0].Provider)
	}

	// Update the override.
	override.InputCost = 1.5
	override.OutputCost = 3.0
	if err := s.UpsertModelPricingOverride(ctx, override); err != nil {
		t.Fatalf("UpsertModelPricingOverride update: %v", err)
	}
	overrides, _ = s.ListModelPricingOverrides(ctx)
	if len(overrides) != 1 {
		t.Fatalf("expected 1 override after upsert, got %d", len(overrides))
	}
	if overrides[0].InputCost != 1.5 || overrides[0].OutputCost != 3.0 {
		t.Errorf("pricing not updated: input=%f output=%f", overrides[0].InputCost, overrides[0].OutputCost)
	}
}

func TestDeleteModelPricingOverride(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	override := store.ModelPricingOverride{
		ModelID:  "test-model",
		Provider: "test",
	}
	s.UpsertModelPricingOverride(ctx, override)

	if err := s.DeleteModelPricingOverride(ctx, "test-model"); err != nil {
		t.Fatalf("DeleteModelPricingOverride: %v", err)
	}
	overrides, _ := s.ListModelPricingOverrides(ctx)
	if len(overrides) != 0 {
		t.Errorf("expected 0 overrides after delete, got %d", len(overrides))
	}
}

func TestGetModelPricing_OverrideTakesPrecedence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert a discovered model with pricing.
	models := []store.DiscoveredModel{{
		ID:            "anthropic/claude-sonnet-4",
		Provider:      "anthropic",
		ModelID:       "claude-sonnet-4",
		DisplayName:   "Claude Sonnet 4",
		InputCost:     3.0,
		OutputCost:    15.0,
		PricingSource: "known",
	}}
	if err := s.UpsertDiscoveredModels(ctx, models); err != nil {
		t.Fatalf("UpsertDiscoveredModels: %v", err)
	}

	// Without override, should return discovered pricing.
	m, err := s.GetModelPricing(ctx, "anthropic/claude-sonnet-4")
	if err != nil {
		t.Fatalf("GetModelPricing: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil pricing for known model")
	}
	if m.InputCost != 3.0 || m.PricingSource != "known" {
		t.Errorf("expected known pricing, got input=%f source=%s", m.InputCost, m.PricingSource)
	}

	// Add an override.
	override := store.ModelPricingOverride{
		ModelID:    "anthropic/claude-sonnet-4",
		Provider:   "anthropic",
		InputCost:  2.0,
		OutputCost: 10.0,
	}
	s.UpsertModelPricingOverride(ctx, override)

	// Override should take precedence.
	m, err = s.GetModelPricing(ctx, "anthropic/claude-sonnet-4")
	if err != nil {
		t.Fatalf("GetModelPricing with override: %v", err)
	}
	if m.PricingSource != "user" {
		t.Errorf("expected 'user' source, got %q", m.PricingSource)
	}
	if m.InputCost != 2.0 {
		t.Errorf("expected override input cost 2.0, got %f", m.InputCost)
	}

	// Delete override — should revert to discovered.
	s.DeleteModelPricingOverride(ctx, "anthropic/claude-sonnet-4")
	m, _ = s.GetModelPricing(ctx, "anthropic/claude-sonnet-4")
	if m.PricingSource != "known" {
		t.Errorf("expected 'known' source after override delete, got %q", m.PricingSource)
	}
}

func TestGetModelPricing_FallbackToModelID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert with composite ID.
	models := []store.DiscoveredModel{{
		ID:            "gemini/gemini-3-flash",
		Provider:      "gemini",
		ModelID:       "gemini-3-flash",
		DisplayName:   "Gemini 3 Flash",
		InputCost:     0.5,
		OutputCost:    3.0,
		PricingSource: "known",
	}}
	s.UpsertDiscoveredModels(ctx, models)

	// Look up by raw model_id (not composite).
	m, _ := s.GetModelPricing(ctx, "gemini-3-flash")
	if m == nil {
		t.Fatal("expected pricing via model_id fallback")
	}
	if m.InputCost != 0.5 {
		t.Errorf("expected 0.5, got %f", m.InputCost)
	}
}

func TestGetModelPricing_UnknownModelReturnsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	m, err := s.GetModelPricing(ctx, "nonexistent-model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m != nil {
		t.Errorf("expected nil for unknown model, got %+v", m)
	}
}

func TestRefreshDiscoveredModels_PreservesPricing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert model with pricing.
	models := []store.DiscoveredModel{{
		ID:            "anthropic/claude-opus-4",
		Provider:      "anthropic",
		ModelID:       "claude-opus-4",
		DisplayName:   "Claude Opus 4",
		ContextWindow: 200000,
		InputCost:     15.0,
		OutputCost:    75.0,
		PricingSource: "openrouter",
	}}
	s.UpsertDiscoveredModels(ctx, models)

	// Refresh with same model but no pricing (discovery-only).
	refreshed := []store.DiscoveredModel{{
		ID:              "anthropic/claude-opus-4",
		Provider:        "anthropic",
		ModelID:         "claude-opus-4",
		DisplayName:     "Claude Opus 4 (updated)",
		ContextWindow:   200000,
		MaxOutputTokens: 32000,
	}}
	if err := s.RefreshDiscoveredModels(ctx, "anthropic", refreshed); err != nil {
		t.Fatalf("RefreshDiscoveredModels: %v", err)
	}

	// Pricing should be preserved, display_name updated.
	all, _ := s.ListDiscoveredModels(ctx, "anthropic")
	if len(all) != 1 {
		t.Fatalf("expected 1 model, got %d", len(all))
	}
	if all[0].DisplayName != "Claude Opus 4 (updated)" {
		t.Errorf("display_name not updated: %s", all[0].DisplayName)
	}
	if all[0].InputCost != 15.0 {
		t.Errorf("pricing lost after refresh: input_cost=%f", all[0].InputCost)
	}
	if all[0].PricingSource != "openrouter" {
		t.Errorf("pricing_source lost: %s", all[0].PricingSource)
	}
}

func TestRefreshDiscoveredModels_RemovesStaleModels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert two models.
	models := []store.DiscoveredModel{
		{ID: "anthropic/model-a", Provider: "anthropic", ModelID: "model-a", DisplayName: "A"},
		{ID: "anthropic/model-b", Provider: "anthropic", ModelID: "model-b", DisplayName: "B"},
	}
	s.UpsertDiscoveredModels(ctx, models)

	// Refresh with only model-a — model-b should be removed.
	refreshed := []store.DiscoveredModel{
		{ID: "anthropic/model-a", Provider: "anthropic", ModelID: "model-a", DisplayName: "A v2"},
	}
	s.RefreshDiscoveredModels(ctx, "anthropic", refreshed)

	all, _ := s.ListDiscoveredModels(ctx, "anthropic")
	if len(all) != 1 {
		t.Fatalf("expected 1 model after refresh, got %d", len(all))
	}
	if all[0].ID != "anthropic/model-a" {
		t.Errorf("wrong model survived: %s", all[0].ID)
	}
}

func TestGetModelPricing_NoPricingSourceReturnsNil(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert a model without pricing (discovery-only).
	models := []store.DiscoveredModel{{
		ID:          "ollama/llama3",
		Provider:    "ollama",
		ModelID:     "llama3",
		DisplayName: "Llama 3",
		// No pricing fields set, PricingSource = ""
	}}
	s.UpsertDiscoveredModels(ctx, models)

	// Should return nil — no pricing data.
	m, _ := s.GetModelPricing(ctx, "ollama/llama3")
	if m != nil {
		t.Errorf("expected nil for model without pricing, got source=%q", m.PricingSource)
	}
}

func TestBulkUpdateModelPricing_InsertsNewModels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	updates := []store.ModelPricingBulk{
		{ModelID: "gpt-4o", Provider: "openai", InputCost: 2.5, OutputCost: 10.0, CacheCost: 0.25, Source: "openrouter"},
		{ModelID: "claude-sonnet-4", Provider: "anthropic", InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3, Source: "openrouter"},
	}
	if err := s.BulkUpdateModelPricing(ctx, updates); err != nil {
		t.Fatalf("BulkUpdateModelPricing: %v", err)
	}

	// Verify inserted.
	m, err := s.GetModelPricing(ctx, "openai/gpt-4o")
	if err != nil || m == nil {
		t.Fatalf("expected pricing for openai/gpt-4o, got err=%v", err)
	}
	if m.InputCost != 2.5 || m.PricingSource != "openrouter" {
		t.Errorf("unexpected pricing: input=%v source=%q", m.InputCost, m.PricingSource)
	}
}

func TestBulkUpdateModelPricing_UpdatesExistingOpenRouter(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed with known pricing.
	s.UpsertDiscoveredModels(ctx, []store.DiscoveredModel{{
		ID: "anthropic/claude-sonnet-4", Provider: "anthropic", ModelID: "claude-sonnet-4",
		InputCost: 3.0, OutputCost: 15.0, PricingSource: "known",
	}})

	// Bulk update with new OpenRouter pricing.
	updates := []store.ModelPricingBulk{
		{ModelID: "claude-sonnet-4", Provider: "anthropic", InputCost: 3.5, OutputCost: 16.0, CacheCost: 0.35, Source: "openrouter"},
	}
	if err := s.BulkUpdateModelPricing(ctx, updates); err != nil {
		t.Fatalf("BulkUpdateModelPricing: %v", err)
	}

	m, _ := s.GetModelPricing(ctx, "anthropic/claude-sonnet-4")
	if m == nil {
		t.Fatal("expected pricing")
	}
	if m.InputCost != 3.5 || m.PricingSource != "openrouter" {
		t.Errorf("expected updated pricing: input=%v source=%q", m.InputCost, m.PricingSource)
	}
}

func TestBulkUpdateModelPricing_SkipsUserOverrides(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create a user override.
	s.UpsertModelPricingOverride(ctx, store.ModelPricingOverride{
		ModelID: "custom-model", Provider: "ollama", InputCost: 0, OutputCost: 0,
	})

	// Also create a discovered_models row with user pricing.
	s.UpsertDiscoveredModels(ctx, []store.DiscoveredModel{{
		ID: "ollama/custom-model", Provider: "ollama", ModelID: "custom-model",
		InputCost: 0, OutputCost: 0, PricingSource: "user",
	}})

	// Try to overwrite with OpenRouter pricing — should be skipped.
	updates := []store.ModelPricingBulk{
		{ModelID: "custom-model", Provider: "ollama", InputCost: 5.0, OutputCost: 20.0, Source: "openrouter"},
	}
	if err := s.BulkUpdateModelPricing(ctx, updates); err != nil {
		t.Fatalf("BulkUpdateModelPricing: %v", err)
	}

	// User override should still be returned (from overrides table).
	m, _ := s.GetModelPricing(ctx, "custom-model")
	if m == nil {
		t.Fatal("expected pricing from override")
	}
	if m.PricingSource != "user" {
		t.Errorf("expected user override preserved, got source=%q", m.PricingSource)
	}

	// The discovered_models row should also be untouched.
	var source string
	s.db.QueryRow(`SELECT pricing_source FROM discovered_models WHERE id = 'ollama/custom-model'`).Scan(&source)
	if source != "user" {
		t.Errorf("expected discovered_models pricing_source=user, got %q", source)
	}
}
