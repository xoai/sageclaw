package provider

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestLookupModelCapabilities_ExactMatch(t *testing.T) {
	caps, ok := LookupModelCapabilities("gpt-4o")
	if !ok {
		t.Fatal("expected gpt-4o to be found")
	}
	if caps.ContextWindow != 128000 {
		t.Errorf("expected context_window=128000, got %d", caps.ContextWindow)
	}
	if !caps.Vision {
		t.Error("expected gpt-4o to support vision")
	}
	if !caps.Tools {
		t.Error("expected gpt-4o to support tools")
	}
}

func TestLookupModelCapabilities_PrefixMatch(t *testing.T) {
	// "claude-sonnet-4-20250514" should match prefix "claude-sonnet-4".
	caps, ok := LookupModelCapabilities("claude-sonnet-4-20250514")
	if !ok {
		t.Fatal("expected claude-sonnet-4-20250514 to match prefix claude-sonnet-4")
	}
	if caps.ContextWindow != 200000 {
		t.Errorf("expected context_window=200000, got %d", caps.ContextWindow)
	}
	if !caps.Thinking {
		t.Error("expected claude-sonnet-4 to support thinking")
	}
	if !caps.Caching {
		t.Error("expected claude-sonnet-4 to support caching")
	}
}

func TestLookupModelCapabilities_Unknown(t *testing.T) {
	_, ok := LookupModelCapabilities("unknown-model-xyz")
	if ok {
		t.Error("expected unknown model to not be found")
	}
}

func TestDefaultCapabilities(t *testing.T) {
	caps := DefaultCapabilities()
	if caps.ContextWindow != 8192 {
		t.Errorf("expected default context_window=8192, got %d", caps.ContextWindow)
	}
	if !caps.Tools {
		t.Error("expected default to support tools")
	}
	if caps.Vision {
		t.Error("expected default to not support vision")
	}
	if caps.Thinking {
		t.Error("expected default to not support thinking")
	}
}

func TestGetCapabilities_WithModelCapabilities(t *testing.T) {
	// Provider that implements ModelCapabilities.
	p := &mockCapProvider{caps: Capabilities{
		ContextWindow: 999999, Thinking: true,
	}}
	caps := GetCapabilities(p, "custom-model")
	if caps.ContextWindow != 999999 {
		t.Errorf("expected 999999, got %d", caps.ContextWindow)
	}
}

func TestGetCapabilities_FallbackToRegistry(t *testing.T) {
	// Provider that does NOT implement ModelCapabilities.
	p := &mockProvider{}
	caps := GetCapabilities(p, "gemini-2.5-pro-latest")
	if caps.ContextWindow != 1000000 {
		t.Errorf("expected 1000000 from registry, got %d", caps.ContextWindow)
	}
}

func TestGetCapabilities_FallbackToDefault(t *testing.T) {
	p := &mockProvider{}
	caps := GetCapabilities(p, "totally-unknown-model")
	if caps.ContextWindow != 8192 {
		t.Errorf("expected default 8192, got %d", caps.ContextWindow)
	}
}

func TestKnownCaps_GeminiHasSearchGrounding(t *testing.T) {
	caps, ok := LookupModelCapabilities("gemini-2.5-flash")
	if !ok {
		t.Fatal("expected gemini-2.5-flash to be found")
	}
	if !caps.SearchGrounding {
		t.Error("expected gemini-2.5-flash to support search grounding")
	}
	if !caps.CodeExecution {
		t.Error("expected gemini-2.5-flash to support code execution")
	}
}

func TestKnownCaps_DeepSeekReasonerNoTools(t *testing.T) {
	caps, ok := LookupModelCapabilities("deepseek-reasoner")
	if !ok {
		t.Fatal("expected deepseek-reasoner to be found")
	}
	if !caps.Thinking {
		t.Error("expected deepseek-reasoner to support thinking")
	}
	if caps.Tools {
		t.Error("expected deepseek-reasoner to NOT support tools")
	}
}

func TestLookupModelCapabilities_LongestPrefixWins(t *testing.T) {
	// "gpt-4o-mini-2025-04-09" must match "gpt-4o-mini" (128k), not "gpt-4o" (128k but different caps).
	caps, ok := LookupModelCapabilities("gpt-4o-mini-2025-04-09")
	if !ok {
		t.Fatal("expected gpt-4o-mini-* to match")
	}
	// gpt-4o has Vision=true, gpt-4o-mini also has Vision=true — but gpt-4o-mini has different MaxOutputTokens.
	// The key test: it must consistently match the more specific prefix.
	miniCaps, _ := LookupModelCapabilities("gpt-4o-mini")
	if caps.MaxOutputTokens != miniCaps.MaxOutputTokens {
		t.Errorf("prefix overlap: got MaxOutputTokens=%d, want %d (from gpt-4o-mini, not gpt-4o)",
			caps.MaxOutputTokens, miniCaps.MaxOutputTokens)
	}

	// "o3-mini-2025-01-31" must match "o3-mini", not "o3".
	caps2, ok := LookupModelCapabilities("o3-mini-2025-01-31")
	if !ok {
		t.Fatal("expected o3-mini-* to match")
	}
	o3MiniCaps, _ := LookupModelCapabilities("o3-mini")
	if caps2.MaxOutputTokens != o3MiniCaps.MaxOutputTokens {
		t.Errorf("prefix overlap: got MaxOutputTokens=%d, want %d (from o3-mini, not o3)",
			caps2.MaxOutputTokens, o3MiniCaps.MaxOutputTokens)
	}
}

func TestKnownCaps_OSeries(t *testing.T) {
	for _, model := range []string{"o3", "o3-mini", "o4-mini"} {
		caps, ok := LookupModelCapabilities(model)
		if !ok {
			t.Errorf("expected %s to be found", model)
			continue
		}
		if !caps.Thinking {
			t.Errorf("expected %s to support thinking", model)
		}
	}
}

// --- Mock types ---

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return nil, nil
}
func (m *mockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan StreamEvent, error) {
	return nil, nil
}

type mockCapProvider struct {
	mockProvider
	caps Capabilities
}

func (m *mockCapProvider) GetModelCapabilities(model string) Capabilities {
	return m.caps
}
