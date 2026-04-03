package agent

import (
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

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
