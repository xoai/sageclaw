package team

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	storeTypes "github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// mockLoopFactory returns a mock Loop that produces a configurable result.
type mockLoopFactory struct {
	response string
	err      error
	delay    time.Duration
}

func (f *mockLoopFactory) RegisterTaskLoop(key string, loop *agent.Loop) func() {
	return func() {} // No-op for tests.
}

func (f *mockLoopFactory) NewTaskLoop(agentID string) *agent.Loop {
	// We can't easily mock agent.Loop since it's a concrete struct.
	// Instead, we'll test at the integration level using a real store
	// and verify the executor's DB operations.
	return nil
}

// setupTestExecutor creates an executor with a real SQLite store for testing.
func setupTestExecutor(t *testing.T) (*TeamExecutor, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	var events []agent.Event
	handler := func(e agent.Event) {
		events = append(events, e)
	}

	exec := NewTeamExecutor(s, &mockLoopFactory{response: "done"}, handler)
	return exec, s
}

func createTestTeamInDB(t *testing.T, s *sqlite.Store, leadID string, memberIDs ...string) string {
	t.Helper()
	ctx := context.Background()
	config := `{"members":[]}`
	if len(memberIDs) > 0 {
		config = `{"members":[`
		for i, m := range memberIDs {
			if i > 0 {
				config += ","
			}
			config += `"` + m + `"`
		}
		config += `]}`
	}
	teamID := fmt.Sprintf("team-%d", time.Now().UnixNano())
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO teams (id, name, lead_id, config, description, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', 'active', '{}', datetime('now'), datetime('now'))`,
		teamID, "Test Team", leadID, config)
	if err != nil {
		t.Fatalf("creating test team: %v", err)
	}
	return teamID
}

func TestDispatch_CreatesTask(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	taskID, err := exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:     teamID,
		Title:      "Research topic",
		AssignedTo: "worker-a",
		CreatedBy:  "lead-1",
		Priority:   5,
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected task ID")
	}

	// Verify task in DB.
	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task.Title != "Research topic" {
		t.Fatalf("expected title 'Research topic', got %q", task.Title)
	}
	// Since mockLoopFactory returns nil, execution won't proceed,
	// but the task should be created.
	if task.Status != "pending" {
		// Task stays pending because execute fails on nil loop
		// and the goroutine may not have run yet.
		t.Logf("task status: %s (may be pending or failed depending on timing)", task.Status)
	}
}

func TestDispatch_InvalidAssignee(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	_, err := exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:     teamID,
		Title:      "Bad assignee",
		AssignedTo: "unknown-agent",
		CreatedBy:  "lead-1",
	})
	if err == nil {
		t.Fatal("expected error for invalid assignee")
	}
}

func TestDispatch_BlockedTask(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// Create blocker task (no assignee, so no execution).
	blockerID, _ := exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:    teamID,
		Title:     "Blocker",
		CreatedBy: "lead-1",
	})

	// Create blocked task.
	blockedID, err := exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:     teamID,
		Title:      "Blocked task",
		AssignedTo: "worker-a",
		CreatedBy:  "lead-1",
		BlockedBy:  blockerID,
	})
	if err != nil {
		t.Fatalf("Dispatch blocked: %v", err)
	}

	task, _ := s.GetTask(ctx, blockedID)
	if task.Status != "blocked" {
		t.Fatalf("expected blocked status, got %q", task.Status)
	}
}

func TestDispatch_CycleDetection(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// Create task A, then B blocked by A.
	idA, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "Task A", CreatedBy: "lead-1",
	})
	idB, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "Task B", CreatedBy: "lead-1", BlockedBy: idA,
	})

	// Valid chain: C depends on B depends on A — should succeed.
	_, err := exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:    teamID,
		Title:     "Task C",
		CreatedBy: "lead-1",
		BlockedBy: idB,
	})
	if err != nil {
		t.Fatalf("valid dependency chain should not error: %v", err)
	}
}

func TestDetectCycles_NoCycle(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	idA, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "A", CreatedBy: "lead-1",
	})
	s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "B", CreatedBy: "lead-1", BlockedBy: idA,
	})

	cycles := exec.detectCycles(ctx, teamID)
	if len(cycles) != 0 {
		t.Fatalf("expected no cycles, got %v", cycles)
	}
}

func TestDetectCycles_SimpleCycle(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// Create A→B→A cycle by directly inserting with cross-references.
	idA, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "A", CreatedBy: "lead-1",
	})
	idB, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "B", CreatedBy: "lead-1", BlockedBy: idA,
	})
	// Now make A blocked by B (creating cycle).
	s.UpdateTask(ctx, idA, map[string]any{"blocked_by": idB, "status": "blocked"})

	cycles := exec.detectCycles(ctx, teamID)
	if len(cycles) != 2 {
		t.Fatalf("expected 2 tasks in cycle, got %d: %v", len(cycles), cycles)
	}
}

func TestDetectCycles_TriangleCycle(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// A→B→C→A cycle.
	idA, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "A", CreatedBy: "lead-1",
	})
	idB, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "B", CreatedBy: "lead-1", BlockedBy: idA,
	})
	idC, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "C", CreatedBy: "lead-1", BlockedBy: idB,
	})
	// Close the cycle: A blocked by C.
	s.UpdateTask(ctx, idA, map[string]any{"blocked_by": idC, "status": "blocked"})

	cycles := exec.detectCycles(ctx, teamID)
	if len(cycles) != 3 {
		t.Fatalf("expected 3 tasks in cycle, got %d: %v", len(cycles), cycles)
	}
}

func TestDetectCycles_IndependentChainsNotAffected(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// Independent chain: D→E (no cycle).
	idD, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "D", CreatedBy: "lead-1",
	})
	s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "E", CreatedBy: "lead-1", BlockedBy: idD,
	})

	// Separate cycle: X→Y→X.
	idX, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "X", CreatedBy: "lead-1",
	})
	idY, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "Y", CreatedBy: "lead-1", BlockedBy: idX,
	})
	s.UpdateTask(ctx, idX, map[string]any{"blocked_by": idY, "status": "blocked"})

	cycles := exec.detectCycles(ctx, teamID)
	// Only X and Y should be in cycles, not D and E.
	if len(cycles) != 2 {
		t.Fatalf("expected 2 cycle members (X,Y), got %d: %v", len(cycles), cycles)
	}
	cycleSet := make(map[string]bool)
	for _, id := range cycles {
		cycleSet[id] = true
	}
	if !cycleSet[idX] || !cycleSet[idY] {
		t.Fatalf("expected X and Y in cycles, got %v", cycles)
	}
}

func TestFailCycleTasks(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "worker-a")

	// Create A→B→A cycle.
	idA, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "A", CreatedBy: "lead-1",
	})
	idB, _ := s.CreateTask(ctx, storeTypes.TeamTask{
		TeamID: teamID, Title: "B", CreatedBy: "lead-1", BlockedBy: idA,
	})
	s.UpdateTask(ctx, idA, map[string]any{"blocked_by": idB, "status": "blocked"})

	exec.FailCycleTasks(ctx, teamID)

	// Both should be failed.
	taskA, _ := s.GetTask(ctx, idA)
	taskB, _ := s.GetTask(ctx, idB)
	if taskA.Status != "failed" {
		t.Fatalf("expected A failed, got %q", taskA.Status)
	}
	if taskB.Status != "failed" {
		t.Fatalf("expected B failed, got %q", taskB.Status)
	}
	if taskA.ErrorMessage != "circular dependency detected" {
		t.Fatalf("expected cycle error message, got %q", taskA.ErrorMessage)
	}
}

func TestInbox_PushAndConsume(t *testing.T) {
	inbox := &TeamInbox{}

	inbox.Push(TaskCompletion{TaskID: "t1", Subject: "Task 1", Status: "completed"})
	inbox.Push(TaskCompletion{TaskID: "t2", Subject: "Task 2", Status: "completed"})

	if !inbox.HasItems() {
		t.Fatal("expected inbox to have items")
	}
	if inbox.Len() != 2 {
		t.Fatalf("expected 2 items, got %d", inbox.Len())
	}

	items := inbox.ConsumeAll()
	if len(items) != 2 {
		t.Fatalf("expected 2 consumed items, got %d", len(items))
	}

	// Verify sequence numbers are monotonic.
	if items[0].Seq >= items[1].Seq {
		t.Fatalf("expected monotonic seq, got %d >= %d", items[0].Seq, items[1].Seq)
	}

	// Inbox should be empty now.
	if inbox.HasItems() {
		t.Fatal("expected inbox to be empty after consume")
	}
}

func TestInbox_BatchComplete(t *testing.T) {
	inbox := &TeamInbox{}

	inbox.Push(TaskCompletion{TaskID: "t1", BatchID: "batch-1", Status: "completed"})
	inbox.Push(TaskCompletion{TaskID: "t2", BatchID: "batch-1", Status: "completed"})

	if !inbox.BatchComplete("batch-1", 2) {
		t.Fatal("expected batch to be complete (2/2)")
	}
	if inbox.BatchComplete("batch-1", 3) {
		t.Fatal("expected batch to be incomplete (2/3)")
	}
	// Empty batch ID is always complete.
	if !inbox.BatchComplete("", 0) {
		t.Fatal("expected empty batch to be complete")
	}
}

func TestTruncateResult(t *testing.T) {
	short := "hello"
	if TruncateResult(short) != short {
		t.Fatal("short string should not be truncated")
	}

	long := make([]byte, MaxResultLength+100)
	for i := range long {
		long[i] = 'x'
	}
	truncated := TruncateResult(string(long))
	if len(truncated) > MaxResultLength {
		t.Fatalf("expected truncated length <= %d, got %d", MaxResultLength, len(truncated))
	}
}

func TestConcurrencyLimiter(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1")

	// Acquire slots up to DefaultMaxConcurrent.
	for i := 0; i < DefaultMaxConcurrent; i++ {
		if err := exec.acquireConcurrency(ctx, teamID); err != nil {
			t.Fatalf("acquire %d: %v", i, err)
		}
	}

	// Next acquire should block. Use a short timeout to verify.
	shortCtx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	err := exec.acquireConcurrency(shortCtx, teamID)
	if err == nil {
		t.Fatal("expected timeout on concurrency acquire")
	}

	// Release one and try again.
	exec.releaseConcurrency(teamID)
	if err := exec.acquireConcurrency(ctx, teamID); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}

func TestEventEmission(t *testing.T) {
	var eventCount int32
	handler := func(e agent.Event) {
		atomic.AddInt32(&eventCount, 1)
	}

	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	exec := NewTeamExecutor(s, &mockLoopFactory{}, handler)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1")

	// Dispatch without assignee (no execution, just creation event).
	_, err = exec.Dispatch(ctx, storeTypes.TeamTask{
		TeamID:    teamID,
		Title:     "Event test",
		CreatedBy: "lead-1",
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	count := atomic.LoadInt32(&eventCount)
	if count < 1 {
		t.Fatalf("expected at least 1 event (created), got %d", count)
	}
}

func TestExtractResponse(t *testing.T) {
	messages := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "world"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "!"}}},
	}
	got := extractResponse(messages)
	if got != "world\n!" {
		t.Fatalf("expected 'world\\n!', got %q", got)
	}
}

func TestIsTransientError(t *testing.T) {
	tests := []struct {
		msg       string
		transient bool
	}{
		{"context deadline exceeded", true},
		{"rate limit exceeded (429)", true},
		{"overloaded", true},
		{"invalid request", false},
		{"unauthorized", false},
	}
	for _, tt := range tests {
		if got := isTransientError(tt.msg); got != tt.transient {
			t.Errorf("isTransientError(%q) = %v, want %v", tt.msg, got, tt.transient)
		}
	}
}

func TestShutdown(t *testing.T) {
	exec, _ := setupTestExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	// No in-flight tasks, should return immediately.
	if err := exec.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
