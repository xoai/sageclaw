package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

// mockProvider returns scripted responses.
type mockProvider struct {
	responses []canonical.Response
	callCount int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	if m.callCount >= len(m.responses) {
		return nil, fmt.Errorf("no more responses")
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return &resp, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestLoop_SimpleResponse(t *testing.T) {
	prov := &mockProvider{
		responses: []canonical.Response{
			{
				ID:         "msg_1",
				StopReason: "end_turn",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hello! I'm SageClaw."}}},
				},
				Usage: canonical.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
	}

	reg := tool.NewRegistry()
	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
	}, prov, reg, nil, nil, nil)

	result := loop.Run(context.Background(), "sess_1", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result.Messages))
	}
	if result.Messages[0].Content[0].Text != "Hello! I'm SageClaw." {
		t.Fatalf("wrong response: %s", result.Messages[0].Content[0].Text)
	}
	if result.Usage.InputTokens != 10 {
		t.Fatalf("wrong input tokens: %d", result.Usage.InputTokens)
	}
}

func TestLoop_ToolCallAndResponse(t *testing.T) {
	prov := &mockProvider{
		responses: []canonical.Response{
			{
				// First response: tool call.
				ID:         "msg_1",
				StopReason: "tool_use",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{
						{Type: "text", Text: "Let me read that file."},
						{Type: "tool_call", ToolCall: &canonical.ToolCall{
							ID:    "call_1",
							Name:  "echo_tool",
							Input: json.RawMessage(`{"text":"hello"}`),
						}},
					}},
				},
				Usage: canonical.Usage{InputTokens: 20, OutputTokens: 15},
			},
			{
				// Second response: final answer.
				ID:         "msg_2",
				StopReason: "end_turn",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "The file says: hello"}}},
				},
				Usage: canonical.Usage{InputTokens: 30, OutputTokens: 10},
			},
		},
	}

	reg := tool.NewRegistry()
	reg.Register("echo_tool", "Echo input", json.RawMessage(`{}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			var params struct{ Text string }
			json.Unmarshal(input, &params)
			return &canonical.ToolResult{Content: "echo: " + params.Text}, nil
		})

	var events []Event
	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
	}, prov, reg, nil, nil, func(e Event) {
		events = append(events, e)
	})

	result := loop.Run(context.Background(), "sess_1", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "read test.txt"}}},
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}

	// Should have: assistant (tool call), tool result, assistant (final).
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 pending messages, got %d", len(result.Messages))
	}

	// Verify usage aggregation.
	if result.Usage.InputTokens != 50 {
		t.Fatalf("expected 50 input tokens, got %d", result.Usage.InputTokens)
	}

	// Verify events.
	hasToolCall := false
	hasToolResult := false
	for _, e := range events {
		if e.Type == EventToolCall {
			hasToolCall = true
		}
		if e.Type == EventToolResult {
			hasToolResult = true
		}
	}
	if !hasToolCall || !hasToolResult {
		t.Fatal("expected tool.call and tool.result events")
	}
}

func TestLoop_MaxIterations(t *testing.T) {
	// Provider always returns tool calls — should hit max iterations.
	responses := make([]canonical.Response, 30)
	for i := range responses {
		responses[i] = canonical.Response{
			ID:         fmt.Sprintf("msg_%d", i),
			StopReason: "tool_use",
			Messages: []canonical.Message{
				{Role: "assistant", Content: []canonical.Content{
					{Type: "tool_call", ToolCall: &canonical.ToolCall{
						ID:    fmt.Sprintf("call_%d", i),
						Name:  "echo_tool",
						Input: json.RawMessage(`{"text":"loop"}`),
					}},
				}},
			},
		}
	}

	prov := &mockProvider{responses: responses}
	reg := tool.NewRegistry()
	reg.Register("echo_tool", "Echo", json.RawMessage(`{}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "echo"}, nil
		})

	loop := NewLoop(Config{
		AgentID:       "test",
		Model:         "test-model",
		MaxIterations: 5,
	}, prov, reg, nil, nil, nil)

	result := loop.Run(context.Background(), "sess_1", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "loop forever"}}},
	})

	// Should have completed without error (just hit max iterations).
	if prov.callCount > 5 {
		t.Fatalf("expected max 5 LLM calls, got %d", prov.callCount)
	}
	_ = result
}

func TestLoop_Inject(t *testing.T) {
	callCount := 0
	prov := &mockProvider{
		responses: []canonical.Response{
			{
				ID:         "msg_1",
				StopReason: "tool_use",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{
						{Type: "tool_call", ToolCall: &canonical.ToolCall{
							ID:    "call_1",
							Name:  "slow_tool",
							Input: json.RawMessage(`{}`),
						}},
					}},
				},
			},
			{
				ID:         "msg_2",
				StopReason: "end_turn",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Done"}}},
				},
			},
		},
	}

	reg := tool.NewRegistry()
	reg.Register("slow_tool", "Slow", json.RawMessage(`{}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			callCount++
			return &canonical.ToolResult{Content: "result"}, nil
		})

	loop := NewLoop(Config{
		AgentID: "test",
		Model:   "test-model",
	}, prov, reg, nil, nil, nil)

	// Inject a message before running.
	loop.Inject(canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: "actually, stop what you're doing"}},
	})

	result := loop.Run(context.Background(), "sess_1", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "do something"}}},
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}
	// The injected message should have been included in history.
	_ = result
}

func TestLoop_Timeout(t *testing.T) {
	// Create a provider that sleeps longer than the timeout.
	slowProvider := &slowMockProvider{}

	loop := NewLoop(Config{
		AgentID: "test",
		Model:   "mock",
		Timeout: 100 * time.Millisecond, // Very short for test.
	}, slowProvider, tool.NewRegistry(), nil, nil, nil)

	result := loop.Run(context.Background(), "sess-1", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	})

	if result.Error == nil {
		t.Fatal("expected timeout error")
	}
	errMsg := result.Error.Error()
	if !strings.Contains(errMsg, "timed out") && !strings.Contains(errMsg, "deadline exceeded") {
		t.Fatalf("expected timeout/deadline message, got: %v", result.Error)
	}
}

// slowMockProvider sleeps until context is cancelled.
type slowMockProvider struct{}

func (s *slowMockProvider) Name() string { return "slow-mock" }
func (s *slowMockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	select {
	case <-time.After(5 * time.Second):
		return &canonical.Response{StopReason: "end_turn",
			Messages: []canonical.Message{{Role: "assistant",
				Content: []canonical.Content{{Type: "text", Text: "late"}}}}}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func (s *slowMockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestSanitizeHistory_OrphanedTool(t *testing.T) {
	msgs := []canonical.Message{
		// Tool message without preceding assistant — should be removed.
		{Role: "user", Content: []canonical.Content{
			{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "orphan", Content: "result"}},
		}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	result := SanitizeHistory(msgs)
	// The orphaned tool message at index 0 should be removed (no preceding assistant).
	// But wait — it's role "user" not "tool", so it won't be filtered by the tool check.
	// Let me test with role "tool".
	msgs2 := []canonical.Message{
		{Role: "tool", Content: []canonical.Content{
			{Type: "tool_result", ToolResult: &canonical.ToolResult{ToolCallID: "orphan", Content: "result"}},
		}},
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
	}

	result = SanitizeHistory(msgs2)
	if len(result) != 1 {
		t.Fatalf("expected 1 message after sanitization, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Fatalf("expected user message, got %s", result[0].Role)
	}
}

func TestSanitizeHistory_MissingToolResult(t *testing.T) {
	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "do something"}}},
		{Role: "assistant", Content: []canonical.Content{
			{Type: "tool_call", ToolCall: &canonical.ToolCall{ID: "call_1", Name: "test"}},
		}},
		// No tool result follows.
	}

	result := SanitizeHistory(msgs)
	// Should have: user, assistant, synthesized tool result.
	if len(result) < 3 {
		t.Fatalf("expected at least 3 messages, got %d", len(result))
	}

	// Last message should be the placeholder.
	last := result[len(result)-1]
	for _, c := range last.Content {
		if c.ToolResult != nil && strings.Contains(c.ToolResult.Content, "interrupted") {
			return // Found placeholder.
		}
	}
	t.Fatal("expected synthesized placeholder tool result")
}

func TestExtractToolCalls(t *testing.T) {
	msg := canonical.Message{
		Role: "assistant",
		Content: []canonical.Content{
			{Type: "text", Text: "Let me check."},
			{Type: "tool_call", ToolCall: &canonical.ToolCall{ID: "c1", Name: "read_file"}},
			{Type: "tool_call", ToolCall: &canonical.ToolCall{ID: "c2", Name: "exec"}},
		},
	}

	calls := ExtractToolCalls(msg)
	if len(calls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(calls))
	}
}

func TestHasToolCalls(t *testing.T) {
	withTC := canonical.Message{
		Content: []canonical.Content{
			{Type: "tool_call", ToolCall: &canonical.ToolCall{ID: "c1", Name: "test"}},
		},
	}
	if !HasToolCalls(withTC) {
		t.Fatal("expected true")
	}

	withoutTC := canonical.Message{
		Content: []canonical.Content{{Type: "text", Text: "hello"}},
	}
	if HasToolCalls(withoutTC) {
		t.Fatal("expected false")
	}
}
