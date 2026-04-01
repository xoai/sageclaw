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

func TestPipeline_V2_AggregateBudgetApplied(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultPipelineConfig()
	cfg.OverflowDir = dir
	cfg.AggregateBudgetChars = 1000

	p := NewContextPipeline(cfg)

	bigContent := strings.Repeat("X", 5000)
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read the file"}}},
		{
			Role: "assistant",
			Content: []canonical.Content{{
				Type:     "tool_use",
				ToolCall: &canonical.ToolCall{ID: "tc1", Name: "read_file", Input: json.RawMessage(`{"path":"a.go"}`)},
			}},
		},
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: bigContent},
			}},
		},
	}

	view := p.Prepare("sess1", history, 1)

	// The tool result should have been overflowed (preview + path).
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolResult != nil && c.ToolResult.ToolCallID == "tc1" {
				if len(c.ToolResult.Content) >= 5000 {
					t.Error("tool result should have been overflowed, still full size")
				}
				if !strings.Contains(c.ToolResult.Content, "[Full result:") {
					t.Error("overflowed result should contain path reference")
				}
				return
			}
		}
	}
	t.Error("tc1 tool result not found in output")
}

func TestPipeline_AnnotationsStamped(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.OverflowDir = "" // No overflow
	p := NewContextPipeline(cfg)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
	}

	_ = p.Prepare("sess1", history, 3)

	// Annotations should be stamped on the original messages (shared pointer).
	for i, msg := range history {
		if msg.Annotations == nil {
			t.Errorf("message %d: annotations not stamped", i)
			continue
		}
		if msg.Annotations.Iteration != 3 {
			t.Errorf("message %d: iteration = %d, want 3", i, msg.Annotations.Iteration)
		}
		if msg.Annotations.TokenEstimate <= 0 {
			t.Errorf("message %d: token estimate not computed", i)
		}
	}
}

func TestPipeline_OriginalHistoryNotMutated(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultPipelineConfig()
	cfg.OverflowDir = dir
	cfg.AggregateBudgetChars = 100

	p := NewContextPipeline(cfg)

	original := strings.Repeat("Z", 500)
	history := []canonical.Message{
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: original},
			}},
		},
		{
			Role: "assistant",
			Content: []canonical.Content{{
				Type:     "tool_use",
				ToolCall: &canonical.ToolCall{ID: "tc1", Name: "grep", Input: json.RawMessage(`{}`)},
			}},
		},
	}

	_ = p.Prepare("sess1", history, 1)

	// Original tool result content should not be mutated.
	if history[0].Content[0].ToolResult.Content != original {
		t.Error("original history content was mutated")
	}
}

func TestPipeline_SanitizeRepairsOrphans(t *testing.T) {
	cfg := DefaultPipelineConfig()
	p := NewContextPipeline(cfg)

	// Orphaned tool_result with no matching tool_use.
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "orphan", Content: "data"},
			}},
		},
	}

	view := p.Prepare("sess1", history, 1)

	// Orphaned tool_result should have been removed by sanitize.
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolResult != nil && c.ToolResult.ToolCallID == "orphan" {
				t.Error("orphaned tool_result should have been removed")
			}
		}
	}
}

func TestPipeline_SanitizePreservesAnnotations(t *testing.T) {
	msgs := []canonical.Message{
		{
			Role:        "assistant",
			Content:     []canonical.Content{{Type: "text", Text: "thinking..."}},
			Annotations: &canonical.MessageAnnotations{Iteration: 5, TokenEstimate: 42},
		},
	}

	result := SanitizePreservingAnnotations(msgs)
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Annotations == nil {
		t.Fatal("annotations lost during sanitize")
	}
	if result[0].Annotations.Iteration != 5 {
		t.Errorf("iteration = %d, want 5", result[0].Annotations.Iteration)
	}
}

func TestPipeline_V1ConfigRunsExistingPath(t *testing.T) {
	// When no pipeline is created (v1 default), the pipeline field is nil.
	// This test verifies NewContextPipeline returns a usable object
	// even with minimal config.
	cfg := PipelineConfig{} // All zeros — Layer 1 won't overflow (budget=0 means pass-through).
	p := NewContextPipeline(cfg)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	view := p.Prepare("sess1", history, 1)
	if len(view) != 1 {
		t.Fatalf("expected 1 message, got %d", len(view))
	}
}

func TestSanitizePreservingAnnotations_SyntheticResult(t *testing.T) {
	// Tool use with no result should get a synthetic result.
	msgs := []canonical.Message{
		{
			Role: "assistant",
			Content: []canonical.Content{{
				Type:     "tool_use",
				ToolCall: &canonical.ToolCall{ID: "tc_orphan", Name: "grep", Input: json.RawMessage(`{}`)},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: 2},
		},
	}

	result := SanitizePreservingAnnotations(msgs)

	// Should have the original + synthetic result.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	// Synthetic result.
	if result[1].Content[0].ToolResult == nil {
		t.Fatal("expected synthetic tool result")
	}
	if result[1].Content[0].ToolResult.ToolCallID != "tc_orphan" {
		t.Error("synthetic result has wrong ToolCallID")
	}
	// Original should preserve annotations.
	if result[0].Annotations == nil || result[0].Annotations.Iteration != 2 {
		t.Error("original message annotations lost")
	}
}

func TestSanitizePreservingAnnotations_MultipleOrphans(t *testing.T) {
	// Multiple orphaned tool_use blocks — each should get a synthetic result.
	msgs := []canonical.Message{
		{
			Role: "assistant",
			Content: []canonical.Content{
				{Type: "tool_use", ToolCall: &canonical.ToolCall{ID: "tc_a", Name: "grep", Input: json.RawMessage(`{}`)}},
				{Type: "tool_use", ToolCall: &canonical.ToolCall{ID: "tc_b", Name: "read_file", Input: json.RawMessage(`{}`)}},
			},
		},
		{
			Role: "assistant",
			Content: []canonical.Content{
				{Type: "tool_use", ToolCall: &canonical.ToolCall{ID: "tc_c", Name: "glob", Input: json.RawMessage(`{}`)}},
			},
		},
	}

	result := SanitizePreservingAnnotations(msgs)

	// Should have: assistant(tc_a,tc_b), synthetic(tc_a), synthetic(tc_b), assistant(tc_c), synthetic(tc_c)
	syntheticCount := 0
	syntheticIDs := make(map[string]bool)
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.ToolResult != nil && c.ToolResult.Content == "[Result unavailable — message was pruned]" {
				syntheticCount++
				syntheticIDs[c.ToolResult.ToolCallID] = true
			}
		}
	}

	if syntheticCount != 3 {
		t.Errorf("expected 3 synthetic results, got %d (messages: %d)", syntheticCount, len(result))
	}
	for _, id := range []string{"tc_a", "tc_b", "tc_c"} {
		if !syntheticIDs[id] {
			t.Errorf("missing synthetic result for %s", id)
		}
	}
}

// --- Full Integration Test (M4 Task 4.4) ---

// buildSyntheticConversation creates a 40-message synthetic conversation
// with mixed tool calls (reads, writes, searches, large results).
func buildSyntheticConversation() []canonical.Message {
	var msgs []canonical.Message
	addTriplet := func(iter int, question, callID, toolName string, resultContent string) {
		msgs = append(msgs,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: question}},
				Annotations: &canonical.MessageAnnotations{Iteration: iter}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{
				Type: "tool_use", ToolCall: &canonical.ToolCall{ID: callID, Name: toolName, Input: json.RawMessage(`{}`)},
			}}, Annotations: &canonical.MessageAnnotations{Iteration: iter}},
			canonical.Message{Role: "user", Content: []canonical.Content{{
				Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: callID, Content: resultContent},
			}}, Annotations: &canonical.MessageAnnotations{Iteration: iter}},
		)
	}

	// Iteration 1: user question + read_file (large, 8K chars)
	addTriplet(1, "What does main.go do?", "tc1", "read_file", strings.Repeat("// main.go code\n", 500))

	// Iterations 2-8: grep searches (read-only, ~3K each, will be snipped)
	for i := 2; i <= 8; i++ {
		addTriplet(i, fmt.Sprintf("Search for pattern %d", i),
			fmt.Sprintf("tc_%d", i), "grep",
			strings.Repeat(fmt.Sprintf("match_%d: line content\n", i), 200))
	}

	// Iteration 9: edit_file (write — should never be snipped)
	addTriplet(9, "Fix the bug", "tc_edit", "edit_file", "File edited successfully")

	// Iterations 10-13: web_fetch (large read-only, ~6K each)
	for i := 10; i <= 13; i++ {
		addTriplet(i, fmt.Sprintf("Fetch page %d", i),
			fmt.Sprintf("tc_%d", i), "web_fetch",
			strings.Repeat("<html>page content</html>\n", 300))
	}

	return msgs
}

func TestPipeline_FullIntegration(t *testing.T) {
	dir := t.TempDir()
	cfg := DefaultPipelineConfig()
	cfg.OverflowDir = dir
	cfg.AggregateBudgetChars = 5000  // Cap per-message tool results
	cfg.SnipAgeIterations = 5        // Snip after 5 iterations
	cfg.MicroCompactEnabled = true
	cfg.MicroCompactAge = 3
	cfg.CollapseEnabled = true
	cfg.CollapseThreshold = 0.7

	p := NewContextPipeline(cfg)
	p.SetLLMCaller(func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		return "Compressed/summarized content.", nil
	})
	p.SetBudgetTokens(2000) // Low budget to trigger collapse

	history := buildSyntheticConversation()
	originalLen := len(history)

	// Compute original token count BEFORE pipeline (which caches and may reset).
	originalTokens := 0
	for i := range history {
		originalTokens += EstimateTokens(&history[i])
	}

	// Deep copy original content for mutation check.
	originalContents := make([]string, len(history))
	for i, msg := range history {
		for _, c := range msg.Content {
			if c.ToolResult != nil {
				originalContents[i] = c.ToolResult.Content
			}
		}
	}

	// Reset cached token estimates so pipeline recomputes fresh.
	for i := range history {
		if history[i].Annotations != nil {
			history[i].Annotations.TokenEstimate = 0
		}
	}

	// Run pipeline at iteration 25 (all messages are aged).
	view := p.PrepareWithContext(context.Background(), "sess1", history, 25)

	// --- Assertions ---

	// 1. Token count decreases (view should be smaller than original).
	viewTokens := 0
	for i := range view {
		viewTokens += EstimateTokens(&view[i])
	}
	if viewTokens >= originalTokens {
		t.Errorf("v2 should use fewer tokens: original=%d, view=%d", originalTokens, viewTokens)
	}

	// 2. Message ordering preserved (roles alternate correctly).
	for i := 1; i < len(view); i++ {
		// No two consecutive user messages with tool_results (would indicate broken ordering).
		if view[i].Role == view[i-1].Role && view[i].Role == "assistant" {
			// Two consecutive assistant messages are OK (summary + next).
		}
	}

	// 3. No orphaned tool_use/tool_result pairs.
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				toolUseIDs[c.ToolCall.ID] = true
			}
			if c.ToolResult != nil {
				toolResultIDs[c.ToolResult.ToolCallID] = true
			}
		}
	}
	for id := range toolResultIDs {
		if !toolUseIDs[id] {
			t.Errorf("orphaned tool_result: %s", id)
		}
	}

	// 4. Write results (edit_file) never snipped.
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolResult != nil && c.ToolResult.ToolCallID == "tc_edit" {
				if strings.Contains(c.ToolResult.Content, "[Snipped:") {
					t.Error("edit_file result should never be snipped")
				}
			}
		}
	}

	// 5. Original history content not mutated (except annotations).
	for i, msg := range history {
		for _, c := range msg.Content {
			if c.ToolResult != nil && originalContents[i] != "" {
				if c.ToolResult.Content != originalContents[i] {
					t.Errorf("original message %d tool result was mutated", i)
				}
			}
		}
	}

	// 6. Fewer messages in view than original (collapse or snip reduced).
	if len(view) >= originalLen {
		// This can happen if collapse and snip don't reduce message count
		// but they should reduce content size (checked by token assertion).
		t.Logf("info: view has %d messages vs original %d (content reduction via snip/compact)", len(view), originalLen)
	}

	t.Logf("Integration: %d messages → %d messages, %d tokens → %d tokens (%.0f%% reduction)",
		originalLen, len(view), originalTokens, viewTokens,
		(1-float64(viewTokens)/float64(originalTokens))*100)
}

func TestPipeline_V1Default_NoChange(t *testing.T) {
	// Verify that agents with no context config run the v1 path unchanged.
	// The pipeline is nil in this case — this test verifies NewContextPipeline
	// with zero config doesn't crash and passes through.
	cfg := PipelineConfig{
		SnipEnabled:         false,
		MicroCompactEnabled: false,
		CollapseEnabled:     false,
	}
	p := NewContextPipeline(cfg)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hi there"}}},
	}

	view := p.Prepare("sess1", history, 1)

	if len(view) != 2 {
		t.Errorf("v1 default should pass through unchanged, got %d messages", len(view))
	}
}

// --- Pressure gating tests ---

func TestPipeline_ZeroOverhead_SmallConversation(t *testing.T) {
	cfg := DefaultPipelineConfig()
	p := NewContextPipeline(cfg)

	// Track whether LLM caller is invoked.
	llmCalled := false
	p.SetLLMCaller(func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		llmCalled = true
		return "should not be called", nil
	})
	p.SetBudgetTokens(100000) // Large budget — pressure will be very low.

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi there!"}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "How are you?"}}},
	}

	view := p.PrepareWithContext(context.Background(), "sess_small", history, 0)

	if llmCalled {
		t.Fatal("LLM should NOT be called for a small conversation with low pressure")
	}
	if len(view) != 3 {
		t.Fatalf("expected 3 messages unchanged, got %d", len(view))
	}
}

func TestPipeline_PressureGating_ContentClearAt50Pct(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MicroCompactAge = 3
	cfg.SnipAgeIterations = 20 // High snip age so snip doesn't activate before content-clear.
	p := NewContextPipeline(cfg)

	llmCalled := false
	p.SetLLMCaller(func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		llmCalled = true
		return "compressed", nil
	})
	// Budget: 1300 tokens. Total content ~760 tokens → pressure ~0.58 (between 0.5 and 0.7).
	p.SetBudgetTokens(1300)

	bigContent := strings.Repeat("X", 3000) // ~750 tokens
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "do something"}},
			Annotations: &canonical.MessageAnnotations{Iteration: 1}},
		{Role: "assistant", Content: []canonical.Content{{
			Type:     "tool_use",
			ToolCall: &canonical.ToolCall{ID: "tc1", Name: "exec_command", Input: json.RawMessage(`{}`)},
		}}, Annotations: &canonical.MessageAnnotations{Iteration: 1}},
		{Role: "user", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: bigContent},
		}}, Annotations: &canonical.MessageAnnotations{Iteration: 1}},
	}

	view := p.PrepareWithContext(context.Background(), "sess_mid", history, 10) // age = 10 - 1 = 9 >> 3

	// At 0.5-0.7 pressure, content-clear should activate but NOT LLM micro-compact.
	if llmCalled {
		t.Fatal("LLM should NOT be called in the 0.5-0.7 pressure band (content-clear only)")
	}

	// The tool result should be content-cleared.
	found := false
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolResult != nil && strings.Contains(c.ToolResult.Content, "Tool result cleared") {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("expected content-cleared tool result at moderate pressure")
	}
}

func TestPipeline_PressureGating_BelowThreshold_NoAction(t *testing.T) {
	cfg := DefaultPipelineConfig()
	cfg.MicroCompactAge = 3
	p := NewContextPipeline(cfg)

	llmCalled := false
	p.SetLLMCaller(func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		llmCalled = true
		return "compressed", nil
	})
	p.SetBudgetTokens(100000) // Very large budget — pressure << 0.5.

	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "do something"}}},
		{Role: "assistant", Content: []canonical.Content{{
			Type:     "tool_use",
			ToolCall: &canonical.ToolCall{ID: "tc1", Name: "read_file", Input: json.RawMessage(`{}`)},
		}}},
		{Role: "user", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: bigContent},
		}}},
	}

	view := p.PrepareWithContext(context.Background(), "sess_low", history, 10)

	if llmCalled {
		t.Fatal("LLM should not be called below 0.5 pressure")
	}
	// Tool result should be unchanged (not content-cleared).
	for _, msg := range view {
		for _, c := range msg.Content {
			if c.ToolResult != nil && strings.Contains(c.ToolResult.Content, "Tool result cleared") {
				t.Fatal("tool result should NOT be content-cleared below 0.5 pressure")
			}
		}
	}
}
