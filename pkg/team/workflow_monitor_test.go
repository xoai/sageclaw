package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
	sqlitestore "github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestMonitorSetup(t *testing.T) (*WorkflowEngine, *WorkflowMonitor, *sqlitestore.Store) {
	t.Helper()
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	exec := NewTeamExecutor(s, nil, func(e agent.Event) {})
	engine := NewWorkflowEngine(s, exec, func(e agent.Event) {})
	return engine, engine.Monitor(), s
}

// Helper: create a workflow in execute state with task IDs.
func setupExecutingWorkflow(t *testing.T, s *sqlitestore.Store, taskCount int) (string, string, []string) {
	t.Helper()
	ctx := context.Background()

	// Create team with a member so FK constraints are satisfied.
	teamID := createTestTeamInDB(t, s, "lead-1", "member-1")
	sessionID := "sess-1"

	wfID, err := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: teamID, SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("create workflow: %v", err)
	}
	// Advance through analyze → plan → create → execute.
	if err := s.UpdateWorkflowState(ctx, wfID, StatePlan, 0, nil); err != nil {
		t.Fatalf("transition to plan: %v", err)
	}
	if err := s.UpdateWorkflowState(ctx, wfID, StateCreate, 1, nil); err != nil {
		t.Fatalf("transition to create: %v", err)
	}

	// Create fake tasks.
	var taskIDs []string
	for i := 0; i < taskCount; i++ {
		taskID, err := s.CreateTask(ctx, store.TeamTask{
			TeamID:     teamID,
			Title:      "Task " + string(rune('A'+i)),
			AssignedTo: "member-1",
			Status:     "in_progress",
			BatchID:    wfID,
		})
		if err != nil {
			t.Fatalf("create task %d: %v", i, err)
		}
		taskIDs = append(taskIDs, taskID)
	}

	taskIDsStr := strings.Join(taskIDs, ",")
	if err := s.UpdateWorkflowState(ctx, wfID, StateExecute, 2, map[string]any{"task_ids": taskIDsStr}); err != nil {
		t.Fatalf("transition to execute: %v", err)
	}

	return wfID, teamID, taskIDs
}

func TestMonitor_StartAndTrack(t *testing.T) {
	_, monitor, _ := newTestMonitorSetup(t)

	monitor.StartMonitoring("wf-1", "team-1", "sess-1", []string{"task-a", "task-b"})

	// Verify internal tracking.
	monitor.mu.Lock()
	if _, ok := monitor.workflows["wf-1"]; !ok {
		t.Error("workflow not tracked")
	}
	if monitor.taskIndex["task-a"] != "wf-1" {
		t.Error("task-a not indexed")
	}
	if monitor.taskIndex["task-b"] != "wf-1" {
		t.Error("task-b not indexed")
	}
	monitor.mu.Unlock()
}

func TestMonitor_IgnoresNonWorkflowEvents(t *testing.T) {
	_, monitor, _ := newTestMonitorSetup(t)

	// No workflows registered — event should be ignored silently.
	monitor.HandleEvent(agent.Event{
		Type: agent.EventTeamTaskCompleted,
		Text: "random-task-id",
	})
	// No panic = pass.
}

func TestMonitor_AllTasksComplete_AdvanceToSynthesize(t *testing.T) {
	engine, monitor, s := newTestMonitorSetup(t)
	_ = engine // Needed for transition.
	ctx := context.Background()

	wfID, teamID, taskIDs := setupExecutingWorkflow(t, s, 2)
	monitor.StartMonitoring(wfID, teamID, "sess-1", taskIDs)

	// Complete task A.
	s.CompleteTask(ctx, taskIDs[0], "Result A")
	monitor.HandleEvent(agent.Event{
		Type: agent.EventTeamTaskCompleted,
		Text: taskIDs[0],
	})

	// Workflow should still be in execute (1/2 done).
	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State == StateSynthesize {
		t.Error("should not advance with 1/2 tasks done")
	}

	// Complete task B.
	s.CompleteTask(ctx, taskIDs[1], "Result B")
	monitor.HandleEvent(agent.Event{
		Type: agent.EventTeamTaskCompleted,
		Text: taskIDs[1],
	})

	// Workflow should advance to synthesize.
	wf, _ = s.GetWorkflow(ctx, wfID)
	if wf.State != StateSynthesize {
		t.Errorf("state = %q, want synthesize", wf.State)
	}

	// Results should be persisted.
	if wf.ResultsJSON == "" {
		t.Error("expected results_json to be populated")
	}
	var results []TaskResult
	json.Unmarshal([]byte(wf.ResultsJSON), &results)
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestMonitor_AllTasksFailed_AdvanceToFailed(t *testing.T) {
	_, monitor, s := newTestMonitorSetup(t)
	ctx := context.Background()

	wfID, teamID, taskIDs := setupExecutingWorkflow(t, s, 2)
	monitor.StartMonitoring(wfID, teamID, "sess-1", taskIDs)

	// Fail both tasks.
	for _, id := range taskIDs {
		s.UpdateTask(ctx, id, map[string]any{"status": "failed", "error_message": "test error"})
		monitor.HandleEvent(agent.Event{
			Type: agent.EventTeamTaskFailed,
			Text: id,
		})
	}

	// Workflow should be failed (all tasks failed → skip SYNTHESIZE).
	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateFailed {
		t.Errorf("state = %q, want failed", wf.State)
	}
}

func TestMonitor_MixedResults_AdvanceToSynthesize(t *testing.T) {
	_, monitor, s := newTestMonitorSetup(t)
	ctx := context.Background()

	wfID, teamID, taskIDs := setupExecutingWorkflow(t, s, 3)
	monitor.StartMonitoring(wfID, teamID, "sess-1", taskIDs)

	// Complete first, fail second, cancel third.
	s.CompleteTask(ctx, taskIDs[0], "Good result")
	monitor.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, Text: taskIDs[0]})

	s.UpdateTask(ctx, taskIDs[1], map[string]any{"status": "failed", "error_message": "oops"})
	monitor.HandleEvent(agent.Event{Type: agent.EventTeamTaskFailed, Text: taskIDs[1]})

	s.CancelTask(ctx, taskIDs[2])
	monitor.HandleEvent(agent.Event{Type: agent.EventTeamTaskCancelled, Text: taskIDs[2]})

	// Should go to synthesize (has at least one completed task).
	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateSynthesize {
		t.Errorf("state = %q, want synthesize", wf.State)
	}
}

func TestMonitor_CancelWorkflow(t *testing.T) {
	_, monitor, s := newTestMonitorSetup(t)
	ctx := context.Background()

	wfID, teamID, taskIDs := setupExecutingWorkflow(t, s, 2)
	monitor.StartMonitoring(wfID, teamID, "sess-1", taskIDs)

	err := monitor.CancelWorkflow(ctx, wfID)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateCancelled {
		t.Errorf("state = %q, want cancelled", wf.State)
	}

	// Tasks should be cancelled too.
	for _, id := range taskIDs {
		task, _ := s.GetTask(ctx, id)
		if task != nil && task.Status != "cancelled" {
			t.Errorf("task %s status = %q, want cancelled", id[:8], task.Status)
		}
	}

	// Monitor should have cleaned up.
	monitor.mu.Lock()
	if _, ok := monitor.workflows[wfID]; ok {
		t.Error("workflow should be cleaned up after cancel")
	}
	monitor.mu.Unlock()
}

func TestMonitor_CancelWorkflow_AlreadyTerminal(t *testing.T) {
	_, monitor, s := newTestMonitorSetup(t)
	ctx := context.Background()

	wfID, _, _ := setupExecutingWorkflow(t, s, 1)
	s.CancelWorkflow(ctx, wfID)

	err := monitor.CancelWorkflow(ctx, wfID)
	if err == nil {
		t.Error("expected error for already-terminal workflow")
	}
}

func TestMonitor_Timeout_AutoFailsStuckTasks(t *testing.T) {
	_, monitor, s := newTestMonitorSetup(t)
	ctx := context.Background()

	wfID, teamID, taskIDs := setupExecutingWorkflow(t, s, 1)
	monitor.StartMonitoring(wfID, teamID, "sess-1", taskIDs)

	// Simulate task claimed long ago.
	oldTime := time.Now().Add(-20 * time.Minute)
	s.UpdateTask(ctx, taskIDs[0], map[string]any{"claimed_at": oldTime.Format("2006-01-02 15:04:05")})

	// Run timeout check.
	monitor.checkTimeouts(10 * time.Minute)

	// Task should be failed.
	task, _ := s.GetTask(ctx, taskIDs[0])
	if task == nil || task.Status != "failed" {
		t.Error("expected task to be auto-failed on timeout")
	}

	// Workflow should advance since all tasks are terminal.
	wf, _ := s.GetWorkflow(ctx, wfID)
	if IsTerminal(wf.State) || wf.State == StateSynthesize || wf.State == StateMonitor {
		// Good — advanced past execute.
	} else {
		t.Errorf("state = %q, expected terminal or synthesize", wf.State)
	}
}

func TestMonitor_Cleanup_RemovesFromMaps(t *testing.T) {
	_, monitor, _ := newTestMonitorSetup(t)

	monitor.StartMonitoring("wf-1", "team-1", "sess-1", []string{"task-a", "task-b"})
	monitor.cleanup("wf-1")

	monitor.mu.Lock()
	defer monitor.mu.Unlock()

	if _, ok := monitor.workflows["wf-1"]; ok {
		t.Error("workflow not cleaned up")
	}
	if _, ok := monitor.taskIndex["task-a"]; ok {
		t.Error("task-a not cleaned up")
	}
}
