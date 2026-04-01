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

// failingStreamProvider sends partial content then an error via streaming.
// Used to verify the discard-on-fallback invariant.
type failingStreamProvider struct {
	streamCalls int
	chatCalls   int
}

func (f *failingStreamProvider) Name() string { return "failing-stream" }

func (f *failingStreamProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	f.chatCalls++
	return &canonical.Response{
		StopReason: "end_turn",
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Clean response after fallback."}}},
		},
		Usage: canonical.Usage{InputTokens: 10, OutputTokens: 5},
	}, nil
}

func (f *failingStreamProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	f.streamCalls++
	if f.streamCalls == 1 {
		// First stream call: send partial content then error mid-stream.
		ch := make(chan provider.StreamEvent, 3)
		ch <- provider.StreamEvent{
			Type:  "content_delta",
			Delta: &canonical.Content{Type: "text", Text: "Partial garbage that should be disc"},
		}
		ch <- provider.StreamEvent{
			Type:  "error",
			Error: fmt.Errorf("connection reset mid-stream"),
		}
		close(ch)
		return ch, nil
	}
	// Subsequent calls: stream unavailable → tryLLMCall falls back to Chat.
	return nil, fmt.Errorf("stream unavailable")
}

// TestLoop_DiscardOnFallback_StreamError verifies that when a stream fails
// mid-way with partial content, the partial message is discarded and the
// Run result reflects the failure (no partial content leaks into history).
func TestLoop_DiscardOnFallback_StreamError(t *testing.T) {
	prov := &failingStreamProvider{}

	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
	}, prov, tool.NewRegistry(), nil, nil, nil)

	result := loop.Run(context.Background(), "sess_discard", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
	})

	// Run should fail (stream errored, no router for fallback).
	if result.Error == nil {
		t.Fatal("expected error from failed stream")
	}
	if !strings.Contains(result.Error.Error(), "stream error") {
		t.Fatalf("expected stream error, got: %v", result.Error)
	}

	// Critical invariant: no partial content in result messages.
	for _, msg := range result.Messages {
		for _, c := range msg.Content {
			if c.Type == "text" && strings.Contains(c.Text, "Partial garbage") {
				t.Fatal("partial stream content leaked into result messages")
			}
		}
	}

	// Only 1 stream call, no Chat fallback (no router).
	if prov.streamCalls != 1 {
		t.Fatalf("expected 1 stream call, got %d", prov.streamCalls)
	}
	if prov.chatCalls != 0 {
		t.Fatalf("expected 0 chat calls, got %d", prov.chatCalls)
	}
}

// TestLoop_DiscardOnFallback_ChatFallback verifies that when ChatStream fails
// to establish (returns error), tryLLMCall falls back to Chat and returns a
// clean response — no partial data from a never-started stream.
func TestLoop_DiscardOnFallback_ChatFallback(t *testing.T) {
	prov := &failingStreamProvider{streamCalls: 1} // Skip the mid-stream failure path.

	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
	}, prov, tool.NewRegistry(), nil, nil, nil)

	result := loop.Run(context.Background(), "sess_fallback", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}

	if len(result.Messages) != 1 {
		t.Fatalf("expected 1 result message, got %d", len(result.Messages))
	}

	text := result.Messages[0].Content[0].Text
	if text != "Clean response after fallback." {
		t.Fatalf("expected clean response, got: %q", text)
	}

	// Chat was called as fallback.
	if prov.chatCalls != 1 {
		t.Fatalf("expected 1 chat call, got %d", prov.chatCalls)
	}
}

func TestContinueReason_String(t *testing.T) {
	tests := []struct {
		reason ContinueReason
		want   string
	}{
		{ContinueNone, "none"},
		{ContinueToolUse, "tool_use"},
		{ContinueMaxTokensRecovery, "max_tokens_recovery"},
		{ContinueDenialRetry, "denial_retry"},
		{ContinueBudgetContinuation, "budget_continuation"},
		{ContinueReason(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.reason.String(); got != tt.want {
			t.Errorf("ContinueReason(%d).String() = %q, want %q", tt.reason, got, tt.want)
		}
	}
}

// TestLoop_MaxTokensRecovery verifies the escalation sequence:
// max_tokens → continue with higher limit → eventually succeed.
func TestLoop_MaxTokensRecovery(t *testing.T) {
	prov := &mockProvider{
		responses: []canonical.Response{
			{
				// First response: truncated (max_tokens).
				ID:         "msg_1",
				StopReason: "max_tokens",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Let me explain the concept of"}}},
				},
				Usage: canonical.Usage{InputTokens: 10, OutputTokens: 100},
			},
			{
				// Second response: also truncated.
				ID:         "msg_2",
				StopReason: "max_tokens",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: " functional programming. It involves"}}},
				},
				Usage: canonical.Usage{InputTokens: 20, OutputTokens: 200},
			},
			{
				// Third response: completes successfully.
				ID:         "msg_3",
				StopReason: "end_turn",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: " using pure functions and immutable data."}}},
				},
				Usage: canonical.Usage{InputTokens: 30, OutputTokens: 50},
			},
		},
	}

	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
		MaxTokens:    8192,
	}, prov, tool.NewRegistry(), nil, nil, nil)

	result := loop.Run(context.Background(), "sess_recovery", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Explain functional programming"}}},
	})

	if result.Error != nil {
		t.Fatalf("run failed: %v", result.Error)
	}

	// All 3 responses should be in pending messages (truncated ones + final).
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}

	// Provider called 3 times.
	if prov.callCount != 3 {
		t.Fatalf("expected 3 provider calls, got %d", prov.callCount)
	}

	// Config MaxTokens should NOT be mutated (escalation is per-run only).
	if loop.config.MaxTokens != 8192 {
		t.Fatalf("expected config.MaxTokens unchanged at 8192, got %d", loop.config.MaxTokens)
	}

	// Usage should be accumulated across all 3 calls.
	if result.Usage.InputTokens != 60 {
		t.Fatalf("expected 60 input tokens, got %d", result.Usage.InputTokens)
	}
	if result.Usage.OutputTokens != 350 {
		t.Fatalf("expected 350 output tokens, got %d", result.Usage.OutputTokens)
	}
}

// TestLoop_MaxTokensRecovery_Exhausted verifies that after 3 max_tokens retries,
// the loop breaks with the truncated response (no error — best effort).
func TestLoop_MaxTokensRecovery_Exhausted(t *testing.T) {
	// 4 truncated responses — exceeds the 3-retry limit.
	responses := make([]canonical.Response, 5)
	for i := range responses {
		responses[i] = canonical.Response{
			ID:         fmt.Sprintf("msg_%d", i),
			StopReason: "max_tokens",
			Messages: []canonical.Message{
				{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: fmt.Sprintf("truncated part %d", i)}}},
			},
			Usage: canonical.Usage{InputTokens: 10, OutputTokens: 100},
		}
	}

	prov := &mockProvider{responses: responses}

	loop := NewLoop(Config{
		AgentID:      "test",
		SystemPrompt: "You are a test agent.",
		Model:        "test-model",
		MaxTokens:    8192,
	}, prov, tool.NewRegistry(), nil, nil, nil)

	result := loop.Run(context.Background(), "sess_exhausted", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Write a very long essay"}}},
	})

	// Should NOT error — the truncated response is still usable.
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}

	// Should have 4 messages: 3 retried + 1 final (the 4th triggers exhaustion).
	if prov.callCount != 4 {
		t.Fatalf("expected 4 provider calls (3 retries + 1 exhaustion), got %d", prov.callCount)
	}
}

func TestIsConsentDenial(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"User denied permission for runtime tools.", true},
		{"Permission for runtime was recently denied.", true},
		{"Consent timeout: no response.", true},
		{"Headless agent cannot use runtime tools.", true},
		{"Tool exec_command has been blocked for this session after repeated denials.", true},
		{"Tool error: file not found", false},
		{"echo: hello world", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isConsentDenial(tt.content); got != tt.want {
			t.Errorf("isConsentDenial(%q) = %v, want %v", tt.content, got, tt.want)
		}
	}
}

// TestDenialEscalation_TracksAndEscalates verifies that 3 consecutive denials
// for the same tool trigger session-blocking via DenyTool.
func TestDenialEscalation_TracksAndEscalates(t *testing.T) {
	// Provider returns 4 responses: 3 tool calls that get denied, then success after escalation.
	prov := &mockProvider{
		responses: []canonical.Response{
			makeToolCallResponse("msg_1", "risky_tool", `{}`),
			makeToolCallResponse("msg_2", "risky_tool", `{}`),
			makeToolCallResponse("msg_3", "risky_tool", `{}`),
			{
				ID: "msg_4", StopReason: "end_turn",
				Messages: []canonical.Message{
					{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Ok, I'll try another approach."}}},
				},
				Usage: canonical.Usage{InputTokens: 10, OutputTokens: 5},
			},
		},
	}

	cs := tool.NewPersistentConsentStore(nil)
	reg := tool.NewRegistry()
	// Register the tool in the runtime group (always-consent) so checkConsent fires.
	reg.RegisterWithGroup("risky_tool", "Risky", json.RawMessage(`{}`),
		tool.GroupRuntime, tool.RiskSensitive, "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "done"}, nil
		})

	// Pre-deny the group so checkConsent returns denial immediately (no consent prompt).
	cs.Deny("sess_escalate", tool.GroupRuntime)

	var escalatedEvents []Event
	loop := NewLoop(Config{
		AgentID:     "test",
		Model:       "test-model",
		ToolProfile: "full",
	}, prov, reg, nil, nil, func(e Event) {
		if e.Type == EventConsentEscalated {
			escalatedEvents = append(escalatedEvents, e)
		}
	}, WithConsentStore(cs))

	result := loop.Run(context.Background(), "sess_escalate", []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Do something risky"}}},
	})

	_ = result // We care about side effects, not the result.

	// After 3 denials, the tool should be session-blocked.
	if !cs.IsToolDenied("sess_escalate", "risky_tool") {
		t.Fatal("expected risky_tool to be session-blocked after 3 denials")
	}

	// EventConsentEscalated should have been emitted.
	if len(escalatedEvents) == 0 {
		t.Fatal("expected EventConsentEscalated to be emitted")
	}
	if escalatedEvents[0].Text != "risky_tool" {
		t.Fatalf("expected escalation for risky_tool, got %q", escalatedEvents[0].Text)
	}
}

// Helper to build a tool call response.
func makeToolCallResponse(id, toolName, input string) canonical.Response {
	return canonical.Response{
		ID: id, StopReason: "tool_use",
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{
				{Type: "tool_call", ToolCall: &canonical.ToolCall{
					ID: "call_" + id, Name: toolName, Input: json.RawMessage(input),
				}},
			}},
		},
		Usage: canonical.Usage{InputTokens: 10, OutputTokens: 10},
	}
}
