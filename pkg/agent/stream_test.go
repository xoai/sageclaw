package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestConsumeStream_CompletePath(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Provider sends a complete tool call (new path).
	events <- provider.StreamEvent{
		Type: "tool_call",
		Delta: &canonical.Content{
			Type: "tool_call",
			ToolCall: &canonical.ToolCall{
				ID:    "call_1",
				Name:  "get_weather",
				Input: json.RawMessage(`{"location":"Paris"}`),
				Meta:  map[string]string{"thought_signature": "sig123"},
			},
		},
	}
	events <- provider.StreamEvent{Type: "done", StopReason: "tool_use"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if len(result.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Message.Content))
	}

	tc := result.Message.Content[0]
	if tc.ToolCall == nil {
		t.Fatal("expected ToolCall, got nil")
	}
	if tc.ToolCall.ID != "call_1" {
		t.Errorf("expected ID=call_1, got %q", tc.ToolCall.ID)
	}
	if tc.ToolCall.Name != "get_weather" {
		t.Errorf("expected Name=get_weather, got %q", tc.ToolCall.Name)
	}
	if string(tc.ToolCall.Input) != `{"location":"Paris"}` {
		t.Errorf("unexpected Input: %s", tc.ToolCall.Input)
	}
	if tc.ToolCall.Meta["thought_signature"] != "sig123" {
		t.Errorf("unexpected Meta: %v", tc.ToolCall.Meta)
	}
}

func TestConsumeStream_DeltaPath(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Provider sends tool call as deltas (old path).
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:       "tool_call",
			ToolCallID: "call_2",
			ToolName:   "search",
		},
	}
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:      "tool_call",
			ToolInput: `{"query":`,
		},
	}
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:      "tool_call",
			ToolInput: `"hello"}`,
		},
	}
	events <- provider.StreamEvent{Type: "done", StopReason: "tool_use"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if len(result.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Message.Content))
	}

	tc := result.Message.Content[0]
	if tc.ToolCall == nil {
		t.Fatal("expected ToolCall, got nil")
	}
	if tc.ToolCall.ID != "call_2" {
		t.Errorf("expected ID=call_2, got %q", tc.ToolCall.ID)
	}
	if string(tc.ToolCall.Input) != `{"query":"hello"}` {
		t.Errorf("unexpected Input: %s", tc.ToolCall.Input)
	}
}

func TestConsumeStream_TextAndToolCall(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	events <- provider.StreamEvent{
		Type:  "content_delta",
		Delta: &canonical.Content{Type: "text", Text: "Let me check "},
	}
	events <- provider.StreamEvent{
		Type:  "content_delta",
		Delta: &canonical.Content{Type: "text", Text: "the weather."},
	}
	events <- provider.StreamEvent{
		Type: "tool_call",
		Delta: &canonical.Content{
			Type: "tool_call",
			ToolCall: &canonical.ToolCall{
				ID:    "call_3",
				Name:  "weather",
				Input: json.RawMessage(`{}`),
			},
		},
	}
	events <- provider.StreamEvent{Type: "done", StopReason: "tool_use"}
	close(events)

	var chunks []string
	result := consumeStream(context.Background(), events, "sess1", 1, func(ev Event) {
		if ev.Type == EventChunk {
			chunks = append(chunks, ev.Text)
		}
	}, nil)

	if len(result.Message.Content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(result.Message.Content))
	}
	if result.Message.Content[0].Text != "Let me check the weather." {
		t.Errorf("unexpected text: %q", result.Message.Content[0].Text)
	}
	if result.Message.Content[1].ToolCall == nil {
		t.Fatal("expected tool call in second block")
	}
	if len(chunks) != 2 {
		t.Errorf("expected 2 chunks emitted, got %d", len(chunks))
	}
}

func TestConsumeStream_MetaPreferredOverToolMeta(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Delta with Meta field (new) should be preferred over ToolMeta (deprecated).
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:       "tool_call",
			ToolCallID: "call_m",
			ToolName:   "fn",
		},
	}
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:     "tool_call",
			Meta:     map[string]string{"thought_signature": "new_sig"},
			ToolMeta: map[string]string{"thought_signature": "old_sig"},
		},
	}
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{Type: "tool_call", ToolInput: `{}`},
	}
	events <- provider.StreamEvent{Type: "done"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if len(result.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Message.Content))
	}
	tc := result.Message.Content[0].ToolCall
	if tc == nil {
		t.Fatal("expected ToolCall")
	}
	if tc.Meta["thought_signature"] != "new_sig" {
		t.Errorf("expected Meta to prefer new_sig, got %v", tc.Meta)
	}
}

func TestConsumeStream_ToolMetaFallback(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Delta with only ToolMeta (deprecated) should still work.
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{
			Type:       "tool_call",
			ToolCallID: "call_f",
			ToolName:   "fn",
			ToolMeta:   map[string]string{"thought_signature": "old_sig"},
		},
	}
	events <- provider.StreamEvent{
		Type:  "tool_call",
		Index: 0,
		Delta: &canonical.Content{Type: "tool_call", ToolInput: `{}`},
	}
	events <- provider.StreamEvent{Type: "done"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)
	tc := result.Message.Content[0].ToolCall
	if tc.Meta["thought_signature"] != "old_sig" {
		t.Errorf("expected ToolMeta fallback, got %v", tc.Meta)
	}
}

func TestConsumeStream_ThinkingAccumulation(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Thinking deltas followed by text and a tool call.
	events <- provider.StreamEvent{
		Type: "content_delta",
		Delta: &canonical.Content{Type: "thinking", Thinking: "Let me "},
	}
	events <- provider.StreamEvent{
		Type: "content_delta",
		Delta: &canonical.Content{Type: "thinking", Thinking: "reason..."},
	}
	events <- provider.StreamEvent{
		Type: "content_delta",
		Delta: &canonical.Content{
			Type: "thinking",
			Meta: map[string]string{"thinking_signature": "sig_stream"},
		},
	}
	events <- provider.StreamEvent{
		Type:  "content_delta",
		Delta: &canonical.Content{Type: "text", Text: "Answer."},
	}
	events <- provider.StreamEvent{Type: "done", StopReason: "end_turn"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if len(result.Message.Content) != 2 {
		t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(result.Message.Content))
	}

	// First block: thinking with signature.
	thinking := result.Message.Content[0]
	if thinking.Type != "thinking" {
		t.Errorf("expected thinking block, got %s", thinking.Type)
	}
	if thinking.Thinking != "Let me reason..." {
		t.Errorf("expected accumulated thinking text, got %q", thinking.Thinking)
	}
	if thinking.Meta["thinking_signature"] != "sig_stream" {
		t.Errorf("expected thinking_signature, got %v", thinking.Meta)
	}

	// Second block: text.
	if result.Message.Content[1].Text != "Answer." {
		t.Errorf("expected text, got %q", result.Message.Content[1].Text)
	}
}

func TestConsumeStream_StopReasonFromUsageEvent(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	// Anthropic sends stop_reason on "usage" event (message_delta), not "done".
	events <- provider.StreamEvent{
		Type: "tool_call",
		Delta: &canonical.Content{
			ToolCall: &canonical.ToolCall{
				ID: "call_1", Name: "search", Input: json.RawMessage(`{}`),
			},
		},
	}
	events <- provider.StreamEvent{
		Type:       "usage",
		StopReason: "tool_use",
		Usage:      &canonical.Usage{OutputTokens: 50},
	}
	events <- provider.StreamEvent{Type: "done"} // No StopReason here.
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if result.StopReason != "tool_use" {
		t.Errorf("expected StopReason=tool_use, got %q", result.StopReason)
	}
}

func TestConsumeStream_CompleteToolCallType(t *testing.T) {
	events := make(chan provider.StreamEvent, 10)

	events <- provider.StreamEvent{
		Type: "tool_call",
		Delta: &canonical.Content{
			ToolCall: &canonical.ToolCall{
				ID: "call_1", Name: "search", Input: json.RawMessage(`{}`),
			},
		},
	}
	events <- provider.StreamEvent{Type: "done", StopReason: "tool_use"}
	close(events)

	result := consumeStream(context.Background(), events, "sess1", 1, func(Event) {}, nil)

	if len(result.Message.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Message.Content))
	}
	// Must be "tool_call" (canonical type), not "tool_use" (Anthropic API type).
	if result.Message.Content[0].Type != "tool_call" {
		t.Errorf("expected Type=tool_call, got %q", result.Message.Content[0].Type)
	}
}
