package team

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
)

// TestIntegration_FullTaskFlow exercises the full notifier + wakeup flow:
// 1. Create tasks (simulating Dispatch's event emissions)
// 2. Simulate completions pushing to inbox
// 3. Emit completion events
// 4. Verify notifier wakes the lead with correct message
func TestIntegration_FullTaskFlow(t *testing.T) {
	s := &mockIntegrationStore{
		teams: map[string]*store.Team{
			"team-alpha": {
				ID:       "team-alpha",
				Name:     "Alpha",
				LeadID:   "lead-1",
				Settings: `{"chat_verbosity":"progressive"}`,
			},
		},
		tasks: map[string]*store.TeamTask{
			"task-1": {ID: "task-1", TeamID: "team-alpha", Title: "Research X", AssignedTo: "researcher", BatchID: "batch-001", Status: "pending"},
			"task-2": {ID: "task-2", TeamID: "team-alpha", Title: "Write summary", AssignedTo: "writer", BatchID: "batch-001", Status: "pending"},
		},
	}

	var mu sync.Mutex
	var wakeMessages []string
	var wakeAgents []string

	exec := &TeamExecutor{
		store:   s,
		inboxes: make(map[string]*TeamInbox),
	}

	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		mu.Lock()
		defer mu.Unlock()
		wakeAgents = append(wakeAgents, leadAgentID)
		wakeMessages = append(wakeMessages, msg)
	})
	exec.SetNotifier(notifier)

	// Step 1: Emit creation events (simulating Dispatch).
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-alpha", Text: "task-1"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-alpha", Text: "task-2"})

	inbox := exec.GetInbox("team-alpha")

	// Step 2: First task completes — push to inbox, emit event.
	s.mu.Lock()
	s.tasks["task-1"].Status = "completed"
	s.tasks["task-1"].Result = "Found important findings about X"
	s.mu.Unlock()
	inbox.Push(TaskCompletion{TaskID: "task-1", AgentKey: "researcher", Subject: "Research X", Result: "Found important findings about X", Status: "completed", BatchID: "batch-001"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-alpha", Text: "task-1"})

	// Progressive mode: should NOT wake yet (batch incomplete).
	mu.Lock()
	if len(wakeAgents) != 0 {
		t.Fatalf("expected no wakeup yet, got %d", len(wakeAgents))
	}
	mu.Unlock()

	// Step 3: Second task completes.
	s.mu.Lock()
	s.tasks["task-2"].Status = "completed"
	s.tasks["task-2"].Result = "Summary written based on research"
	s.mu.Unlock()
	inbox.Push(TaskCompletion{TaskID: "task-2", AgentKey: "writer", Subject: "Write summary", Result: "Summary written based on research", Status: "completed", BatchID: "batch-001"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-alpha", Text: "task-2"})

	// Batch complete: should wake the lead.
	mu.Lock()
	defer mu.Unlock()
	if len(wakeAgents) != 1 {
		t.Fatalf("expected 1 wakeup, got %d", len(wakeAgents))
	}
	if wakeAgents[0] != "lead-1" {
		t.Fatalf("expected lead-1, got %s", wakeAgents[0])
	}
	msg := wakeMessages[0]
	if !strings.Contains(msg, "team-task-result") {
		t.Fatal("expected <team-task-result> tags in wakeup message")
	}
	if !strings.Contains(msg, "researcher") {
		t.Fatal("expected researcher agent attribution in message")
	}
	if !strings.Contains(msg, "writer") {
		t.Fatal("expected writer agent attribution in message")
	}
	if !strings.Contains(msg, "Synthesize") {
		t.Fatal("expected synthesis instruction in message")
	}
}

// TestIntegration_DetailedWakesPerTask verifies detailed mode wakes after each task.
func TestIntegration_DetailedWakesPerTask(t *testing.T) {
	s := &mockIntegrationStore{
		teams: map[string]*store.Team{
			"team-beta": {
				ID:       "team-beta",
				Name:     "Beta",
				LeadID:   "lead-2",
				Settings: `{"chat_verbosity":"detailed"}`,
			},
		},
		tasks: map[string]*store.TeamTask{
			"task-a": {ID: "task-a", TeamID: "team-beta", Title: "Task A", AssignedTo: "worker-1", BatchID: "b1", Status: "completed"},
			"task-b": {ID: "task-b", TeamID: "team-beta", Title: "Task B", AssignedTo: "worker-2", BatchID: "b1", Status: "pending"},
		},
	}

	wakeCount := 0
	exec := &TeamExecutor{store: s, inboxes: make(map[string]*TeamInbox)}
	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		wakeCount++
	})

	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-beta", Text: "task-a"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-beta", Text: "task-b"})

	inbox := exec.GetInbox("team-beta")
	inbox.Push(TaskCompletion{TaskID: "task-a", Status: "completed", BatchID: "b1", Result: "done"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-beta", Text: "task-a"})

	// Detailed mode: should wake immediately.
	if wakeCount != 1 {
		t.Fatalf("expected 1 immediate wakeup in detailed mode, got %d", wakeCount)
	}
}

// TestIntegration_FailedTasksWakeLead verifies failed tasks also trigger wakeup.
func TestIntegration_FailedTasksWakeLead(t *testing.T) {
	s := &mockIntegrationStore{
		teams: map[string]*store.Team{
			"team-gamma": {
				ID:       "team-gamma",
				Name:     "Gamma",
				LeadID:   "lead-3",
				Settings: `{}`,
			},
		},
		tasks: map[string]*store.TeamTask{
			"task-f": {ID: "task-f", TeamID: "team-gamma", Title: "Failing task", Status: "failed"},
		},
	}

	var wakeMsg string
	exec := &TeamExecutor{store: s, inboxes: make(map[string]*TeamInbox)}
	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		wakeMsg = msg
	})

	inbox := exec.GetInbox("team-gamma")
	inbox.Push(TaskCompletion{TaskID: "task-f", Status: "failed", Error: "timeout after 10m"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskFailed, AgentID: "team-gamma", Text: "task-f"})

	if wakeMsg == "" {
		t.Fatal("expected wakeup on failed task")
	}
	if !strings.Contains(wakeMsg, "FAILED") {
		t.Fatal("expected FAILED marker in wakeup message")
	}
}

// --- Mock store for integration test ---

type mockIntegrationStore struct {
	store.Store
	mu    sync.Mutex
	teams map[string]*store.Team
	tasks map[string]*store.TeamTask
}

func (m *mockIntegrationStore) GetTeam(ctx context.Context, teamID string) (*store.Team, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.teams[teamID], nil
}

func (m *mockIntegrationStore) GetTask(ctx context.Context, taskID string) (*store.TeamTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[taskID]
	if t == nil {
		return nil, nil
	}
	cp := *t
	return &cp, nil
}

func (m *mockIntegrationStore) CreateSessionWithKind(ctx context.Context, channel, chatID, agentID, kind string) (*store.Session, error) {
	return &store.Session{ID: "mock-session-" + agentID, CreatedAt: time.Now()}, nil
}
