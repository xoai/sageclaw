package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestParseSSEStream_ToolCallComplete(t *testing.T) {
	// OpenAI sends tool calls as incremental deltas. ParseSSEStream must
	// accumulate them internally and emit a single complete ToolCall event.
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"lo"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"cation"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\":\"Paris\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n")

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sse), events)

	var toolCalls []*canonical.ToolCall
	var stopReason string
	for ev := range events {
		if ev.Type == "tool_call" && ev.Delta != nil && ev.Delta.ToolCall != nil {
			toolCalls = append(toolCalls, ev.Delta.ToolCall)
		}
		if ev.Type == "done" {
			stopReason = ev.StopReason
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 complete tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("expected ID=call_abc, got %q", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("expected Name=get_weather, got %q", tc.Name)
	}
	if string(tc.Input) != `{"location":"Paris"}` {
		t.Errorf("expected accumulated args, got %q", string(tc.Input))
	}
	if stopReason != "tool_use" {
		t.Errorf("expected stop_reason=tool_use, got %q", stopReason)
	}
}

func TestParseSSEStream_MultipleToolCalls(t *testing.T) {
	// Two parallel tool calls — different indices.
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":1,"id":"call_2","type":"function","function":{"name":"fetch","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"test\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"url\":\"https://x.com\"}"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n")

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sse), events)

	var toolCalls []*canonical.ToolCall
	for ev := range events {
		if ev.Type == "tool_call" && ev.Delta != nil && ev.Delta.ToolCall != nil {
			toolCalls = append(toolCalls, ev.Delta.ToolCall)
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}

	// Find by name (order may vary since map iteration is random).
	names := map[string]json.RawMessage{}
	for _, tc := range toolCalls {
		names[tc.Name] = tc.Input
	}
	if _, ok := names["search"]; !ok {
		t.Error("missing search tool call")
	}
	if _, ok := names["fetch"]; !ok {
		t.Error("missing fetch tool call")
	}
}

func TestParseSSEStream_TextContent(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello "},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"world"},"finish_reason":null}]}`,
		`data: [DONE]`,
	}, "\n")

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sse), events)

	var texts []string
	for ev := range events {
		if ev.Type == "content_delta" && ev.Delta != nil {
			texts = append(texts, ev.Delta.Text)
		}
	}

	if len(texts) != 2 || texts[0] != "Hello " || texts[1] != "world" {
		t.Errorf("unexpected text deltas: %v", texts)
	}
}

func TestParseSSEStream_UsageChunk(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`,
		`data: [DONE]`,
	}, "\n")

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sse), events)

	var usageEv *provider.StreamEvent
	for ev := range events {
		if ev.Type == "usage" {
			usageEv = &ev
		}
	}

	if usageEv == nil {
		t.Fatal("expected usage event")
	}
	if usageEv.Usage.InputTokens != 10 || usageEv.Usage.OutputTokens != 5 {
		t.Errorf("unexpected usage: %+v", usageEv.Usage)
	}
}

func TestParseSSEStream_StopReasonTextOnly(t *testing.T) {
	sse := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hi"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`data: [DONE]`,
	}, "\n")

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sse), events)

	var stopReason string
	for ev := range events {
		if ev.Type == "done" {
			stopReason = ev.StopReason
		}
	}

	if stopReason != "end_turn" {
		t.Errorf("expected stop_reason=end_turn, got %q", stopReason)
	}
}
