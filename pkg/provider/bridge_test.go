package provider

import (
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestContextBridge_NoTruncation(t *testing.T) {
	cb := NewContextBridge()

	messages := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi there!"}}},
	}

	result := cb.Transfer(messages, "claude-sonnet-4-20250514", "gpt-4o")
	if result.Truncated {
		t.Error("expected no truncation for small conversation")
	}
	if result.RetainedCount != 2 {
		t.Errorf("expected 2 retained, got %d", result.RetainedCount)
	}
}

func TestContextBridge_TruncationToSmallModel(t *testing.T) {
	cb := NewContextBridge()
	// Override to make a very small window for testing.
	cb.ModelContextWindows["tiny-model"] = 100 // ~100 tokens

	// Create a large conversation.
	messages := make([]canonical.Message, 0, 20)
	for i := 0; i < 20; i++ {
		messages = append(messages, canonical.Message{
			Role:    "user",
			Content: []canonical.Content{{Type: "text", Text: strings.Repeat("word ", 50)}}, // ~50 tokens each
		})
		messages = append(messages, canonical.Message{
			Role:    "assistant",
			Content: []canonical.Content{{Type: "text", Text: strings.Repeat("reply ", 50)}},
		})
	}

	result := cb.Transfer(messages, "claude-sonnet-4-20250514", "tiny-model")
	if !result.Truncated {
		t.Error("expected truncation for large conversation → tiny model")
	}
	if result.RetainedCount >= result.OriginalCount {
		t.Errorf("expected fewer messages, got %d/%d", result.RetainedCount, result.OriginalCount)
	}

	// First message should be the original first message.
	if result.Messages[0].Role != "user" {
		t.Error("first message should be user")
	}
}

func TestContextBridge_ToolPairRepair(t *testing.T) {
	cb := NewContextBridge()
	cb.ModelContextWindows["tiny-model"] = 200

	messages := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Search for Go tutorials"}}},
		// Tool call.
		{Role: "assistant", Content: []canonical.Content{{
			Type:     "tool_use",
			ToolCall: &canonical.ToolCall{ID: "call_123", Name: "web_search", Input: []byte(`{"q":"Go tutorials"}`)},
		}}},
		// Tool result.
		{Role: "tool", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "call_123", Content: "Found 5 results..."},
		}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Here are the results..."}}},
		// More messages to force truncation.
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("more context ", 100)}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: strings.Repeat("more response ", 100)}}},
	}

	result := cb.Transfer(messages, "claude-sonnet-4-20250514", "tiny-model")

	// Check no orphaned tool results.
	callIDs := map[string]bool{}
	for _, msg := range result.Messages {
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				callIDs[c.ToolCall.ID] = true
			}
		}
	}
	for _, msg := range result.Messages {
		for _, c := range msg.Content {
			if c.ToolResult != nil && !callIDs[c.ToolResult.ToolCallID] {
				t.Errorf("orphaned tool result for call %s", c.ToolResult.ToolCallID)
			}
		}
	}
}

func TestContextBridge_EmptyMessages(t *testing.T) {
	cb := NewContextBridge()
	result := cb.Transfer(nil, "gpt-4o", "claude-sonnet-4-20250514")
	if result.Truncated {
		t.Error("empty messages should not be truncated")
	}
	if result.RetainedCount != 0 {
		t.Errorf("expected 0 retained, got %d", result.RetainedCount)
	}
}

func TestContextBridge_GapMarker(t *testing.T) {
	cb := NewContextBridge()
	cb.ModelContextWindows["tiny-model"] = 150

	messages := make([]canonical.Message, 0, 10)
	for i := 0; i < 10; i++ {
		messages = append(messages, canonical.Message{
			Role:    "user",
			Content: []canonical.Content{{Type: "text", Text: strings.Repeat("test ", 30)}},
		})
	}

	result := cb.Transfer(messages, "gpt-4o", "tiny-model")
	if !result.Truncated {
		t.Error("expected truncation")
	}

	// Check for gap marker.
	hasGap := false
	for _, msg := range result.Messages {
		for _, c := range msg.Content {
			if strings.Contains(c.Text, "Context bridge") {
				hasGap = true
			}
		}
	}
	if !hasGap {
		t.Error("expected gap marker message when messages are truncated")
	}
}

func TestContextBridge_UnknownModel(t *testing.T) {
	cb := NewContextBridge()
	// Unknown model should default to 8192 context window.
	window := cb.getContextWindow("unknown-model-xyz")
	if window != 8192 {
		t.Errorf("expected 8192 default, got %d", window)
	}
}
