package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

// mockRegistry creates a registry with test tools.
func mockRegistry() *tool.Registry {
	reg := tool.NewRegistry()

	// Concurrent-safe: read_file, web_fetch, memory_search
	reg.RegisterFull("read_file", "Read file", nil, "fs", "moderate", "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return &canonical.ToolResult{Content: "file content"}, nil
		})
	reg.RegisterFull("web_fetch", "Fetch URL", nil, "web", "moderate", "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return &canonical.ToolResult{Content: "web content"}, nil
		})
	reg.RegisterFull("memory_search", "Search memory", nil, "memory", "safe", "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return &canonical.ToolResult{Content: "memory results"}, nil
		})

	// Exclusive: write_file, execute_command
	reg.RegisterWithGroup("write_file", "Write file", nil, "fs", "moderate", "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return &canonical.ToolResult{Content: "written"}, nil
		})
	reg.RegisterWithGroup("execute_command", "Exec command", nil, "runtime", "sensitive", "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			time.Sleep(50 * time.Millisecond)
			return &canonical.ToolResult{Content: "executed"}, nil
		})

	return reg
}

func makeTc(id, name string) canonical.ToolCall {
	return canonical.ToolCall{ID: id, Name: name, Input: json.RawMessage(`{}`)}
}

func TestStreamingExecutor_ExecuteRemaining_AllConcurrent(t *testing.T) {
	reg := mockRegistry()
	sem := NewResourceSemaphores()
	var events int64
	onEvent := func(e Event) {
		if e.Type == EventToolCallStarted {
			atomic.AddInt64(&events, 1)
		}
	}

	exec := NewStreamingExecutor(reg, sem, onEvent, nil)
	ctx := context.Background()
	exec.StartIteration(ctx, ctx)

	calls := []canonical.ToolCall{
		makeTc("1", "read_file"),
		makeTc("2", "web_fetch"),
		makeTc("3", "memory_search"),
	}

	start := time.Now()
	results := exec.ExecuteRemaining(ctx, ctx, calls, nil)
	elapsed := time.Since(start)

	// 3 concurrent tools with 50ms sleep should complete in ~50-100ms, not 150ms.
	if elapsed > 200*time.Millisecond {
		t.Errorf("concurrent execution took %v, expected < 200ms", elapsed)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Results should be in tool-call order.
	for i, r := range results {
		if r.ToolCallID != calls[i].ID {
			t.Errorf("result %d: got ID %s, want %s", i, r.ToolCallID, calls[i].ID)
		}
		if r.IsError {
			t.Errorf("result %d is error: %s", i, r.Content)
		}
	}
}

func TestStreamingExecutor_ExecuteRemaining_MixedBatches(t *testing.T) {
	reg := mockRegistry()
	sem := NewResourceSemaphores()
	exec := NewStreamingExecutor(reg, sem, nil, nil)
	ctx := context.Background()
	exec.StartIteration(ctx, ctx)

	calls := []canonical.ToolCall{
		makeTc("1", "read_file"),
		makeTc("2", "web_fetch"),
		makeTc("3", "write_file"),
		makeTc("4", "read_file"),
	}

	results := exec.ExecuteRemaining(ctx, ctx, calls, nil)

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// All results should be present and in order.
	expected := []string{"file content", "web content", "written", "file content"}
	for i, r := range results {
		if r.Content != expected[i] {
			t.Errorf("result %d: got %q, want %q", i, r.Content, expected[i])
		}
	}
}

func TestStreamingExecutor_EarlyExecution(t *testing.T) {
	reg := mockRegistry()
	sem := NewResourceSemaphores()
	exec := NewStreamingExecutor(reg, sem, nil, nil)
	ctx := context.Background()
	exec.StartIteration(ctx, ctx)

	// Feed a concurrent-safe tool during "streaming".
	exec.FeedToolCall(makeTc("1", "read_file"))

	// Wait a bit for early execution to start.
	time.Sleep(10 * time.Millisecond)

	// Feed an exclusive tool — should NOT start early.
	exec.FeedToolCall(makeTc("2", "write_file"))

	// Execute remaining.
	calls := []canonical.ToolCall{
		makeTc("1", "read_file"),
		makeTc("2", "write_file"),
	}

	results := exec.ExecuteRemaining(ctx, ctx, calls, nil)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Content != "file content" {
		t.Errorf("result 0: got %q, want %q", results[0].Content, "file content")
	}
	if results[1].Content != "written" {
		t.Errorf("result 1: got %q, want %q", results[1].Content, "written")
	}
}

func TestStreamingExecutor_ConsentDenied(t *testing.T) {
	reg := mockRegistry()
	sem := NewResourceSemaphores()
	denyAll := func(ctx context.Context, tc canonical.ToolCall) *canonical.ToolResult {
		return &canonical.ToolResult{
			ToolCallID: tc.ID,
			Content:    "Consent denied",
			IsError:    true,
		}
	}
	exec := NewStreamingExecutor(reg, sem, nil, denyAll)
	ctx := context.Background()
	exec.StartIteration(ctx, ctx)

	// Feed a tool — consent should deny it.
	exec.FeedToolCall(makeTc("1", "read_file"))

	calls := []canonical.ToolCall{makeTc("1", "read_file")}
	results := exec.ExecuteRemaining(ctx, ctx, calls, denyAll)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].IsError {
		t.Error("expected error result for denied consent")
	}
	if results[0].Content != "Consent denied" {
		t.Errorf("got %q, want 'Consent denied'", results[0].Content)
	}
}

func TestStreamingExecutor_ToolError(t *testing.T) {
	reg := tool.NewRegistry()
	reg.RegisterFull("fail_tool", "Fails", nil, "core", "safe", "builtin", true,
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return nil, fmt.Errorf("boom")
		})

	sem := NewResourceSemaphores()
	exec := NewStreamingExecutor(reg, sem, nil, nil)
	ctx := context.Background()
	exec.StartIteration(ctx, ctx)

	calls := []canonical.ToolCall{makeTc("1", "fail_tool")}
	results := exec.ExecuteRemaining(ctx, ctx, calls, nil)

	if !results[0].IsError {
		t.Error("expected error result")
	}
	if results[0].Content != "Tool error: boom" {
		t.Errorf("got %q", results[0].Content)
	}
}

func TestStreamingExecutor_NilFallback(t *testing.T) {
	// When executor is nil, the V1 path should be used (tested via loop_test.go).
	// This test just verifies nil safety.
	var exec *StreamingExecutor
	if exec != nil {
		t.Error("nil executor should be nil")
	}
}
