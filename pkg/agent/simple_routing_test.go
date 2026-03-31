package agent

import (
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestIsSimpleMessage_TrivialOk(t *testing.T) {
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi! How can I help?"}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "ok"}}},
	}
	// "ok" is ~1 token, iteration > 0, but prev assistant ends with "?" → false
	if isSimpleMessage(history, 1) {
		t.Error("expected false: previous assistant asked a question")
	}
}

func TestIsSimpleMessage_ShortAfterStatement(t *testing.T) {
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Here is the result."}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "thanks"}}},
	}
	// "thanks" is ~2 tokens, prev assistant ends with "." not "?" → true
	if !isSimpleMessage(history, 1) {
		t.Error("expected true for trivial 'thanks' after a statement")
	}
}

func TestIsSimpleMessage_FirstIteration(t *testing.T) {
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "ok"}}},
	}
	if isSimpleMessage(history, 0) {
		t.Error("expected false on first iteration")
	}
}

func TestIsSimpleMessage_ToolResult(t *testing.T) {
	history := []canonical.Message{
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Done."}}},
		{Role: "user", Content: []canonical.Content{
			{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "1", Content: "ok"}},
		}},
	}
	if isSimpleMessage(history, 1) {
		t.Error("expected false when last user message has tool_result")
	}
}

func TestIsSimpleMessage_LongMessage(t *testing.T) {
	history := []canonical.Message{
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Done."}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Can you please explain the architecture of this system in detail, including all the components and how they interact with each other?"}}},
	}
	if isSimpleMessage(history, 1) {
		t.Error("expected false for long message")
	}
}

func TestIsSimpleMessage_Emoji(t *testing.T) {
	history := []canonical.Message{
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "All done!"}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "👍"}}},
	}
	if !isSimpleMessage(history, 2) {
		t.Error("expected true for emoji response after statement")
	}
}

func TestIsSimpleMessage_TooFewMessages(t *testing.T) {
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
	}
	if isSimpleMessage(history, 1) {
		t.Error("expected false with only 1 message in history")
	}
}
