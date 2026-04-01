package context

import (
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestEnsureAnnotations_LazyInit(t *testing.T) {
	msg := canonical.Message{Role: "user"}
	if msg.Annotations != nil {
		t.Fatal("expected nil annotations before init")
	}

	ann := EnsureAnnotations(&msg)
	if ann == nil {
		t.Fatal("expected non-nil annotations after init")
	}

	// Idempotent — same pointer returned.
	ann2 := EnsureAnnotations(&msg)
	if ann != ann2 {
		t.Error("expected same pointer on second call")
	}
}

func TestAnnotateIteration(t *testing.T) {
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	AnnotateIteration(msgs, 3)

	for i, m := range msgs {
		if m.Annotations == nil {
			t.Fatalf("msg[%d] annotations nil", i)
		}
		if m.Annotations.Iteration != 3 {
			t.Errorf("msg[%d] iteration=%d, want 3", i, m.Annotations.Iteration)
		}
	}

	// Second call with different iteration — should NOT overwrite (non-zero).
	AnnotateIteration(msgs, 5)
	if msgs[0].Annotations.Iteration != 3 {
		t.Error("expected iteration to stay 3, not overwritten")
	}
}

func TestEstimateTokens_Caching(t *testing.T) {
	msg := canonical.Message{
		Role: "user",
		Content: []canonical.Content{{Type: "text", Text: "Hello, world!"}},
	}

	est1 := EstimateTokens(&msg)
	if est1 <= 0 {
		t.Fatalf("expected positive estimate, got %d", est1)
	}

	// Second call returns cached value.
	est2 := EstimateTokens(&msg)
	if est1 != est2 {
		t.Errorf("expected cached value %d, got %d", est1, est2)
	}
}

func TestEstimateTokens_ToolResult(t *testing.T) {
	msg := canonical.Message{
		Role: "tool",
		Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{Content: "a very long result text here"},
		}},
	}

	est := EstimateTokens(&msg)
	if est < 5 {
		t.Errorf("expected reasonable estimate for tool result, got %d", est)
	}
}

func TestIsSnippable(t *testing.T) {
	readOnly := map[string]bool{"read_file": true, "list_files": true}
	toolNames := map[string]string{"tc1": "read_file", "tc2": "write_file"}

	// All read-only — snippable.
	msg := canonical.Message{
		Role: "tool",
		Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: "data"},
		}},
	}
	if !IsSnippable(msg, toolNames, readOnly) {
		t.Error("expected snippable for read-only tool")
	}

	// Non-read-only — not snippable.
	msg2 := canonical.Message{
		Role: "tool",
		Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "tc2", Content: "ok"},
		}},
	}
	if IsSnippable(msg2, toolNames, readOnly) {
		t.Error("expected not snippable for write tool")
	}

	// Mixed — not snippable (no partial snippability).
	msg3 := canonical.Message{
		Role: "tool",
		Content: []canonical.Content{
			{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: "ok"}},
			{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "tc2", Content: "ok"}},
		},
	}
	if IsSnippable(msg3, toolNames, readOnly) {
		t.Error("expected not snippable for mixed tools")
	}

	// No tool results — not snippable.
	msg4 := canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: "hi"}},
	}
	if IsSnippable(msg4, toolNames, readOnly) {
		t.Error("expected not snippable for non-tool message")
	}
}

func TestCopyMessageWithAnnotations(t *testing.T) {
	msg := canonical.Message{
		Role:    "assistant",
		Content: []canonical.Content{{Type: "text", Text: "hello"}},
	}
	EnsureAnnotations(&msg)
	msg.Annotations.Iteration = 5

	cp := CopyMessageWithAnnotations(msg)

	// Same annotations pointer.
	if cp.Annotations != msg.Annotations {
		t.Error("expected same annotations pointer")
	}
	if cp.Role != msg.Role {
		t.Error("expected same role")
	}

	// Modifying copy's content doesn't affect original.
	cp.Content[0].Text = "modified"
	if msg.Content[0].Text == "modified" {
		t.Error("expected copy to not affect original content")
	}
}

func TestAnnotations_NotSerialized(t *testing.T) {
	msg := canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: "hi"}},
	}
	EnsureAnnotations(&msg)
	msg.Annotations.Iteration = 5

	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Annotations should NOT appear in JSON.
	var raw map[string]any
	json.Unmarshal(data, &raw)
	if _, ok := raw["Annotations"]; ok {
		t.Error("Annotations should not be serialized (json:\"-\")")
	}
	if _, ok := raw["annotations"]; ok {
		t.Error("annotations should not be serialized")
	}
}
