package anthropic

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestToAPIRequest_Basic(t *testing.T) {
	req := &canonical.Request{
		Model:     "claude-sonnet-4-20250514",
		System:    "You are a helpful assistant.",
		MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		},
	}

	data, err := ToAPIRequest(req, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("wrong model: %v", raw["model"])
	}
	if raw["max_tokens"].(float64) != 1024 {
		t.Fatalf("wrong max_tokens: %v", raw["max_tokens"])
	}

	// Messages should be present.
	msgs := raw["messages"].([]any)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	// Single text block should be string shorthand.
	msg := msgs[0].(map[string]any)
	if msg["role"] != "user" {
		t.Fatalf("wrong role: %v", msg["role"])
	}
	if msg["content"] != "Hello" {
		t.Fatalf("expected string content 'Hello', got: %v", msg["content"])
	}
}

func TestToAPIRequest_WithTools(t *testing.T) {
	req := &canonical.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read file"}}},
		},
		Tools: []canonical.ToolDef{
			{
				Name:        "read_file",
				Description: "Read a file",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
			},
		},
	}

	data, err := ToAPIRequest(req, true)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	tools := raw["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	tool := tools[0].(map[string]any)
	if tool["name"] != "read_file" {
		t.Fatalf("wrong tool name: %v", tool["name"])
	}

	// With caching enabled, last tool should have cache_control.
	cc := tool["cache_control"]
	if cc == nil {
		t.Fatal("expected cache_control on last tool")
	}
}

func TestToAPIRequest_WithToolCall(t *testing.T) {
	req := &canonical.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1024,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read file"}}},
			{Role: "assistant", Content: []canonical.Content{
				{Type: "tool_call", ToolCall: &canonical.ToolCall{
					ID:    "call_123",
					Name:  "read_file",
					Input: json.RawMessage(`{"path":"test.txt"}`),
				}},
			}},
			{Role: "user", Content: []canonical.Content{
				{Type: "tool_result", ToolResult: &canonical.ToolResult{
					ToolCallID: "call_123",
					Content:    "file contents here",
				}},
			}},
		},
	}

	data, err := ToAPIRequest(req, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	msgs := raw["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Assistant message should have tool_use block.
	assistantMsg := msgs[1].(map[string]any)
	blocks := assistantMsg["content"].([]any)
	block := blocks[0].(map[string]any)
	if block["type"] != "tool_use" {
		t.Fatalf("expected tool_use, got: %v", block["type"])
	}
}

func TestFromAPIResponse_Text(t *testing.T) {
	apiResp := `{
		"id": "msg_123",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "Hello! How can I help?"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 8}
	}`

	resp, err := FromAPIResponse([]byte(apiResp))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if resp.ID != "msg_123" {
		t.Fatalf("wrong ID: %s", resp.ID)
	}
	if resp.StopReason != "end_turn" {
		t.Fatalf("wrong stop_reason: %s", resp.StopReason)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(resp.Messages))
	}
	if resp.Messages[0].Content[0].Text != "Hello! How can I help?" {
		t.Fatalf("wrong text: %s", resp.Messages[0].Content[0].Text)
	}
	if resp.Usage.InputTokens != 10 || resp.Usage.OutputTokens != 8 {
		t.Fatalf("wrong usage: %+v", resp.Usage)
	}
}

func TestFromAPIResponse_ToolUse(t *testing.T) {
	apiResp := `{
		"id": "msg_456",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Let me read that file."},
			{"type": "tool_use", "id": "call_789", "name": "read_file", "input": {"path": "test.txt"}}
		],
		"stop_reason": "tool_use",
		"usage": {"input_tokens": 20, "output_tokens": 15}
	}`

	resp, err := FromAPIResponse([]byte(apiResp))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	if resp.StopReason != "tool_use" {
		t.Fatalf("wrong stop_reason: %s", resp.StopReason)
	}

	content := resp.Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}
	if content[0].Type != "text" {
		t.Fatalf("expected text block, got %s", content[0].Type)
	}
	if content[1].Type != "tool_call" {
		t.Fatalf("expected tool_call block, got %s", content[1].Type)
	}
	if content[1].ToolCall.Name != "read_file" {
		t.Fatalf("wrong tool name: %s", content[1].ToolCall.Name)
	}
}

func TestParseSSEStream_TextResponse(t *testing.T) {
	sseData := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hello"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" world"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}

`

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sseData), events)

	var collected []provider.StreamEvent
	for evt := range events {
		collected = append(collected, evt)
	}

	// Should have: 2 content deltas, 1 usage, 1 done.
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
		t.Fatalf("expected 2 text deltas, got %d (total events: %d)", textDeltas, len(collected))
	}
	if !hasDone {
		t.Fatal("expected done event")
	}
}

func TestParseSSEStream_ToolUse(t *testing.T) {
	sseData := `event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"read_file"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_stop
data: {"type":"message_stop"}

`

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sseData), events)

	var collected []provider.StreamEvent
	for evt := range events {
		collected = append(collected, evt)
	}

	hasToolCall := false
	for _, evt := range collected {
		if evt.Type == "tool_call" && evt.Delta != nil && evt.Delta.ToolCall != nil {
			if evt.Delta.ToolCall.Name == "read_file" {
				hasToolCall = true
			}
		}
	}
	if !hasToolCall {
		t.Fatal("expected tool_call event for read_file")
	}
}
