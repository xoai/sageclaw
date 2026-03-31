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

func TestToAPIRequest_ThinkingBudget(t *testing.T) {
	tests := []struct {
		level       string
		wantBudget  int
		wantMinMax  int // max_tokens must be at least budget + 8192
	}{
		{"low", 4096, 4096 + 8192},
		{"medium", 10000, 10000 + 8192},
		{"high", 32000, 32000 + 8192},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			req := &canonical.Request{
				Model:     "claude-sonnet-4-20250514",
				MaxTokens: 1024, // Too small — should be adjusted.
				Messages: []canonical.Message{
					{Role: "user", Content: []canonical.Content{{Type: "text", Text: "think"}}},
				},
				Options: map[string]any{"thinking_level": tt.level},
			}

			data, err := ToAPIRequest(req, false)
			if err != nil {
				t.Fatalf("translation failed: %v", err)
			}

			var raw map[string]any
			json.Unmarshal(data, &raw)

			// Thinking should be present.
			thinking := raw["thinking"].(map[string]any)
			if thinking["type"] != "enabled" {
				t.Errorf("expected type=enabled, got %v", thinking["type"])
			}
			if int(thinking["budget_tokens"].(float64)) != tt.wantBudget {
				t.Errorf("expected budget=%d, got %v", tt.wantBudget, thinking["budget_tokens"])
			}

			// max_tokens should be adjusted up.
			maxTokens := int(raw["max_tokens"].(float64))
			if maxTokens < tt.wantMinMax {
				t.Errorf("max_tokens=%d, want at least %d", maxTokens, tt.wantMinMax)
			}

			// Temperature must NOT be present when thinking is enabled.
			if _, ok := raw["temperature"]; ok {
				t.Error("temperature must be omitted when thinking is enabled")
			}
		})
	}
}

func TestToAPIRequest_ThinkingDisabled_TemperaturePreserved(t *testing.T) {
	req := &canonical.Request{
		Model:       "claude-sonnet-4-20250514",
		MaxTokens:   1024,
		Temperature: 0.7,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
	}

	data, err := ToAPIRequest(req, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	if raw["thinking"] != nil {
		t.Error("thinking should not be present without thinking_level")
	}
	temp, ok := raw["temperature"].(float64)
	if !ok || temp != 0.7 {
		t.Errorf("expected temperature=0.7, got %v", raw["temperature"])
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

func TestFromAPIResponse_Thinking(t *testing.T) {
	apiResp := `{
		"id": "msg_think",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "thinking", "thinking": "Let me reason about this...", "signature": "sig_abc123"},
			{"type": "text", "text": "Here is my answer."}
		],
		"stop_reason": "end_turn",
		"usage": {"input_tokens": 10, "output_tokens": 50}
	}`

	resp, err := FromAPIResponse([]byte(apiResp))
	if err != nil {
		t.Fatalf("parsing failed: %v", err)
	}

	content := resp.Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks, got %d", len(content))
	}

	// First block: thinking with signature in Meta.
	if content[0].Type != "thinking" {
		t.Errorf("expected thinking block, got %s", content[0].Type)
	}
	if content[0].Thinking != "Let me reason about this..." {
		t.Errorf("wrong thinking text: %s", content[0].Thinking)
	}
	if content[0].Meta["thinking_signature"] != "sig_abc123" {
		t.Errorf("expected thinking_signature=sig_abc123, got %v", content[0].Meta)
	}

	// Second block: text.
	if content[1].Type != "text" || content[1].Text != "Here is my answer." {
		t.Errorf("unexpected text block: %+v", content[1])
	}
}

func TestToAPIRequest_ThinkingSignatureRoundTrip(t *testing.T) {
	// Simulate a multi-turn conversation with thinking signatures.
	req := &canonical.Request{
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 8192,
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Think carefully"}}},
			{Role: "assistant", Content: []canonical.Content{
				{Type: "thinking", Thinking: "Let me reason...", Meta: map[string]string{"thinking_signature": "sig_round"}},
				{Type: "text", Text: "My answer."},
			}},
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Follow up"}}},
		},
		Options: map[string]any{"thinking_level": "medium"},
	}

	data, err := ToAPIRequest(req, false)
	if err != nil {
		t.Fatalf("translation failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	msgs := raw["messages"].([]any)
	// Message[1] should be assistant with thinking + text blocks.
	assistantMsg := msgs[1].(map[string]any)
	blocks := assistantMsg["content"].([]any)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks in assistant message, got %d", len(blocks))
	}

	thinkingBlock := blocks[0].(map[string]any)
	if thinkingBlock["type"] != "thinking" {
		t.Errorf("expected thinking block, got %v", thinkingBlock["type"])
	}
	if thinkingBlock["signature"] != "sig_round" {
		t.Errorf("expected signature=sig_round, got %v", thinkingBlock["signature"])
	}
	if thinkingBlock["thinking"] != "Let me reason..." {
		t.Errorf("expected thinking text, got %v", thinkingBlock["thinking"])
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

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"test.txt\"}"}}

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

	// Tool call should be emitted as a complete ToolCall (not deltas).
	var completeTC *canonical.ToolCall
	for _, evt := range collected {
		if evt.Type == "tool_call" && evt.Delta != nil && evt.Delta.ToolCall != nil {
			completeTC = evt.Delta.ToolCall
		}
	}
	if completeTC == nil {
		t.Fatal("expected complete tool_call event")
	}
	if completeTC.ID != "call_1" {
		t.Errorf("expected ID=call_1, got %q", completeTC.ID)
	}
	if completeTC.Name != "read_file" {
		t.Errorf("expected Name=read_file, got %q", completeTC.Name)
	}
	if string(completeTC.Input) != `{"path":"test.txt"}` {
		t.Errorf("expected accumulated input, got %q", string(completeTC.Input))
	}
}

func TestParseSSEStream_ThinkingAndSignature(t *testing.T) {
	sseData := `event: message_start
data: {"type":"message_start","message":{"id":"msg_1","role":"assistant"}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Let me think..."}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig_stream_123"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Answer."}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_stop
data: {"type":"message_stop"}

`

	events := make(chan provider.StreamEvent, 32)
	go ParseSSEStream(strings.NewReader(sseData), events)

	var collected []provider.StreamEvent
	for evt := range events {
		collected = append(collected, evt)
	}

	hasThinking := false
	hasSignature := false
	hasText := false
	for _, evt := range collected {
		if evt.Type == "content_delta" && evt.Delta != nil {
			if evt.Delta.Type == "thinking" && evt.Delta.Thinking != "" {
				hasThinking = true
			}
			if evt.Delta.Type == "thinking" && evt.Delta.Meta != nil {
				if evt.Delta.Meta["thinking_signature"] == "sig_stream_123" {
					hasSignature = true
				}
			}
			if evt.Delta.Type == "text" && evt.Delta.Text == "Answer." {
				hasText = true
			}
		}
	}

	if !hasThinking {
		t.Error("expected thinking_delta event")
	}
	if !hasSignature {
		t.Error("expected signature_delta event")
	}
	if !hasText {
		t.Error("expected text_delta event")
	}
}

func TestToAPIRequest_CacheBreakpoints_LatestTurn(t *testing.T) {
	// Build a conversation with 6 messages (user/assistant alternating).
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi there"}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Question 1"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Answer 1"}}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Question 2"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Answer 2"}}},
	}

	req := &canonical.Request{
		Model: "claude-sonnet-4-20250514", MaxTokens: 4096,
		System:   "You are helpful.",
		Messages: msgs,
		Tools: []canonical.ToolDef{{
			Name: "read", Description: "Read",
			InputSchema: json.RawMessage(`{"type":"object"}`),
		}},
	}

	data, err := ToAPIRequest(req, true) // cache enabled
	if err != nil {
		t.Fatalf("failed: %v", err)
	}

	var raw map[string]any
	json.Unmarshal(data, &raw)

	apiMsgs := raw["messages"].([]any)

	// Breakpoints should be on the LAST user message (index 4 in conversation,
	// which maps to message index 4 in apiMsgs) and the LAST assistant before
	// that (index 3). NOT on 25%/50% positions.
	//
	// Find which messages have cache_control.
	var cachedIndices []int
	for i, m := range apiMsgs {
		msg := m.(map[string]any)
		content, ok := msg["content"].([]any)
		if !ok {
			continue // content might be a string for simple messages
		}
		for _, block := range content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := b["cache_control"]; ok {
				cachedIndices = append(cachedIndices, i)
			}
		}
	}

	// Should have exactly 2 cached message positions.
	if len(cachedIndices) != 2 {
		t.Fatalf("expected 2 cache breakpoints on messages, got %d at indices %v", len(cachedIndices), cachedIndices)
	}

	// The cached positions should be at the end (latest turns), not at 25%/50%.
	// With 6 messages, 25% = index 1, 50% = index 3.
	// Latest-turn: last user = index 4, last assistant before = index 3.
	lastIdx := cachedIndices[len(cachedIndices)-1]
	if lastIdx < 3 {
		t.Errorf("expected cache breakpoints near end of conversation, got indices %v", cachedIndices)
	}
}

func TestToAPIRequest_CacheBreakpoints_TooFewMessages(t *testing.T) {
	// With < 4 messages, no turn breakpoints should be set.
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi"}}},
	}

	req := &canonical.Request{
		Model: "claude-sonnet-4", MaxTokens: 4096,
		System:   "Test",
		Messages: msgs,
	}

	data, _ := ToAPIRequest(req, true)
	var raw map[string]any
	json.Unmarshal(data, &raw)

	apiMsgs := raw["messages"].([]any)
	for _, m := range apiMsgs {
		msg := m.(map[string]any)
		content, ok := msg["content"].([]any)
		if !ok {
			continue
		}
		for _, block := range content {
			b, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if _, ok := b["cache_control"]; ok {
				t.Error("expected no cache breakpoints on messages with < 4 messages")
			}
		}
	}
}
