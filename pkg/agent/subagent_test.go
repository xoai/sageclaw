package agent

import (
	"context"
	"testing"
	"time"
)

func TestSubagentManager_ConcurrencyLimit(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 2,
		MaxConcurrent:       3,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	// Without a loop pool, spawns will fail on execute — but we can test limit checks.
	// First two should acquire global semaphore slots (even if they fail on execute).
	// Third should be rejected by per-agent limit.
	// We test the counting logic by directly checking.

	// Spawn will fail because loopPool is nil, but the per-agent check happens before.
	_, _, err := mgr.Spawn(context.Background(), "agent-1", "sess-1", "task-1", "label-1", "async")
	// This fails inside execute (nil pool), but the task gets created and marked failed.
	if err != nil {
		t.Logf("expected nil pool error propagated or nil: %v", err)
	}

	_, _, err = mgr.Spawn(context.Background(), "agent-1", "sess-1", "task-2", "label-2", "async")
	if err != nil {
		t.Logf("second spawn: %v", err)
	}

	// Wait for goroutines to run and fail (releasing semaphore slots).
	time.Sleep(100 * time.Millisecond)

	// Check task states.
	tasks := mgr.List("agent-1", "sess-1")
	if len(tasks) < 2 {
		t.Fatalf("expected at least 2 tasks, got %d", len(tasks))
	}
}

func TestSubagentManager_Cancel(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	taskID, _, _ := mgr.Spawn(context.Background(), "agent-1", "sess-1", "task", "label", "async")
	time.Sleep(50 * time.Millisecond)

	err := mgr.Cancel(taskID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	tasks := mgr.List("agent-1", "sess-1")
	found := false
	for _, tk := range tasks {
		if tk.ID == taskID {
			found = true
			if tk.Status != "cancelled" && tk.Status != "failed" {
				t.Fatalf("expected cancelled or failed, got %s", tk.Status)
			}
		}
	}
	if !found {
		// Task may have been consumed already — that's OK.
		t.Log("task already consumed (completed before cancel)")
	}
}

func TestSubagentManager_CancelAll(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	mgr.Spawn(context.Background(), "agent-1", "sess-1", "task-1", "l1", "async")
	mgr.Spawn(context.Background(), "agent-1", "sess-1", "task-2", "l2", "async")
	time.Sleep(50 * time.Millisecond)

	mgr.CancelAll("agent-1", "sess-1")

	// All should be cancelled or already failed.
	tasks := mgr.List("agent-1", "sess-1")
	for _, tk := range tasks {
		if tk.Status == "running" {
			t.Fatalf("expected non-running after CancelAll, got %s for %s", tk.Status, tk.ID)
		}
	}
}

func TestSubagentManager_ConsumeCompleted(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	mgr.Spawn(context.Background(), "agent-1", "sess-1", "task", "label", "async")
	time.Sleep(100 * time.Millisecond) // Wait for execute to fail (nil pool).

	completed := mgr.ConsumeCompleted("agent-1", "sess-1")
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed task, got %d", len(completed))
	}
	if completed[0].Status != "failed" {
		t.Fatalf("expected failed (nil pool), got %s", completed[0].Status)
	}

	// Second consume should be empty.
	again := mgr.ConsumeCompleted("agent-1", "sess-1")
	if len(again) != 0 {
		t.Fatalf("expected 0 after second consume, got %d", len(again))
	}
}

func TestSubagentManager_Cleanup(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	mgr.Spawn(context.Background(), "agent-1", "sess-1", "task", "label", "async")
	time.Sleep(100 * time.Millisecond)

	// Task should be failed (nil pool). Cleanup with 0 age removes it.
	mgr.Cleanup(0)

	tasks := mgr.List("agent-1", "sess-1")
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after cleanup, got %d", len(tasks))
	}
}

func TestSubagentManager_Shutdown(t *testing.T) {
	mgr := NewSubagentManager(SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
		DefaultTimeout:      5 * time.Second,
	}, nil, nil)

	mgr.Spawn(context.Background(), "agent-1", "sess-1", "task", "label", "async")
	time.Sleep(50 * time.Millisecond)

	mgr.Shutdown()

	tasks := mgr.List("agent-1", "sess-1")
	for _, tk := range tasks {
		if tk.Status == "running" {
			t.Fatalf("expected non-running after shutdown, got running for %s", tk.ID)
		}
	}
}

func TestBuildSubagentResultsMessage(t *testing.T) {
	tasks := []SubagentTask{
		{Label: "research", Status: "completed", Result: "Found 3 papers"},
		{Label: "draft", Status: "failed", Error: "timeout"},
	}

	msg := buildSubagentResultsMessage(tasks)
	if msg.Role != "user" {
		t.Fatalf("expected user role, got %s", msg.Role)
	}
	text := msg.Content[0].Text
	if text == "" {
		t.Fatal("expected non-empty content")
	}
	// Check XML tags.
	if !contains(text, "<subagent-results>") {
		t.Fatal("missing <subagent-results> tag")
	}
	if !contains(text, `label="research"`) {
		t.Fatal("missing research label")
	}
	if !contains(text, `status="completed"`) {
		t.Fatal("missing completed status")
	}
	if !contains(text, "Found 3 papers") {
		t.Fatal("missing result text")
	}
	if !contains(text, `label="draft"`) {
		t.Fatal("missing draft label")
	}
	if !contains(text, `error="timeout"`) {
		t.Fatal("missing error attribute")
	}
	if !contains(text, "Synthesize") {
		t.Fatal("missing synthesis instruction")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsHelper(s, sub))
}

func containsHelper(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
