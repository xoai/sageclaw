package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

func specRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.RegisterFull("read_file", "Read", nil, "fs", "moderate", "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "speculated content"}, nil
		})
	reg.RegisterWithGroup("edit", "Edit", nil, "fs", "moderate", "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "edited"}, nil
		})
	reg.RegisterWithGroup("write_file", "Write", nil, "fs", "moderate", "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "written"}, nil
		})
	return reg
}

func TestSpeculativeEngine_PatternMatch(t *testing.T) {
	reg := specRegistry()
	engine := NewSpeculativeEngine(reg)

	editCall := canonical.ToolCall{
		ID:    "1",
		Name:  "edit",
		Input: json.RawMessage(`{"path":"main.go","old_string":"foo","new_string":"bar"}`),
	}
	editResult := &canonical.ToolResult{Content: "edited"}

	// Trigger speculative execution.
	engine.OnToolResult(editCall, editResult, reg, func(name string, input json.RawMessage) (*canonical.ToolResult, error) {
		return reg.Execute(context.Background(), name, input)
	})

	// Wait for background goroutine.
	time.Sleep(50 * time.Millisecond)

	// Check cache for the anticipated read_file call.
	readCall := canonical.ToolCall{
		ID:    "2",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"main.go"}`),
	}

	result := engine.CheckCache(readCall)
	if result == nil {
		t.Fatal("expected speculative cache hit")
	}
	if result.Content != "speculated content" {
		t.Errorf("got %q, want 'speculated content'", result.Content)
	}
	if result.ToolCallID != "2" {
		t.Errorf("expected ToolCallID '2', got %q", result.ToolCallID)
	}
}

func TestSpeculativeEngine_CacheMiss(t *testing.T) {
	reg := specRegistry()
	engine := NewSpeculativeEngine(reg)

	// No trigger — cache should be empty.
	readCall := canonical.ToolCall{
		ID:    "1",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"nonexistent.go"}`),
	}
	result := engine.CheckCache(readCall)
	if result != nil {
		t.Error("expected nil for cache miss")
	}
}

func TestSpeculativeEngine_TTLExpiry(t *testing.T) {
	reg := specRegistry()
	engine := NewSpeculativeEngine(reg)

	// Manually inject a stale entry.
	key := cacheKey("read_file", json.RawMessage(`{"path":"old.go"}`))
	engine.mu.Lock()
	engine.cache[key] = &speculativeResult{
		Result:    &canonical.ToolResult{Content: "stale"},
		CreatedAt: time.Now().Add(-35 * time.Second), // Expired.
	}
	engine.mu.Unlock()

	readCall := canonical.ToolCall{
		ID:    "1",
		Name:  "read_file",
		Input: json.RawMessage(`{"path":"old.go"}`),
	}
	result := engine.CheckCache(readCall)
	if result != nil {
		t.Error("expected nil for expired cache entry")
	}
}

func TestSpeculativeEngine_OneShot(t *testing.T) {
	reg := specRegistry()
	engine := NewSpeculativeEngine(reg)

	key := cacheKey("read_file", json.RawMessage(`{"path":"once.go"}`))
	engine.mu.Lock()
	engine.cache[key] = &speculativeResult{
		Result:    &canonical.ToolResult{Content: "cached"},
		CreatedAt: time.Now(),
	}
	engine.mu.Unlock()

	readCall := canonical.ToolCall{ID: "1", Name: "read_file", Input: json.RawMessage(`{"path":"once.go"}`)}

	// First hit succeeds.
	result := engine.CheckCache(readCall)
	if result == nil {
		t.Fatal("first cache check should hit")
	}

	// Second hit misses (one-shot).
	result = engine.CheckCache(readCall)
	if result != nil {
		t.Error("second cache check should miss (one-shot)")
	}
}

func TestSpeculativeEngine_Cleanup(t *testing.T) {
	reg := specRegistry()
	engine := NewSpeculativeEngine(reg)

	// Add mixed entries.
	engine.mu.Lock()
	engine.cache["fresh"] = &speculativeResult{
		Result:    &canonical.ToolResult{Content: "fresh"},
		CreatedAt: time.Now(),
	}
	engine.cache["stale"] = &speculativeResult{
		Result:    &canonical.ToolResult{Content: "stale"},
		CreatedAt: time.Now().Add(-60 * time.Second),
	}
	engine.cache["nil-reservation"] = nil
	engine.mu.Unlock()

	engine.Cleanup()

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if len(engine.cache) != 1 {
		t.Errorf("expected 1 entry after cleanup, got %d", len(engine.cache))
	}
	if _, ok := engine.cache["fresh"]; !ok {
		t.Error("fresh entry should survive cleanup")
	}
}

func TestDerivePathInput(t *testing.T) {
	input := json.RawMessage(`{"path":"src/main.go","old_string":"foo","new_string":"bar"}`)
	derived := derivePathInput(input)
	if derived == nil {
		t.Fatal("expected non-nil derived input")
	}

	var params struct {
		Path string `json:"path"`
	}
	json.Unmarshal(derived, &params)
	if params.Path != "src/main.go" {
		t.Errorf("got path %q, want 'src/main.go'", params.Path)
	}

	// No path field.
	noPath := derivePathInput(json.RawMessage(`{"query":"hello"}`))
	if noPath != nil {
		t.Error("expected nil for input without path")
	}
}
