package openai

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestToOpenAIRequest_Basic(t *testing.T) {
	req := &canonical.Request{
		Model:     "gpt-4o",
		System:    "You are a helpful assistant.",
		MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		},
	}

	data, err := ToOpenAIRequest(req)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["model"] != "gpt-4o" {
		t.Fatalf("wrong model: %v", raw["model"])
	}

	msgs := raw["messages"].([]any)
	// Should have system + user = 2 messages.
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}

	// First message should be system.
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" {
		t.Fatalf("expected system role, got %v", sys["role"])
	}
	if sys["content"] != "You are a helpful assistant." {
		t.Fatalf("wrong system content: %v", sys["content"])
	}

	// Second message should be user.
	usr := msgs[1].(map[string]any)
	if usr["role"] != "user" {
		t.Fatalf("expected user role, got %v", usr["role"])
	}
}

func TestToOpenAIRequest_WithTools(t *testing.T) {
	req := &canonical.Request{
		Model: "gpt-4o", MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read"}}},
		},
		Tools: []canonical.ToolDef{{
			Name: "read_file", Description: "Read a file",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}},
	}

	data, _ := ToOpenAIRequest(req)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	tools := raw["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	tool := tools[0].(map[string]any)
	if tool["type"] != "function" {
		t.Fatalf("expected function type, got %v", tool["type"])
	}
	fn := tool["function"].(map[string]any)
	if fn["name"] != "read_file" {
		t.Fatalf("wrong tool name: %v", fn["name"])
	}
}

func TestToOpenAIRequest_WithToolCall(t *testing.T) {
	req := &canonical.Request{
		Model: "gpt-4o", MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read file"}}},
			{Role: "assistant", Content: []canonical.Content{
				{Type: "tool_call", ToolCall: &canonical.ToolCall{
					ID: "call_1", Name: "read_file", Input: json.RawMessage(`{"path":"test.txt"}`),
				}},
			}},
			{Role: "user", Content: []canonical.Content{
				{Type: "tool_result", ToolResult: &canonical.ToolResult{
					ToolCallID: "call_1", Content: "file contents",
				}},
			}},
		},
	}

	data, _ := ToOpenAIRequest(req)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	msgs := raw["messages"].([]any)
	// user + assistant + tool = 3
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Assistant should have tool_calls.
	assistant := msgs[1].(map[string]any)
	tcs := assistant["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("expected 1 tool_call, got %d", len(tcs))
	}
	tc := tcs[0].(map[string]any)
	if tc["type"] != "function" {
		t.Fatalf("expected function type, got %v", tc["type"])
	}

	// Tool result should be role=tool.
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" {
		t.Fatalf("expected tool role, got %v", toolMsg["role"])
	}
}

func TestToOpenAIRequest_OSeries(t *testing.T) {
	req := &canonical.Request{
		Model:     "o3-mini",
		MaxTokens: 4096,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "think"}}},
		},
		Options: map[string]any{"thinking_level": "high"},
	}

	data, err := ToOpenAIRequest(req)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	// o-series uses max_completion_tokens, not max_tokens.
	if _, ok := raw["max_tokens"]; ok {
		t.Error("o-series should not have max_tokens")
	}
	if raw["max_completion_tokens"].(float64) != 4096 {
		t.Errorf("expected max_completion_tokens=4096, got %v", raw["max_completion_tokens"])
	}

	// reasoning_effort from thinking_level.
	if raw["reasoning_effort"] != "high" {
		t.Errorf("expected reasoning_effort=high, got %v", raw["reasoning_effort"])
	}

	// No temperature for o-series.
	if _, ok := raw["temperature"]; ok {
		t.Error("o-series should not have temperature")
	}
}

func TestToOpenAIRequest_NonOSeries_MaxTokens(t *testing.T) {
	req := &canonical.Request{
		Model:     "gpt-4o",
		MaxTokens: 2048,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		},
	}

	data, _ := ToOpenAIRequest(req)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	// Non o-series uses max_tokens.
	if raw["max_tokens"].(float64) != 2048 {
		t.Errorf("expected max_tokens=2048, got %v", raw["max_tokens"])
	}
	if _, ok := raw["max_completion_tokens"]; ok {
		t.Error("non o-series should not have max_completion_tokens")
	}
}

func TestIsOSeries(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"o1-preview", true},
		{"o3-mini", true},
		{"o4-mini", true},
		{"gpt-4o", false},
		{"gpt-4o-mini", false},
		{"claude-sonnet-4-20250514", false},
	}
	for _, tt := range tests {
		if got := isOSeries(tt.model); got != tt.want {
			t.Errorf("isOSeries(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestFromOpenAIResponse_Text(t *testing.T) {
	apiResp := `{
		"id": "chatcmpl-123",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello! How can I help?"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
	}`

	resp, err := FromOpenAIResponse([]byte(apiResp))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if resp.ID != "chatcmpl-123" {
		t.Fatalf("wrong ID: %s", resp.ID)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("expected end_turn, got %s", resp.StopReason)
	}
	if resp.Messages[0].Content[0].Text != "Hello! How can I help?" {
		t.Fatalf("wrong text: %s", resp.Messages[0].Content[0].Text)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 8 {
		t.Fatalf("wrong usage: %+v", resp.Usage)
	}
}

func TestFromOpenAIResponse_ToolCall(t *testing.T) {
	apiResp := `{
		"id": "chatcmpl-456",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{
					"id": "call_789",
					"type": "function",
					"function": {"name": "read_file", "arguments": "{\"path\":\"test.txt\"}"}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 20, "completion_tokens": 15}
	}`

	resp, err := FromOpenAIResponse([]byte(apiResp))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Fatalf("expected tool_use, got %s", resp.StopReason)
	}

	content := resp.Messages[0].Content
	if len(content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(content))
	}
	if content[0].ToolCall.Name != "read_file" {
		t.Fatalf("wrong tool name: %s", content[0].ToolCall.Name)
	}
}

func TestParseSSEStream_TextResponse(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","content":""},"index":0}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Hello"},"index":0}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":" world"},"index":0}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{},"index":0,"finish_reason":"stop"}]}

data: [DONE]

`

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sseData), events)

	var collected []provider.StreamEvent
	for evt := range events {
		collected = append(collected, evt)
	}

	textDeltas := 0
	hasDone := false
	for _, evt := range collected {
		if evt.Type == "content_delta" {
			textDeltas++
		}
		if evt.Type == "done" {
			hasDone = true
		}
	}

	if textDeltas != 2 {
		t.Fatalf("expected 2 text deltas, got %d", textDeltas)
	}
	if !hasDone {
		t.Fatal("expected done event")
	}
}

func TestParseSSEStream_ToolCall(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":""}}]},"index":0}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"function":{"arguments":"{\"path\":"}}]},"index":0}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{},"index":0,"finish_reason":"tool_calls"}]}

data: [DONE]

`

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sseData), events)

	var collected []provider.StreamEvent
	for evt := range events {
		collected = append(collected, evt)
	}

	hasToolCall := false
	for _, evt := range collected {
		if evt.Type == "tool_call" {
			hasToolCall = true
		}
	}
	if !hasToolCall {
		t.Fatal("expected tool_call event")
	}
}

func TestMapFinishReason(t *testing.T) {
	tests := []struct{ in, out string }{
		{"stop", "end_turn"},
		{"tool_calls", "tool_use"},
		{"length", "max_tokens"},
		{"unknown", "unknown"},
	}
	for _, tt := range tests {
		if got := mapFinishReason(tt.in); got != tt.out {
			t.Errorf("mapFinishReason(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}
