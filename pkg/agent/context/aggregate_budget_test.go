package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func makeToolResultMsg(callID, content string) canonical.Message {
	return canonical.Message{
		Role: "tool",
		Content: []canonical.Content{
			{ToolResult: &canonical.ToolResult{ToolCallID: callID, Content: content}},
		},
	}
}

func TestAggregateBudget_UnderBudget(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	history := []canonical.Message{
		makeToolResultMsg("tc1", strings.Repeat("A", 100)),
		makeToolResultMsg("tc2", strings.Repeat("B", 200)),
	}

	out := applyAggregateBudget("s1", history, om, 1000)
	if len(out) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(out))
	}
	// Content should be unchanged.
	if out[0].Content[0].ToolResult.Content != strings.Repeat("A", 100) {
		t.Error("message 0 content changed when under budget")
	}
	if out[1].Content[0].ToolResult.Content != strings.Repeat("B", 200) {
		t.Error("message 1 content changed when under budget")
	}
	// No overflow files should exist.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Error("overflow files created when under budget")
	}
}

func TestAggregateBudget_SingleLargeResult(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	bigContent := strings.Repeat("X", 5000)
	history := []canonical.Message{
		makeToolResultMsg("tc_big", bigContent),
	}

	out := applyAggregateBudget("s1", history, om, 1000)
	result := out[0].Content[0].ToolResult.Content

	// Should have preview (500 chars) + path reference.
	if !strings.HasPrefix(result, strings.Repeat("X", 500)) {
		t.Error("preview missing or wrong")
	}
	if !strings.Contains(result, "[Full result:") {
		t.Error("path reference missing")
	}

	// Overflow file should exist.
	overflowPath := filepath.Join(dir, "s1", "tc_big.txt")
	data, err := os.ReadFile(overflowPath)
	if err != nil {
		t.Fatalf("overflow file not found: %v", err)
	}
	if string(data) != bigContent {
		t.Error("overflow file content mismatch")
	}

	// Annotation should be set.
	if out[0].Annotations == nil || out[0].Annotations.OverflowPath == "" {
		t.Error("OverflowPath annotation not set")
	}
}

func TestAggregateBudget_MultipleResults_LargestOverflowed(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Single message with two tool results.
	history := []canonical.Message{
		{
			Role: "tool",
			Content: []canonical.Content{
				{ToolResult: &canonical.ToolResult{ToolCallID: "tc_small", Content: strings.Repeat("S", 300)}},
				{ToolResult: &canonical.ToolResult{ToolCallID: "tc_large", Content: strings.Repeat("L", 3000)}},
			},
		},
	}

	// Budget 1000: total is 3300, so the 3000-char result should overflow.
	out := applyAggregateBudget("s1", history, om, 1000)

	// Small result should be unchanged.
	if out[0].Content[0].ToolResult.Content != strings.Repeat("S", 300) {
		t.Error("small result was modified")
	}

	// Large result should have preview + path.
	largeContent := out[0].Content[1].ToolResult.Content
	if !strings.Contains(largeContent, "[Full result:") {
		t.Error("large result not overflowed")
	}
}

func TestAggregateBudget_OriginalUnmutated(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	original := strings.Repeat("X", 5000)
	history := []canonical.Message{
		makeToolResultMsg("tc1", original),
	}

	_ = applyAggregateBudget("s1", history, om, 1000)

	// Original history should not be mutated.
	if history[0].Content[0].ToolResult.Content != original {
		t.Error("original history was mutated")
	}
}

func TestAggregateBudget_NilOverflow(t *testing.T) {
	history := []canonical.Message{
		makeToolResultMsg("tc1", strings.Repeat("X", 5000)),
	}

	out := applyAggregateBudget("s1", history, nil, 1000)
	// Should pass through unchanged.
	if out[0].Content[0].ToolResult.Content != strings.Repeat("X", 5000) {
		t.Error("nil overflow should pass through")
	}
}

func TestAggregateBudget_NonToolMessages_Untouched(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Text: strings.Repeat("U", 50000)}}},
		{Role: "assistant", Content: []canonical.Content{{Text: strings.Repeat("A", 50000)}}},
	}

	out := applyAggregateBudget("s1", history, om, 1000)
	if out[0].Content[0].Text != strings.Repeat("U", 50000) {
		t.Error("user message was modified")
	}
	if out[1].Content[0].Text != strings.Repeat("A", 50000) {
		t.Error("assistant message was modified")
	}
}

func TestAggregateBudget_PreviewContent(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Content shorter than preview limit should use full content as preview.
	shortContent := strings.Repeat("Z", 200)
	// But aggregate still triggers because budget is 100.
	history := []canonical.Message{
		makeToolResultMsg("tc1", shortContent),
	}

	out := applyAggregateBudget("s1", history, om, 100)
	result := out[0].Content[0].ToolResult.Content

	// Preview should be full content (200 < 500 preview limit).
	if !strings.HasPrefix(result, shortContent) {
		t.Error("preview should contain full content when shorter than preview limit")
	}
}
