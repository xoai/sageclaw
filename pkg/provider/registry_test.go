package provider

import (
	"context"
	"testing"
)

// mockPricingStore implements PricingStore for testing.
type mockPricingStore struct {
	models      map[string]*PricingStoreResult
	bulkUpdates []PricingUpdate // captured from BulkUpdatePricing calls
}

func (m *mockPricingStore) GetModelPricing(_ context.Context, modelID string) (*PricingStoreResult, error) {
	r, ok := m.models[modelID]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func (m *mockPricingStore) BulkUpdatePricing(_ context.Context, updates []PricingUpdate) error {
	m.bulkUpdates = append(m.bulkUpdates, updates...)
	// Also populate models map so ResolvePricing finds them.
	for _, u := range updates {
		m.models[u.ModelID] = &PricingStoreResult{
			InputCost:     u.InputCost,
			OutputCost:    u.OutputCost,
			CacheCost:     u.CacheCost,
			PricingSource: u.Source,
		}
	}
	return nil
}

func TestModelRegistry_ResolvePricing(t *testing.T) {
	store := &mockPricingStore{
		models: map[string]*PricingStoreResult{
			"claude-sonnet-4": {
				InputCost:         3.0,
				OutputCost:        15.0,
				CacheCost:         0.3,
				ThinkingCost:      0,
				CacheCreationCost: 3.75,
				PricingSource:     "known",
			},
		},
	}
	reg := NewModelRegistry(store)

	// Known model.
	p := reg.ResolvePricing("claude-sonnet-4")
	if p == nil {
		t.Fatal("expected pricing for claude-sonnet-4")
	}
	if p.InputCost != 3.0 || p.OutputCost != 15.0 {
		t.Errorf("unexpected pricing: input=%f output=%f", p.InputCost, p.OutputCost)
	}
	if p.CacheCreationCost != 3.75 {
		t.Errorf("expected cache_creation_cost=3.75, got %f", p.CacheCreationCost)
	}

	// Unknown model → nil.
	p = reg.ResolvePricing("nonexistent")
	if p != nil {
		t.Errorf("expected nil for unknown model, got %+v", p)
	}

	// Cache hit: second call should return same result without store call.
	p = reg.ResolvePricing("claude-sonnet-4")
	if p == nil || p.InputCost != 3.0 {
		t.Error("cache miss on second call")
	}

	// After invalidation, should re-query store.
	reg.InvalidateCache()
	p = reg.ResolvePricing("claude-sonnet-4")
	if p == nil || p.InputCost != 3.0 {
		t.Error("failed after cache invalidation")
	}
}

func TestModelRegistry_NoCachingForMisses(t *testing.T) {
	callCount := 0
	store := &countingPricingStore{count: &callCount}
	reg := NewModelRegistry(store)

	// First call: miss → store query.
	p := reg.ResolvePricing("unknown")
	if p != nil {
		t.Error("expected nil")
	}
	if callCount != 1 {
		t.Errorf("expected 1 store call, got %d", callCount)
	}

	// Second call: misses are NOT cached → another store query.
	// This ensures models that appear after discovery are found on next call.
	reg.ResolvePricing("unknown")
	if callCount != 2 {
		t.Errorf("expected 2 store calls (no nil caching), got %d", callCount)
	}
}

func TestModelRegistry_ModelAppearsAfterDiscovery(t *testing.T) {
	// Simulates: model unknown at first, then discovery adds it.
	dynamic := &dynamicPricingStore{models: make(map[string]*PricingStoreResult)}
	reg := NewModelRegistry(dynamic)

	// Initially unknown.
	p := reg.ResolvePricing("new-model")
	if p != nil {
		t.Error("expected nil before discovery")
	}

	// Discovery adds the model.
	dynamic.models["new-model"] = &PricingStoreResult{
		InputCost:  1.0,
		OutputCost: 5.0,
	}

	// Should find it now (no nil caching blocking).
	p = reg.ResolvePricing("new-model")
	if p == nil {
		t.Fatal("expected pricing after discovery")
	}
	if p.InputCost != 1.0 {
		t.Errorf("expected 1.0, got %f", p.InputCost)
	}
}

type dynamicPricingStore struct {
	models map[string]*PricingStoreResult
}

func (s *dynamicPricingStore) GetModelPricing(_ context.Context, modelID string) (*PricingStoreResult, error) {
	r := s.models[modelID]
	return r, nil
}

func (s *dynamicPricingStore) BulkUpdatePricing(_ context.Context, _ []PricingUpdate) error {
	return nil
}

type countingPricingStore struct {
	count *int
}

func (s *countingPricingStore) GetModelPricing(_ context.Context, _ string) (*PricingStoreResult, error) {
	*s.count++
	return nil, nil
}

func (s *countingPricingStore) BulkUpdatePricing(_ context.Context, _ []PricingUpdate) error {
	return nil
}

func TestCalculateCost_BasicFormula(t *testing.T) {
	pricing := &ModelPricing{
		InputCost:  3.0,  // $3/1M input
		OutputCost: 15.0, // $15/1M output
		CacheCost:  0.3,  // $0.3/1M cached
	}

	// 1000 input, 500 output, no cache, no thinking.
	cost, saved := CalculateCost(pricing, 1000, 500, 0, 0, 0)
	expectedCost := 1000.0/1_000_000*3.0 + 500.0/1_000_000*15.0
	if abs(cost-expectedCost) > 0.000001 {
		t.Errorf("basic cost: expected %f, got %f", expectedCost, cost)
	}
	if saved != 0 {
		t.Errorf("expected 0 saved with no cache, got %f", saved)
	}
}

func TestCalculateCost_WithCaching(t *testing.T) {
	pricing := &ModelPricing{
		InputCost:  3.0,
		OutputCost: 15.0,
		CacheCost:  0.3,
	}

	// 10000 total input, 8000 from cache, 2000 regular.
	cost, saved := CalculateCost(pricing, 10000, 500, 0, 8000, 0)
	regularInput := 2000.0 // 10000 - 8000
	expectedCost := regularInput/1_000_000*3.0 + 500.0/1_000_000*15.0 + 8000.0/1_000_000*0.3
	if abs(cost-expectedCost) > 0.000001 {
		t.Errorf("cache cost: expected %f, got %f", expectedCost, cost)
	}
	if saved <= 0 {
		t.Error("expected positive savings with cache read")
	}
}

func TestCalculateCost_WithThinking(t *testing.T) {
	pricing := &ModelPricing{
		InputCost:    3.0,
		OutputCost:   15.0,
		CacheCost:    0.3,
		ThinkingCost: 15.0, // Same as output for this model.
	}

	// 1000 input, 800 output total (300 thinking + 500 regular).
	cost, _ := CalculateCost(pricing, 1000, 800, 0, 0, 300)
	expectedCost := 1000.0/1_000_000*3.0 + 500.0/1_000_000*15.0 + 300.0/1_000_000*15.0
	if abs(cost-expectedCost) > 0.000001 {
		t.Errorf("thinking cost: expected %f, got %f", expectedCost, cost)
	}
}

func TestCalculateCost_ThinkingFallbackToOutput(t *testing.T) {
	pricing := &ModelPricing{
		InputCost:    3.0,
		OutputCost:   15.0,
		ThinkingCost: 0, // Should fall back to OutputCost.
	}

	cost, _ := CalculateCost(pricing, 0, 500, 0, 0, 200)
	expectedCost := 300.0/1_000_000*15.0 + 200.0/1_000_000*15.0 // regular=300, thinking=200
	if abs(cost-expectedCost) > 0.000001 {
		t.Errorf("thinking fallback: expected %f, got %f", expectedCost, cost)
	}
}

func TestCalculateCost_CacheCreationCost(t *testing.T) {
	// Explicit CacheCreationCost.
	pricing := &ModelPricing{
		InputCost:         3.0,
		OutputCost:        15.0,
		CacheCreationCost: 4.0, // Explicit, not 3.0 * 1.25 = 3.75.
	}
	cost, _ := CalculateCost(pricing, 0, 0, 1000, 0, 0)
	expectedCost := 1000.0 / 1_000_000 * 4.0
	if abs(cost-expectedCost) > 0.000001 {
		t.Errorf("explicit cache creation: expected %f, got %f", expectedCost, cost)
	}

	// Fallback CacheCreationCost (0 → InputCost * 1.25).
	pricing2 := &ModelPricing{
		InputCost:  3.0,
		OutputCost: 15.0,
	}
	cost2, _ := CalculateCost(pricing2, 0, 0, 1000, 0, 0)
	expectedCost2 := 1000.0 / 1_000_000 * 3.75
	if abs(cost2-expectedCost2) > 0.000001 {
		t.Errorf("fallback cache creation: expected %f, got %f", expectedCost2, cost2)
	}
}

func TestCalculateCost_NilPricing(t *testing.T) {
	cost, saved := CalculateCost(nil, 10000, 5000, 1000, 3000, 500)
	if cost != 0 || saved != 0 {
		t.Errorf("nil pricing should return (0, 0), got (%f, %f)", cost, saved)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
