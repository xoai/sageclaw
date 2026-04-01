package context

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// mockLLMCaller returns a canned response or error.
func mockLLMCaller(response string, err error) LLMCaller {
	return func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		return response, err
	}
}

func makeToolResultWithAnnotation(callID, content string, iteration int) canonical.Message {
	return canonical.Message{
		Role: "user",
		Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: callID, Content: content},
		}},
		Annotations: &canonical.MessageAnnotations{Iteration: iteration},
	}
}

func TestMicroCompact_EligibleResult(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", bigContent, 1),
	}

	caller := mockLLMCaller("Compressed content here", nil)
	out := applyMicroCompact(context.Background(), history, 10, 5, caller)

	result := out[0].Content[0].ToolResult.Content
	if !strings.Contains(result, "Compressed content here") {
		t.Error("compressed content not applied")
	}
	if !strings.Contains(result, "[micro-compacted from 3000 chars]") {
		t.Error("micro-compact marker missing")
	}
}

func TestMicroCompact_TooYoung(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", bigContent, 8),
	}

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "compressed", nil
	}
	out := applyMicroCompact(context.Background(), history, 10, 5, caller)

	if called {
		t.Error("LLM should not be called for young messages")
	}
	if out[0].Content[0].ToolResult.Content != bigContent {
		t.Error("young message should not be modified")
	}
}

func TestMicroCompact_TooSmall(t *testing.T) {
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", "small result", 1),
	}

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "compressed", nil
	}
	_ = applyMicroCompact(context.Background(), history, 100, 5, caller)

	if called {
		t.Error("LLM should not be called for small results")
	}
}

func TestMicroCompact_SkipsSnipped(t *testing.T) {
	history := []canonical.Message{
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: strings.Repeat("X", 3000)},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: 1, Snipped: true},
		},
	}

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "compressed", nil
	}
	_ = applyMicroCompact(context.Background(), history, 100, 5, caller)

	if called {
		t.Error("LLM should not be called for snipped messages")
	}
}

func TestMicroCompact_SkipsOverflowed(t *testing.T) {
	history := []canonical.Message{
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: strings.Repeat("X", 3000)},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: 1, OverflowPath: "/tmp/overflow.txt"},
		},
	}

	called := false
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		called = true
		return "compressed", nil
	}
	_ = applyMicroCompact(context.Background(), history, 100, 5, caller)

	if called {
		t.Error("LLM should not be called for overflowed messages")
	}
}

func TestMicroCompact_LLMFailureGraceful(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", bigContent, 1),
	}

	caller := mockLLMCaller("", errors.New("rate limited"))
	out := applyMicroCompact(context.Background(), history, 100, 5, caller)

	// Should be unchanged on failure.
	if out[0].Content[0].ToolResult.Content != bigContent {
		t.Error("content should be unchanged on LLM failure")
	}
}

func TestMicroCompact_NilCaller(t *testing.T) {
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", strings.Repeat("X", 3000), 1),
	}

	out := applyMicroCompact(context.Background(), history, 100, 5, nil)
	if out[0].Content[0].ToolResult.Content != strings.Repeat("X", 3000) {
		t.Error("nil caller should pass through")
	}
}

func TestMicroCompact_BatchCap(t *testing.T) {
	// Create 10 eligible results — only first 5 should be batched.
	var history []canonical.Message
	for i := 0; i < 10; i++ {
		history = append(history, makeToolResultWithAnnotation(
			"tc"+string(rune('A'+i)), strings.Repeat("X", 3000), 1,
		))
	}

	var receivedPrompt string
	caller := func(ctx context.Context, system, user string, timeout time.Duration) (string, error) {
		receivedPrompt = user
		return "--- Result 1 ---\nC1\n--- Result 2 ---\nC2\n--- Result 3 ---\nC3\n--- Result 4 ---\nC4\n--- Result 5 ---\nC5", nil
	}

	_ = applyMicroCompact(context.Background(), history, 100, 5, caller)

	// Should only contain 5 results in prompt.
	count := strings.Count(receivedPrompt, "--- Result")
	if count != 5 {
		t.Errorf("expected 5 results in prompt, got %d", count)
	}
}

func TestMicroCompact_OriginalNotMutated(t *testing.T) {
	original := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", original, 1),
	}

	caller := mockLLMCaller("compressed", nil)
	_ = applyMicroCompact(context.Background(), history, 100, 5, caller)

	if history[0].Content[0].ToolResult.Content != original {
		t.Error("original history was mutated")
	}
}

func TestMicroCompact_TokenEstimateReset(t *testing.T) {
	history := []canonical.Message{
		{
			Role: "user",
			Content: []canonical.Content{{
				Type:       "tool_result",
				ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: strings.Repeat("X", 3000)},
			}},
			Annotations: &canonical.MessageAnnotations{Iteration: 1, TokenEstimate: 750},
		},
	}

	caller := mockLLMCaller("compressed", nil)
	out := applyMicroCompact(context.Background(), history, 100, 5, caller)

	if out[0].Annotations.TokenEstimate != 0 {
		t.Errorf("token estimate should be reset, got %d", out[0].Annotations.TokenEstimate)
	}
}

func TestParseMicroCompactResponse_Numbered(t *testing.T) {
	response := "--- Result 1 ---\nCompressed A\n\n--- Result 2 ---\nCompressed B\n"
	parts := parseMicroCompactResponse(response, 2)

	if parts[0] != "Compressed A" {
		t.Errorf("part 0 = %q", parts[0])
	}
	if parts[1] != "Compressed B" {
		t.Errorf("part 1 = %q", parts[1])
	}
}

func TestParseMicroCompactResponse_SingleResult(t *testing.T) {
	response := "This is the compressed content."
	parts := parseMicroCompactResponse(response, 1)

	if parts[0] != "This is the compressed content." {
		t.Errorf("single result = %q", parts[0])
	}
}

// --- Content-clear tests ---

func TestContentClear_EligibleResult(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		// Assistant with tool_call so buildToolNameMap can resolve the name.
		{Role: "assistant", Content: []canonical.Content{{
			Type:     "tool_call",
			ToolCall: &canonical.ToolCall{ID: "tc1", Name: "read_file"},
		}}, Annotations: &canonical.MessageAnnotations{Iteration: 1}},
		makeToolResultWithAnnotation("tc1", bigContent, 1),
	}

	out := applyContentClear(history, 10, 5) // age=9 >= 5, size=3000 >= 2000

	result := out[1].Content[0].ToolResult.Content
	if !strings.Contains(result, "Tool result cleared") {
		t.Errorf("expected content-clear placeholder, got: %s", result)
	}
	if !strings.Contains(result, "read_file") {
		t.Error("placeholder should contain tool name")
	}
	if !strings.Contains(result, "3000 chars") {
		t.Error("placeholder should contain original size")
	}
}

func TestContentClear_TooYoung(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", bigContent, 8),
	}

	out := applyContentClear(history, 10, 5) // age=2 < 5

	if out[0].Content[0].ToolResult.Content != bigContent {
		t.Error("young message should not be cleared")
	}
}

func TestContentClear_TooSmall(t *testing.T) {
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", "small result", 1),
	}

	out := applyContentClear(history, 10, 5) // size < 2000

	if out[0].Content[0].ToolResult.Content != "small result" {
		t.Error("small result should not be cleared")
	}
}

func TestContentClear_SkipsSnipped(t *testing.T) {
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{
			Type:       "tool_result",
			ToolResult: &canonical.ToolResult{ToolCallID: "tc1", Content: bigContent},
		}}, Annotations: &canonical.MessageAnnotations{Iteration: 1, Snipped: true}},
	}

	out := applyContentClear(history, 10, 5)

	if out[0].Content[0].ToolResult.Content != bigContent {
		t.Error("already-snipped message should not be cleared")
	}
}

func TestContentClear_NoLLMCall(t *testing.T) {
	// Content-clear should never invoke an LLM — it's purely string replacement.
	// This test verifies the function signature has no LLMCaller parameter.
	bigContent := strings.Repeat("X", 3000)
	history := []canonical.Message{
		makeToolResultWithAnnotation("tc1", bigContent, 1),
	}

	// applyContentClear takes no LLMCaller — compile-time guarantee.
	out := applyContentClear(history, 10, 5)

	if strings.Contains(out[0].Content[0].ToolResult.Content, bigContent) {
		t.Error("eligible result should have been cleared")
	}
}
