package team

import (
	"context"
	"sync"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
)

// mockNotifierStore implements the subset of store.Store used by the notifier.
type mockNotifierStore struct {
	store.Store
	tasks map[string]*store.TeamTask
	teams map[string]*store.Team
}

func (m *mockNotifierStore) GetTask(ctx context.Context, taskID string) (*store.TeamTask, error) {
	return m.tasks[taskID], nil
}

func (m *mockNotifierStore) GetTeam(ctx context.Context, teamID string) (*store.Team, error) {
	return m.teams[teamID], nil
}

func TestNotifier_DetailedWakesImmediately(t *testing.T) {
	s := &mockNotifierStore{
		tasks: map[string]*store.TeamTask{
			"task-1": {ID: "task-1", TeamID: "team-1", BatchID: "batch-1", Status: "completed"},
		},
		teams: map[string]*store.Team{
			"team-1": {ID: "team-1", LeadID: "lead-agent", Settings: `{"chat_verbosity":"detailed"}`},
		},
	}

	var mu sync.Mutex
	var woken []string

	exec := &TeamExecutor{
		store:   s,
		inboxes: make(map[string]*TeamInbox),
	}

	// Pre-populate the inbox (simulating what executor.execute normally does).
	inbox := exec.GetInbox("team-1")
	inbox.Push(TaskCompletion{
		TaskID:   "task-1",
		AgentKey: "researcher",
		Subject:  "Research task",
		Result:   "Research result",
		Status:   "completed",
		BatchID:  "batch-1",
	})

	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		mu.Lock()
		defer mu.Unlock()
		woken = append(woken, leadAgentID)
	})

	// Track the batch creation.
	notifier.HandleEvent(agent.Event{
		Type:    agent.EventTeamTaskCreated,
		AgentID: "team-1",
		Text:    "task-1",
	})

	// Signal completion.
	notifier.HandleEvent(agent.Event{
		Type:    agent.EventTeamTaskCompleted,
		AgentID: "team-1",
		Text:    "task-1",
	})

	mu.Lock()
	defer mu.Unlock()
	if len(woken) != 1 || woken[0] != "lead-agent" {
		t.Fatalf("expected lead-agent to be woken once, got: %v", woken)
	}
}

func TestNotifier_ProgressiveWaitsForBatch(t *testing.T) {
	s := &mockNotifierStore{
		tasks: map[string]*store.TeamTask{
			"task-1": {ID: "task-1", TeamID: "team-1", BatchID: "batch-1", Status: "completed"},
			"task-2": {ID: "task-2", TeamID: "team-1", BatchID: "batch-1", Status: "pending"},
		},
		teams: map[string]*store.Team{
			"team-1": {ID: "team-1", LeadID: "lead-agent", Settings: `{"chat_verbosity":"progressive"}`},
		},
	}

	var mu sync.Mutex
	wakeCount := 0

	exec := &TeamExecutor{
		store:   s,
		inboxes: make(map[string]*TeamInbox),
	}

	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		mu.Lock()
		defer mu.Unlock()
		wakeCount++
	})

	// Track two tasks in batch.
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-1", Text: "task-1"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-1", Text: "task-2"})

	// First task completes — push to inbox.
	inbox := exec.GetInbox("team-1")
	inbox.Push(TaskCompletion{TaskID: "task-1", Status: "completed", BatchID: "batch-1"})

	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-1", Text: "task-1"})

	mu.Lock()
	if wakeCount != 0 {
		t.Fatalf("expected no wake yet (batch incomplete), got %d", wakeCount)
	}
	mu.Unlock()

	// Second task completes.
	s.tasks["task-2"] = &store.TeamTask{ID: "task-2", TeamID: "team-1", BatchID: "batch-1", Status: "completed"}
	inbox.Push(TaskCompletion{TaskID: "task-2", Status: "completed", BatchID: "batch-1"})

	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-1", Text: "task-2"})

	mu.Lock()
	defer mu.Unlock()
	if wakeCount != 1 {
		t.Fatalf("expected 1 wake after batch complete, got %d", wakeCount)
	}
}

func TestNotifier_NoBatchWakesImmediately(t *testing.T) {
	s := &mockNotifierStore{
		tasks: map[string]*store.TeamTask{
			"task-1": {ID: "task-1", TeamID: "team-1", BatchID: "", Status: "completed"},
		},
		teams: map[string]*store.Team{
			"team-1": {ID: "team-1", LeadID: "lead-agent", Settings: `{}`},
		},
	}

	wakeCount := 0
	exec := &TeamExecutor{store: s, inboxes: make(map[string]*TeamInbox)}
	inbox := exec.GetInbox("team-1")
	inbox.Push(TaskCompletion{TaskID: "task-1", Status: "completed"})

	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		wakeCount++
	})

	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-1", Text: "task-1"})

	if wakeCount != 1 {
		t.Fatalf("expected 1 wake for no-batch task, got %d", wakeCount)
	}
}

func TestBuildWakeupMessage(t *testing.T) {
	n := &TeamProgressNotifier{}
	msg := n.buildWakeupMessage([]TaskCompletion{
		{TaskID: "t1", AgentKey: "researcher", Status: "completed", Result: "findings here"},
		{TaskID: "t2", AgentKey: "writer", Status: "failed", Error: "timeout"},
	})

	if msg == "" {
		t.Fatal("expected non-empty wakeup message")
	}
	if !contains(msg, "<team-task-result") {
		t.Fatal("expected team-task-result tags")
	}
	if !contains(msg, "FAILED") {
		t.Fatal("expected FAILED marker for failed task")
	}
	if !contains(msg, "Synthesize") {
		t.Fatal("expected synthesis instruction")
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
