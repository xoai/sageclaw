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

func TestRouter_HasTier_ComboOnly(t *testing.T) {
	router := NewEmptyRouter()
	router.RegisterProvider("gemini", &stubProvider{name: "gemini"})
	router.SetCombo("fast", Combo{
		Name:     "fast",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "gemini", ModelID: "gemini-2.5-flash"}},
	})

	if !router.HasTier(TierFast) {
		t.Fatal("expected HasTier(fast) = true with combo")
	}
	if router.HasTier(TierStrong) {
		t.Fatal("expected HasTier(strong) = false with no combo or route")
	}
}

func TestRouter_HasRoutes_ComboOnly(t *testing.T) {
	router := NewEmptyRouter()
	if router.HasRoutes() {
		t.Fatal("expected HasRoutes() = false for empty router")
	}

	router.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})
	router.SetCombo("strong", Combo{
		Name:     "strong",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "anthropic", ModelID: "claude-sonnet-4"}},
	})

	if !router.HasRoutes() {
		t.Fatal("expected HasRoutes() = true with combo loaded")
	}
}

func TestRouter_Resolve_ComboOnly(t *testing.T) {
	router := NewEmptyRouter()
	router.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})
	router.RegisterProvider("gemini", &stubProvider{name: "gemini"})
	router.SetCombo("strong", Combo{
		Name:     "strong",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "anthropic", ModelID: "claude-sonnet-4"}},
	})
	router.SetCombo("fast", Combo{
		Name:     "fast",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "gemini", ModelID: "gemini-2.5-flash"}},
	})

	// Resolve strong via combo.
	p, model := router.Resolve(TierStrong)
	if p == nil {
		t.Fatal("expected non-nil provider for strong")
	}
	if p.Name() != "anthropic" || model != "claude-sonnet-4" {
		t.Fatalf("expected anthropic/claude-sonnet-4, got %s/%s", p.Name(), model)
	}

	// Resolve fast via combo.
	p, model = router.Resolve(TierFast)
	if p == nil {
		t.Fatal("expected non-nil provider for fast")
	}
	if p.Name() != "gemini" || model != "gemini-2.5-flash" {
		t.Fatalf("expected gemini/gemini-2.5-flash, got %s/%s", p.Name(), model)
	}
}

func TestRouter_Resolve_FallbackToCombo(t *testing.T) {
	router := NewEmptyRouter()
	router.RegisterProvider("anthropic", &stubProvider{name: "anthropic"})
	router.SetCombo("strong", Combo{
		Name:     "strong",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "anthropic", ModelID: "claude-sonnet-4"}},
	})

	// Resolve fast — no fast combo or route, should fall back to strong combo.
	p, model := router.Resolve(TierFast)
	if p == nil {
		t.Fatal("expected fallback to strong combo, got nil")
	}
	if p.Name() != "anthropic" || model != "claude-sonnet-4" {
		t.Fatalf("expected anthropic/claude-sonnet-4, got %s/%s", p.Name(), model)
	}
}

func TestRouter_Resolve_NoCombosNoRoutes(t *testing.T) {
	router := NewEmptyRouter()

	p, model := router.Resolve(TierStrong)
	if p != nil {
		t.Fatalf("expected nil provider, got %s/%s", p.Name(), model)
	}
	if model != "" {
		t.Fatalf("expected empty model, got %s", model)
	}
}

func TestRouter_ChatWithFallback_NilPrimary(t *testing.T) {
	router := NewEmptyRouter()
	// Has a combo so HasRoutes() returns true, but for a different tier.
	router.RegisterProvider("gemini", &stubProvider{name: "gemini"})
	router.SetCombo("fast", Combo{
		Name:     "fast",
		Strategy: "priority",
		Models:   []ComboModel{{Provider: "gemini", ModelID: "gemini-2.5-flash"}},
	})

	req := &canonical.Request{Model: "test"}
	// Resolve TierLocal — no combo or route for local, fallback strong also empty.
	// But "fast" combo exists so HasRoutes() is true. Resolve returns nil.
	_, err := router.ChatWithFallback(context.Background(), TierLocal, req)
	if err == nil {
		t.Fatal("expected error for nil primary provider")
	}
}
