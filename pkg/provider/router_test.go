package provider

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

type stubProvider struct {
	name string
}

func (p *stubProvider) Name() string { return p.name }
func (p *stubProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return nil, nil
}
func (p *stubProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan StreamEvent, error) {
	return nil, nil
}

func TestRouter_Resolve(t *testing.T) {
	anthropic := &stubProvider{name: "anthropic"}
	ollama := &stubProvider{name: "ollama"}

	router, err := NewRouter(map[Tier]Route{
		TierLocal:  {Provider: ollama, Model: "llama3.2:3b"},
		TierFast:   {Provider: anthropic, Model: "claude-haiku-4-5-20251001"},
		TierStrong: {Provider: anthropic, Model: "claude-sonnet-4-20250514"},
	}, TierStrong)
	if err != nil {
		t.Fatalf("creating router: %v", err)
	}

	// Resolve local tier.
	p, model := router.Resolve(TierLocal)
	if p.Name() != "ollama" {
		t.Fatalf("expected ollama, got %s", p.Name())
	}
	if model != "llama3.2:3b" {
		t.Fatalf("expected llama3.2:3b, got %s", model)
	}

	// Resolve strong tier.
	p, model = router.Resolve(TierStrong)
	if p.Name() != "anthropic" {
		t.Fatalf("expected anthropic, got %s", p.Name())
	}
	if model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected claude-sonnet-4-20250514, got %s", model)
	}
}

func TestRouter_Fallback(t *testing.T) {
	anthropic := &stubProvider{name: "anthropic"}

	router, _ := NewRouter(map[Tier]Route{
		TierStrong: {Provider: anthropic, Model: "claude-sonnet-4-20250514"},
	}, TierStrong)

	// Unknown tier should fall back to strong.
	p, model := router.Resolve(TierLocal)
	if p.Name() != "anthropic" {
		t.Fatalf("expected fallback to anthropic, got %s", p.Name())
	}
	if model != "claude-sonnet-4-20250514" {
		t.Fatalf("expected fallback model, got %s", model)
	}
}

func TestRouter_InvalidFallback(t *testing.T) {
	_, err := NewRouter(map[Tier]Route{
		TierStrong: {Provider: &stubProvider{}, Model: "test"},
	}, TierLocal) // local doesn't exist
	if err == nil {
		t.Fatal("expected error for invalid fallback")
	}
}

func TestRouter_HasTier(t *testing.T) {
	router, _ := NewRouter(map[Tier]Route{
		TierStrong: {Provider: &stubProvider{}, Model: "test"},
		TierFast:   {Provider: &stubProvider{}, Model: "test"},
	}, TierStrong)

	if !router.HasTier(TierStrong) {
		t.Fatal("expected HasTier(strong) = true")
	}
	if router.HasTier(TierLocal) {
		t.Fatal("expected HasTier(local) = false")
	}
}
