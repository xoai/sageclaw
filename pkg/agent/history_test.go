package agent

import (
	"fmt"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestLimitTurns_FitsInBudget(t *testing.T) {
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi there"}}},
	}

	result := PrepareHistory(msgs, 100, HistoryConfig{ContextWindow: 200000, MaxRatio: 0.85})
	if len(result) != 2 {
		t.Errorf("expected 2 messages when fits in budget, got %d", len(result))
	}
}

func TestLimitTurns_DropsOldTurns(t *testing.T) {
	// Create many turns to exceed a tiny budget.
	var msgs []canonical.Message
	for i := 0; i < 50; i++ {
		msgs = append(msgs,
			canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("word ", 100)}}},
			canonical.Message{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("reply ", 100)}}},
		)
	}

	result := PrepareHistory(msgs, 100, HistoryConfig{ContextWindow: 2000, MaxRatio: 0.85})
	if len(result) >= len(msgs) {
		t.Errorf("expected fewer messages after limiting, got %d (original %d)", len(result), len(msgs))
	}
	if len(result) < 2 {
		t.Errorf("expected at least 2 messages (first + last turn), got %d", len(result))
	}

	// First message should be the first user message (protected).
	if result[0].Role != "user" {
		t.Errorf("first message should be user (protected), got %s", result[0].Role)
	}
}

func TestPruneToolResults_NoPruningBelowThreshold(t *testing.T) {
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "test"}}},
		{Role: "assistant", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "1", Content: strings.Repeat("x", 5000)},
		}}},
	}

	// With a huge context window, ratio < 0.3 — no pruning.
	result := pruneToolResults(msgs, 1000000)
	if result[1].Content[0].ToolResult.Content != msgs[1].Content[0].ToolResult.Content {
		t.Error("should not prune when ratio < 0.3")
	}
}

func TestPruneToolResults_SoftTrim(t *testing.T) {
	// Use varied content (not repetitive chars) so tokenizer doesn't compress it.
	var parts []string
	for i := 0; i < 400; i++ {
		parts = append(parts, fmt.Sprintf("Line %d: The quick brown fox jumps over the lazy dog.", i))
	}
	bigResult := strings.Join(parts, "\n") // ~20000 chars, ~5000 tokens.

	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "test"}}},
		{Role: "user", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "1", Content: bigResult},
		}}},
	}

	// Context window sized so ratio is between 0.3 and 0.5.
	result := pruneToolResults(msgs, 12000)
	content := result[1].Content[0].ToolResult.Content
	if len(content) >= len(bigResult) {
		t.Errorf("expected soft trim to reduce content: original=%d, got=%d", len(bigResult), len(content))
	}
	if !strings.Contains(content, "trimmed") {
		t.Errorf("expected trimmed marker in content, got: %s", content[:min(200, len(content))])
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestSanitize_RemovesOrphanedToolResults(t *testing.T) {
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "test"}}},
		{Role: "user", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "orphan", Content: "result"},
		}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "response"}}},
	}

	result := sanitize(msgs)
	// The orphaned tool result should be removed.
	for _, msg := range result {
		for _, c := range msg.Content {
			if c.ToolResult != nil && c.ToolResult.ToolCallID == "orphan" {
				t.Error("orphaned tool result should have been removed")
			}
		}
	}
}

func TestNeedsCompaction_MessageCount(t *testing.T) {
	var msgs []canonical.Message
	for i := 0; i < 60; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	if !NeedsCompaction(msgs, 200000, 50, 0.75) {
		t.Error("should need compaction with 60 messages (threshold 50)")
	}
}

func TestNeedsCompaction_BelowThreshold(t *testing.T) {
	var msgs []canonical.Message
	for i := 0; i < 10; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	if NeedsCompaction(msgs, 200000, 50, 0.75) {
		t.Error("should not need compaction with 10 messages")
	}
}

func TestCompactionSplit_KeepsRecent(t *testing.T) {
	var msgs []canonical.Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}})
	}

	toCompact, toKeep := CompactionSplit(msgs, 0.30, 4)
	if len(toKeep) < 4 {
		t.Errorf("expected at least 4 kept messages, got %d", len(toKeep))
	}
	if len(toCompact)+len(toKeep) != len(msgs) {
		t.Errorf("split should preserve all messages: %d + %d != %d", len(toCompact), len(toKeep), len(msgs))
	}
}

func TestInjectSummary(t *testing.T) {
	toKeep := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "recent msg"}}},
	}

	result := InjectSummary("This is a summary of the conversation.", toKeep)
	if len(result) != 2 {
		t.Errorf("expected 2 messages (summary + kept), got %d", len(result))
	}
	if result[0].Role != "assistant" {
		t.Error("summary should be an assistant message")
	}
	if !strings.Contains(result[0].Content[0].Text, "summary") {
		t.Error("summary message should contain summary text")
	}
}
