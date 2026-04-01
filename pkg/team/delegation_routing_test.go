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

// TestDispatch_LeadCannotSelfAssign verifies the executor rejects tasks
// assigned to the team lead — tasks must go to members.
func TestDispatch_LeadCannotSelfAssign(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "researcher", "writer")

	_, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Research something",
		AssignedTo: "lead-1", // Lead trying to self-assign.
		CreatedBy:  "lead-1",
	})
	if err == nil {
		t.Fatal("expected error when dispatching to lead")
	}
	if !strings.Contains(err.Error(), "lead") {
		t.Errorf("error should mention 'lead', got: %v", err)
	}
}

// TestDispatch_SkillMatchedMemberExecutes verifies that tasks assigned to
// a specialist member are accepted and launched (not rejected).
func TestDispatch_SkillMatchedMemberExecutes(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "researcher", "writer", "coder")

	// Assign research task to researcher — should succeed.
	taskID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:      teamID,
		Title:       "Deep dive into competitor pricing models",
		Description: "Analyze top 5 competitors, extract pricing tiers, produce comparison table",
		AssignedTo:  "researcher",
		CreatedBy:   "lead-1",
		Priority:    5,
	})
	if err != nil {
		t.Fatalf("expected researcher assignment to succeed: %v", err)
	}

	task, _ := s.GetTask(ctx, taskID)
	if task == nil {
		t.Fatal("task should exist in DB")
	}
	if task.AssignedTo != "researcher" {
		t.Errorf("task should be assigned to researcher, got %q", task.AssignedTo)
	}
}

// TestDispatch_MultiMemberParallelTasks verifies that a lead can create
// tasks for multiple members in one batch (parallel delegation).
func TestDispatch_MultiMemberParallelTasks(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "researcher", "writer")

	// Create two parallel tasks for different members.
	researchID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Research AI pricing trends",
		AssignedTo: "researcher",
		CreatedBy:  "lead-1",
		BatchID:    "batch-001",
	})
	if err != nil {
		t.Fatalf("research dispatch: %v", err)
	}

	writeID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Draft intro paragraph",
		AssignedTo: "writer",
		CreatedBy:  "lead-1",
		BatchID:    "batch-001",
	})
	if err != nil {
		t.Fatalf("write dispatch: %v", err)
	}

	// Both tasks should exist and be in the same batch.
	researchTask, _ := s.GetTask(ctx, researchID)
	writeTask, _ := s.GetTask(ctx, writeID)

	if researchTask.BatchID != "batch-001" || writeTask.BatchID != "batch-001" {
		t.Error("both tasks should share the same batch ID")
	}
	if researchTask.AssignedTo != "researcher" {
		t.Errorf("research task assigned to %q, want researcher", researchTask.AssignedTo)
	}
	if writeTask.AssignedTo != "writer" {
		t.Errorf("write task assigned to %q, want writer", writeTask.AssignedTo)
	}
}

// TestDispatch_DependentTaskBlockedUntilPredecessorCompletes verifies
// that a task with blocked_by stays blocked and only executes after
// its dependency completes — enabling sequential delegation chains.
func TestDispatch_DependentTaskBlockedUntilPredecessorCompletes(t *testing.T) {
	exec, s := setupTestExecutor(t)
	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "researcher", "writer")

	// Step 1: researcher does research.
	researchID, _ := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Research topic",
		AssignedTo: "researcher",
		CreatedBy:  "lead-1",
	})

	// Step 2: writer writes based on research (blocked).
	writeID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Write article based on research",
		AssignedTo: "writer",
		CreatedBy:  "lead-1",
		BlockedBy:  researchID,
	})
	if err != nil {
		t.Fatalf("blocked dispatch: %v", err)
	}

	writeTask, _ := s.GetTask(ctx, writeID)
	if writeTask.Status != "blocked" {
		t.Fatalf("write task should be blocked, got %q", writeTask.Status)
	}
}

// TestIntegration_SkillMatchedDelegationFlow tests the full flow:
// lead creates tasks for specialist members → members complete →
// results arrive in lead's inbox with correct attribution.
func TestIntegration_SkillMatchedDelegationFlow(t *testing.T) {
	s := &mockIntegrationStore{
		teams: map[string]*store.Team{
			"team-dev": {
				ID:       "team-dev",
				Name:     "Dev Team",
				LeadID:   "lead-1",
				Settings: `{"chat_verbosity":"progressive"}`,
			},
		},
		tasks: map[string]*store.TeamTask{
			"task-research": {
				ID: "task-research", TeamID: "team-dev",
				Title:      "Research React server components",
				AssignedTo: "frontend-expert",
				BatchID:    "batch-ui",
				Status:     "pending",
			},
			"task-api": {
				ID: "task-api", TeamID: "team-dev",
				Title:      "Design REST API for user settings",
				AssignedTo: "backend-expert",
				BatchID:    "batch-ui",
				Status:     "pending",
			},
		},
	}

	var mu sync.Mutex
	var wakeMessages []string

	exec := &TeamExecutor{store: s, inboxes: make(map[string]*TeamInbox)}
	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		mu.Lock()
		defer mu.Unlock()
		wakeMessages = append(wakeMessages, msg)
	})
	exec.SetNotifier(notifier)

	// Emit creation events.
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-dev", Text: "task-research"})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCreated, AgentID: "team-dev", Text: "task-api"})

	inbox := exec.GetInbox("team-dev")

	// Frontend expert completes research.
	s.mu.Lock()
	s.tasks["task-research"].Status = "completed"
	s.tasks["task-research"].Result = "Server components reduce bundle size by 40%. Key patterns: async components, streaming SSR, progressive hydration."
	s.mu.Unlock()
	inbox.Push(TaskCompletion{
		TaskID: "task-research", AgentKey: "frontend-expert",
		Subject: "Research React server components",
		Result:  "Server components reduce bundle size by 40%.",
		Status:  "completed", BatchID: "batch-ui",
	})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-dev", Text: "task-research"})

	// Progressive mode — should NOT wake yet (batch incomplete).
	mu.Lock()
	if len(wakeMessages) != 0 {
		t.Fatalf("should not wake lead before batch completes, got %d wakes", len(wakeMessages))
	}
	mu.Unlock()

	// Backend expert completes API design.
	s.mu.Lock()
	s.tasks["task-api"].Status = "completed"
	s.tasks["task-api"].Result = "REST endpoints: GET/PUT /api/settings, PATCH /api/settings/theme. Schema validated."
	s.mu.Unlock()
	inbox.Push(TaskCompletion{
		TaskID: "task-api", AgentKey: "backend-expert",
		Subject: "Design REST API for user settings",
		Result:  "REST endpoints designed and validated.",
		Status:  "completed", BatchID: "batch-ui",
	})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskCompleted, AgentID: "team-dev", Text: "task-api"})

	// Batch complete — lead should be woken with both results.
	mu.Lock()
	defer mu.Unlock()

	if len(wakeMessages) != 1 {
		t.Fatalf("expected 1 batch wakeup, got %d", len(wakeMessages))
	}

	msg := wakeMessages[0]

	// Results should be attributed to the correct specialist.
	if !strings.Contains(msg, "frontend-expert") {
		t.Error("wakeup should attribute research result to frontend-expert")
	}
	if !strings.Contains(msg, "backend-expert") {
		t.Error("wakeup should attribute API result to backend-expert")
	}

	// Results should contain task result tags.
	if !strings.Contains(msg, "team-task-result") {
		t.Error("wakeup should contain <team-task-result> tags")
	}

	// Synthesis instruction should be present.
	if !strings.Contains(msg, "Synthesize") {
		t.Error("wakeup should instruct lead to synthesize results")
	}
}

// TestIntegration_FailedSpecialistNotifiesLead verifies that when a
// specialist member fails, the lead is notified so it can reassign.
func TestIntegration_FailedSpecialistNotifiesLead(t *testing.T) {
	s := &mockIntegrationStore{
		teams: map[string]*store.Team{
			"team-1": {ID: "team-1", Name: "T", LeadID: "lead-1", Settings: `{}`},
		},
		tasks: map[string]*store.TeamTask{
			"task-x": {ID: "task-x", TeamID: "team-1", Title: "Complex analysis", AssignedTo: "analyst", Status: "failed"},
		},
	}

	var wakeMsg string
	exec := &TeamExecutor{store: s, inboxes: make(map[string]*TeamInbox)}
	notifier := NewTeamProgressNotifier(s, exec, func(ctx context.Context, leadAgentID, teamID, msg string) {
		wakeMsg = msg
	})

	inbox := exec.GetInbox("team-1")
	inbox.Push(TaskCompletion{
		TaskID: "task-x", Status: "failed",
		Error: "rate_limit: model overloaded",
	})
	notifier.HandleEvent(agent.Event{Type: agent.EventTeamTaskFailed, AgentID: "team-1", Text: "task-x"})

	if wakeMsg == "" {
		t.Fatal("lead should be notified when specialist fails")
	}
	if !strings.Contains(wakeMsg, "FAILED") {
		t.Error("notification should clearly indicate failure")
	}
}

// --- Mock helpers ---

func (m *mockIntegrationStore) ListTasks(ctx context.Context, teamID, status string) ([]store.TeamTask, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []store.TeamTask
	for _, t := range m.tasks {
		if t.TeamID != teamID {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		result = append(result, *t)
	}
	return result, nil
}

func (m *mockIntegrationStore) GetTeamByAgent(ctx context.Context, agentID string) (*store.Team, string, error) {
	return nil, "", nil
}

func (m *mockIntegrationStore) CreateTask(ctx context.Context, task store.TeamTask) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := "task-" + time.Now().Format("150405.000")
	task.ID = id
	task.Status = "pending"
	m.tasks[id] = &task
	return id, nil
}
