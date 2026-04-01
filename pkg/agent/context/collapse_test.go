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

// buildTestHistory creates a synthetic conversation with N iterations.
// Each iteration: user msg, assistant tool_call, user tool_result.
func buildTestHistory(iterations int, toolName string, resultSize int) []canonical.Message {
	var msgs []canonical.Message
	for i := 1; i <= iterations; i++ {
		callID := fmt.Sprintf("tc_%d", i)
		msgs = append(msgs,
			canonical.Message{
				Role:        "user",
				Content:     []canonical.Content{{Type: "text", Text: fmt.Sprintf("Question %d", i)}},
				Annotations: &canonical.MessageAnnotations{Iteration: i, TokenEstimate: 10},
			},
			canonical.Message{
				Role: "assistant",
				Content: []canonical.Content{{
					Type:     "tool_use",
					ToolCall: &canonical.ToolCall{ID: callID, Name: toolName, Input: json.RawMessage(`{}`)},
				}},
				Annotations: &canonical.MessageAnnotations{Iteration: i, TokenEstimate: 15},
			},
			canonical.Message{
				Role: "user",
				Content: []canonical.Content{{
					Type:       "tool_result",
					ToolResult: &canonical.ToolResult{ToolCallID: callID, Content: strings.Repeat("R", resultSize)},
				}},
				Annotations: &canonical.MessageAnnotations{Iteration: i, TokenEstimate: resultSize / 4},
			},
		)
	}
	return msgs
}

func TestCollapse_TriggersAboveThreshold(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("Summary of collapsed conversation.", nil)

	// 20 iterations, each tool result ~500 tokens → total ~10000+ tokens.
	history := buildTestHistory(20, "grep", 2000)

	// Budget 5000 tokens, threshold 0.7 → triggers at 3500 tokens.
	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// Should have fewer messages than original.
	if len(out) >= len(history) {
		t.Errorf("expected fewer messages after collapse, got %d vs %d", len(out), len(history))
	}

	// Should contain the summary text.
	found := false
	for _, msg := range out {
		for _, c := range msg.Content {
			if strings.Contains(c.Text, "Summary of collapsed conversation") {
				found = true
			}
		}
	}
	if !found {
		t.Error("summary message not found in output")
	}

	// Collapse store should have an entry.
	if !cs.HasCollapses("sess1") {
		t.Error("collapse store should have entries")
	}
}

func TestCollapse_NoTriggerBelowThreshold(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("should not be called", nil)

	// Small conversation — well under budget.
	history := buildTestHistory(3, "grep", 100)

	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 100000)

	if len(out) != len(history) {
		t.Errorf("should not collapse when under threshold, got %d vs %d", len(out), len(history))
	}
}

func TestCollapse_ProtectsFirstUserMessage(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("Summary", nil)

	history := buildTestHistory(20, "grep", 2000)

	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// First message should still be present.
	if out[0].Role != "user" || !strings.Contains(out[0].Content[0].Text, "Question 1") {
		t.Error("first user message should be protected")
	}
}

func TestCollapse_ProtectsLastMessages(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("Summary", nil)

	history := buildTestHistory(20, "grep", 2000)
	lastMsg := history[len(history)-1]

	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// Last message from original should be in output.
	outLast := out[len(out)-1]
	if outLast.Content[0].ToolResult != nil {
		if outLast.Content[0].ToolResult.ToolCallID != lastMsg.Content[0].ToolResult.ToolCallID {
			t.Error("last messages should be protected")
		}
	}
}

func TestCollapse_ProtectsWriteToolResults(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("Summary", nil)

	// Build history with a mix of read and write tools.
	history := buildTestHistory(20, "edit_file", 2000)

	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// edit_file is a write tool — should not be collapsed.
	// Since all messages have write tools, nothing should be collapsible.
	// Result should be same size (nothing collapsed).
	if len(out) < len(history) {
		t.Error("write tool results should be protected from collapse")
	}
}

func TestCollapse_LLMFailureGraceful(t *testing.T) {
	cs := NewCollapseStore()
	caller := mockLLMCaller("", fmt.Errorf("timeout"))

	history := buildTestHistory(20, "grep", 2000)

	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// Should return original on LLM failure.
	if len(out) != len(history) {
		t.Error("should return original on LLM failure")
	}
}

func TestCollapse_NilStore(t *testing.T) {
	caller := mockLLMCaller("Summary", nil)
	history := buildTestHistory(20, "grep", 2000)

	out := applyCollapse(context.Background(), "sess1", history, nil, caller, 0.7, 5000)

	if len(out) != len(history) {
		t.Error("nil store should pass through")
	}
}

func TestProjectView_ReplacesCollapsedRange(t *testing.T) {
	history := buildTestHistory(10, "grep", 100)

	collapses := []CollapseEntry{
		{StartIter: 1, EndIter: 5, Iterations: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}, Summary: "First half summary"},
	}

	view := projectView(history, collapses)

	// Should have summary message + remaining messages.
	foundSummary := false
	for _, msg := range view {
		for _, c := range msg.Content {
			if strings.Contains(c.Text, "First half summary") {
				foundSummary = true
			}
		}
	}
	if !foundSummary {
		t.Error("summary not found in projected view")
	}

	// Messages from iterations 6-10 should still be present.
	for _, msg := range view {
		if msg.Annotations != nil && msg.Annotations.Iteration >= 6 {
			return // Found at least one.
		}
	}
	t.Error("non-collapsed messages missing from view")
}

func TestProjectView_OriginalUnchanged(t *testing.T) {
	history := buildTestHistory(10, "grep", 100)
	originalLen := len(history)

	collapses := []CollapseEntry{
		{StartIter: 1, EndIter: 5, Iterations: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true}, Summary: "Summary"},
	}

	_ = projectView(history, collapses)

	if len(history) != originalLen {
		t.Error("original history was mutated")
	}
}

func TestProjectView_EmptyCollapses(t *testing.T) {
	history := buildTestHistory(5, "grep", 100)

	view := projectView(history, nil)

	if len(view) != len(history) {
		t.Error("empty collapses should return original")
	}
}

func TestCollapse_ReusesExistingEntries(t *testing.T) {
	cs := NewCollapseStore()

	// Pre-populate with a collapse entry.
	cs.Add("sess1", CollapseEntry{
		StartIter:  1,
		EndIter:    5,
		Iterations: map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true},
		Summary:    "Cached summary",
		CreatedAt:  time.Now(),
	})

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "new summary", nil
	}

	history := buildTestHistory(10, "grep", 2000)
	out := applyCollapse(context.Background(), "sess1", history, cs, caller, 0.7, 5000)

	// Should use cached summary, not call LLM.
	if called {
		t.Error("LLM should not be called when cached collapses exist")
	}

	// Output should contain the cached summary.
	found := false
	for _, msg := range out {
		for _, c := range msg.Content {
			if strings.Contains(c.Text, "Cached summary") {
				found = true
			}
		}
	}
	if !found {
		t.Error("cached summary not found in output")
	}
}
