package context

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// makeConversation builds a minimal assistant tool_call + user tool_result pair.
func makeConversation(callID, toolName, resultContent string, iteration int) []canonical.Message {
	return []canonical.Message{
		{
			Role: "assistant",
			Content: []canonical.Content{{
				Type:     "tool_use",
				ToolCall: &canonical.ToolCall{ID: callID, Name: toolName, Input: json.RawMessage(`{}`)},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: iteration},
		},
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: callID, Content: resultContent},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: iteration},
		},
	}
}

func TestSnip_AgeThreshold(t *testing.T) {
	// Old read_file result (iteration 1, current 10, snipAge 8) → should snip.
	msgs := makeConversation("tc1", "read_file", strings.Repeat("X", 1000), 1)

	out := applySnip(context.Background(), msgs, 10, 8, 0, nil) // protectedCount=0 to not protect

	tr := out[1].Content[0].ToolResult
	if !strings.Contains(tr.Content, "[Snipped:") {
		t.Error("expected snipped marker, got:", tr.Content[:50])
	}
	if !out[1].Annotations.Snipped {
		t.Error("Snipped annotation not set")
	}
}

func TestSnip_TooYoung(t *testing.T) {
	// Recent result (iteration 5, current 10, snipAge 8) → should NOT snip.
	msgs := makeConversation("tc1", "read_file", "content", 5)

	out := applySnip(context.Background(), msgs, 10, 8, 0, nil)

	if out[1].Content[0].ToolResult.Content != "content" {
		t.Error("young message should not be snipped")
	}
}

func TestSnip_WriteToolNeverSnipped(t *testing.T) {
	// edit_file is a write tool → never snipped regardless of age.
	msgs := makeConversation("tc1", "edit_file", "OK", 1)

	out := applySnip(context.Background(), msgs, 100, 8, 0, nil)

	if out[1].Content[0].ToolResult.Content != "OK" {
		t.Error("write tool result should never be snipped")
	}
}

func TestSnip_ProtectedZone(t *testing.T) {
	// Last 3 tool_result messages are protected.
	var history []canonical.Message
	for i := 0; i < 5; i++ {
		history = append(history, makeConversation(
			"tc"+string(rune('A'+i)), "grep", strings.Repeat("D", 500), 1,
		)...)
	}

	out := applySnip(context.Background(), history, 100, 1, 3, nil)

	// Messages at indices 1, 3, 5 are tool_results (iterations 0-4).
	// Last 3 tool_results: indices 9, 7, 5 → protected.
	// Indices 1 and 3 should be snipped.
	snippedCount := 0
	for _, msg := range out {
		for _, c := range msg.Content {
			if c.ToolResult != nil && strings.Contains(c.ToolResult.Content, "[Snipped:") {
				snippedCount++
			}
		}
	}
	if snippedCount != 2 {
		t.Errorf("expected 2 snipped results, got %d", snippedCount)
	}
}

func TestSnip_AlreadySnipped(t *testing.T) {
	msgs := makeConversation("tc1", "read_file", "[Snipped: ...]", 1)
	msgs[1].Annotations.Snipped = true

	out := applySnip(context.Background(), msgs, 100, 1, 0, nil)

	// Should not double-snip.
	if out[1].Content[0].ToolResult.Content != "[Snipped: ...]" {
		t.Error("already snipped message was modified")
	}
}

func TestSnip_UseSummaryWhenAvailable(t *testing.T) {
	msgs := makeConversation("tc1", "grep", strings.Repeat("X", 500), 1)
	msgs[1].Annotations.Summary = "Found 3 matches in main.go"

	out := applySnip(context.Background(), msgs, 100, 1, 0, nil)

	tr := out[1].Content[0].ToolResult
	if !strings.Contains(tr.Content, "Found 3 matches in main.go") {
		t.Error("summary not used in snip marker:", tr.Content)
	}
}

func TestSnip_OriginalNotMutated(t *testing.T) {
	original := strings.Repeat("Y", 200)
	msgs := makeConversation("tc1", "read_file", original, 1)

	_ = applySnip(context.Background(), msgs, 100, 1, 0, nil)

	if msgs[1].Content[0].ToolResult.Content != original {
		t.Error("original message was mutated")
	}
}

func TestSnip_TokenEstimateReset(t *testing.T) {
	msgs := makeConversation("tc1", "glob", strings.Repeat("Z", 300), 1)
	msgs[1].Annotations.TokenEstimate = 100

	out := applySnip(context.Background(), msgs, 100, 1, 0, nil)

	if out[1].Annotations.TokenEstimate != 0 {
		t.Errorf("token estimate should be reset, got %d", out[1].Annotations.TokenEstimate)
	}
}

func TestSnip_MixedToolsNotSnipped(t *testing.T) {
	// Message with both read-only and write tool results → not snippable.
	msgs := []canonical.Message{
		{
			Role: "assistant",
			Content: []canonical.Content{
				{Type: "tool_use", ToolCall: &canonical.ToolCall{ID: "tc_read", Name: "grep", Input: json.RawMessage(`{}`)}},
				{Type: "tool_use", ToolCall: &canonical.ToolCall{ID: "tc_write", Name: "edit_file", Input: json.RawMessage(`{}`)}},
			},
			Annotations: &canonical.MessageAnnotations{Iteration: 1},
		},
		{
			Role: "user",
			Content: []canonical.Content{
				{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "tc_read", Content: "matches"}},
				{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "tc_write", Content: "ok"}},
			},
			Annotations: &canonical.MessageAnnotations{Iteration: 1},
		},
	}

	out := applySnip(context.Background(), msgs, 100, 1, 0, nil)

	// Neither result should be snipped since the message has mixed tools.
	for _, c := range out[1].Content {
		if c.ToolResult != nil && strings.Contains(c.ToolResult.Content, "[Snipped:") {
			t.Error("mixed tool message should not be snipped")
		}
	}
}

func TestSnip_DisabledWhenSnipAgeZero(t *testing.T) {
	msgs := makeConversation("tc1", "read_file", "content", 1)

	out := applySnip(context.Background(), msgs, 100, 0, 0, nil)

	if out[1].Content[0].ToolResult.Content != "content" {
		t.Error("snip should be disabled when snipAge=0")
	}
}

func TestSnip_LazySummary_GeneratedWhenSnipping(t *testing.T) {
	bigContent := strings.Repeat("Found 3 matches in main.go\n", 50) // >100 chars
	msgs := makeConversation("tc1", "grep", bigContent, 1)

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "grep found 3 matches in main.go", nil
	}

	out := applySnip(context.Background(), msgs, 100, 1, 0, caller)

	if !called {
		t.Fatal("LLM caller should be invoked for lazy summary")
	}
	result := out[1].Content[0].ToolResult.Content
	if !strings.Contains(result, "grep found 3 matches") {
		t.Errorf("snip marker should contain lazy summary, got: %s", result)
	}
}

func TestSnip_LazySummary_FallbackWithoutCaller(t *testing.T) {
	bigContent := strings.Repeat("X", 200)
	msgs := makeConversation("tc1", "grep", bigContent, 1)

	out := applySnip(context.Background(), msgs, 100, 1, 0, nil)

	result := out[1].Content[0].ToolResult.Content
	if !strings.Contains(result, "grep result from iteration") {
		t.Errorf("should use opaque marker without LLM caller, got: %s", result)
	}
}

func TestSnip_LazySummary_FallbackOnError(t *testing.T) {
	bigContent := strings.Repeat("X", 200)
	msgs := makeConversation("tc1", "grep", bigContent, 1)

	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		return "", fmt.Errorf("timeout")
	}

	out := applySnip(context.Background(), msgs, 100, 1, 0, caller)

	result := out[1].Content[0].ToolResult.Content
	if !strings.Contains(result, "grep result from iteration") {
		t.Errorf("should fall back to opaque marker on LLM error, got: %s", result)
	}
}
