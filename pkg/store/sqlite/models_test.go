package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func TestUpsertAndListDiscoveredModels(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	models := []store.DiscoveredModel{
		{ID: "anthropic/claude-sonnet-4", Provider: "anthropic", ModelID: "claude-sonnet-4", DisplayName: "Claude Sonnet 4", ContextWindow: 200000, MaxOutputTokens: 8192, Capabilities: map[string]bool{"vision": true, "thinking": true}},
		{ID: "anthropic/claude-haiku-4", Provider: "anthropic", ModelID: "claude-haiku-4", DisplayName: "Claude Haiku 4", ContextWindow: 200000, Capabilities: map[string]bool{"vision": true}},
		{ID: "openai/gpt-4o", Provider: "openai", ModelID: "gpt-4o", DisplayName: "GPT-4o", ContextWindow: 128000, Capabilities: map[string]bool{}},
	}

	if err := s.UpsertDiscoveredModels(ctx, models); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// List all.
	all, err := s.ListAllDiscoveredModels(ctx)
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3 models, got %d", len(all))
	}

	// List by provider.
	anthropic, err := s.ListDiscoveredModels(ctx, "anthropic")
	if err != nil {
		t.Fatalf("list anthropic: %v", err)
	}
	if len(anthropic) != 2 {
		t.Fatalf("expected 2 anthropic models, got %d", len(anthropic))
	}
	// Ordered by display_name: Claude Haiku before Claude Sonnet.
	if anthropic[0].DisplayName != "Claude Haiku 4" {
		t.Fatalf("expected Claude Haiku 4 first, got %s", anthropic[0].DisplayName)
	}

	// Check capabilities round-trip.
	if !anthropic[1].Capabilities["vision"] {
		t.Fatal("expected vision capability on Sonnet")
	}
	if !anthropic[1].Capabilities["thinking"] {
		t.Fatal("expected thinking capability on Sonnet")
	}
}

func TestRefreshDiscoveredModels_Transactional(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert initial models.
	initial := []store.DiscoveredModel{
		{ID: "openai/gpt-4o", Provider: "openai", ModelID: "gpt-4o", DisplayName: "GPT-4o", Capabilities: map[string]bool{}},
		{ID: "openai/gpt-4o-mini", Provider: "openai", ModelID: "gpt-4o-mini", DisplayName: "GPT-4o Mini", Capabilities: map[string]bool{}},
	}
	if err := s.UpsertDiscoveredModels(ctx, initial); err != nil {
		t.Fatalf("initial upsert: %v", err)
	}

	// Refresh with different models (simulating model list changed).
	refreshed := []store.DiscoveredModel{
		{ID: "openai/gpt-4.1", Provider: "openai", ModelID: "gpt-4.1", DisplayName: "GPT-4.1", ContextWindow: 1000000, Capabilities: map[string]bool{}},
	}
	if err := s.RefreshDiscoveredModels(ctx, "openai", refreshed); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Old models should be gone, only new one remains.
	models, err := s.ListDiscoveredModels(ctx, "openai")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model after refresh, got %d", len(models))
	}
	if models[0].ModelID != "gpt-4.1" {
		t.Fatalf("expected gpt-4.1, got %s", models[0].ModelID)
	}
}

func TestRefreshDoesNotAffectOtherProviders(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Insert models for two providers.
	s.UpsertDiscoveredModels(ctx, []store.DiscoveredModel{
		{ID: "anthropic/sonnet", Provider: "anthropic", ModelID: "sonnet", DisplayName: "Sonnet", Capabilities: map[string]bool{}},
		{ID: "openai/gpt", Provider: "openai", ModelID: "gpt", DisplayName: "GPT", Capabilities: map[string]bool{}},
	})

	// Refresh only openai.
	s.RefreshDiscoveredModels(ctx, "openai", []store.DiscoveredModel{
		{ID: "openai/gpt-new", Provider: "openai", ModelID: "gpt-new", DisplayName: "GPT New", Capabilities: map[string]bool{}},
	})

	// Anthropic should be untouched.
	anthropic, _ := s.ListDiscoveredModels(ctx, "anthropic")
	if len(anthropic) != 1 {
		t.Fatalf("expected 1 anthropic model, got %d", len(anthropic))
	}
}

func TestGetDiscoveredModelAge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No models — should return large duration.
	age, err := s.GetDiscoveredModelAge(ctx, "anthropic")
	if err != nil {
		t.Fatalf("age: %v", err)
	}
	if age < 24*time.Hour {
		t.Fatalf("expected large age for empty cache, got %v", age)
	}

	// Insert a model — age should be very small.
	s.UpsertDiscoveredModels(ctx, []store.DiscoveredModel{
		{ID: "anthropic/test", Provider: "anthropic", ModelID: "test", DisplayName: "Test", Capabilities: map[string]bool{}},
	})

	age, err = s.GetDiscoveredModelAge(ctx, "anthropic")
	if err != nil {
		t.Fatalf("age after insert: %v", err)
	}
	if age > 5*time.Second {
		t.Fatalf("expected small age after insert, got %v", age)
	}
}

func TestDeleteDiscoveredModelsByProvider(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertDiscoveredModels(ctx, []store.DiscoveredModel{
		{ID: "openai/a", Provider: "openai", ModelID: "a", DisplayName: "A", Capabilities: map[string]bool{}},
		{ID: "openai/b", Provider: "openai", ModelID: "b", DisplayName: "B", Capabilities: map[string]bool{}},
	})

	if err := s.DeleteDiscoveredModelsByProvider(ctx, "openai"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	models, _ := s.ListDiscoveredModels(ctx, "openai")
	if len(models) != 0 {
		t.Fatalf("expected 0 models after delete, got %d", len(models))
	}
}
