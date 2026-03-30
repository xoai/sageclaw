package sqlite

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/store"
)

// createTestTeam inserts a team with a lead and optional members.
func createTestTeam(t *testing.T, s *Store, leadID string, memberIDs ...string) string {
	t.Helper()
	ctx := context.Background()
	teamID := newID()
	config := `{"members":[]}`
	if len(memberIDs) > 0 {
		config = `{"members":["` + memberIDs[0] + `"`
		for _, m := range memberIDs[1:] {
			config += `,"` + m + `"`
		}
		config += `]}`
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO teams (id, name, lead_id, config, description, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', 'active', '{}', datetime('now'), datetime('now'))`,
		teamID, "Test Team", leadID, config)
	if err != nil {
		t.Fatalf("creating test team: %v", err)
	}
	return teamID
}

func TestGetTeam(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	teamID := createTestTeam(t, s, "lead-agent")

	team, err := s.GetTeam(ctx, teamID)
	if err != nil {
		t.Fatalf("GetTeam: %v", err)
	}
	if team == nil {
		t.Fatal("expected team, got nil")
	}
	if team.Name != "Test Team" {
		t.Fatalf("expected name 'Test Team', got %q", team.Name)
	}
	if team.LeadID != "lead-agent" {
		t.Fatalf("expected lead 'lead-agent', got %q", team.LeadID)
	}

	// Non-existent team.
	team, err = s.GetTeam(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTeam nonexistent: %v", err)
	}
	if team != nil {
		t.Fatal("expected nil for nonexistent team")
	}
}

func TestGetTeamByAgent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	createTestTeam(t, s, "lead-1", "member-a", "member-b")

	// Lead lookup.
	team, role, err := s.GetTeamByAgent(ctx, "lead-1")
	if err != nil {
		t.Fatalf("GetTeamByAgent lead: %v", err)
	}
	if team == nil || role != "lead" {
		t.Fatalf("expected lead role, got team=%v role=%q", team, role)
	}

	// Member lookup.
	team, role, err = s.GetTeamByAgent(ctx, "member-a")
	if err != nil {
		t.Fatalf("GetTeamByAgent member: %v", err)
	}
	if team == nil || role != "member" {
		t.Fatalf("expected member role, got team=%v role=%q", team, role)
	}

	// Unknown agent.
	team, role, err = s.GetTeamByAgent(ctx, "unknown")
	if err != nil {
		t.Fatalf("GetTeamByAgent unknown: %v", err)
	}
	if team != nil {
		t.Fatalf("expected nil for unknown agent, got %v", team)
	}
}

func TestListTeamMembers(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	teamID := createTestTeam(t, s, "lead-1", "member-a", "member-b")

	members, err := s.ListTeamMembers(ctx, teamID)
	if err != nil {
		t.Fatalf("ListTeamMembers: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members (lead + 2), got %d", len(members))
	}
	if members[0].Role != "lead" {
		t.Fatalf("expected first member to be lead, got %q", members[0].Role)
	}
}

func TestCreateAndGetTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, err := s.CreateTask(ctx, store.TeamTask{
		TeamID:      teamID,
		Title:       "Research topic",
		Description: "Find key papers",
		AssignedTo:  "member-a",
		CreatedBy:   "lead-1",
		Priority:    10,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if task == nil {
		t.Fatal("expected task, got nil")
	}
	if task.Title != "Research topic" {
		t.Fatalf("expected title 'Research topic', got %q", task.Title)
	}
	if task.Status != "pending" {
		t.Fatalf("expected status 'pending', got %q", task.Status)
	}
	if task.Priority != 10 {
		t.Fatalf("expected priority 10, got %d", task.Priority)
	}
	if task.TaskNumber != 1 {
		t.Fatalf("expected task_number 1, got %d", task.TaskNumber)
	}
	if task.Identifier != "TSK-1" {
		t.Fatalf("expected identifier 'TSK-1', got %q", task.Identifier)
	}
	if task.MaxRetries != 1 {
		t.Fatalf("expected max_retries 1, got %d", task.MaxRetries)
	}
}

func TestTaskNumberAutoIncrement(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	id1, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task 1", CreatedBy: "lead-1"})
	id2, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task 2", CreatedBy: "lead-1"})
	id3, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task 3", CreatedBy: "lead-1"})

	t1, _ := s.GetTask(ctx, id1)
	t2, _ := s.GetTask(ctx, id2)
	t3, _ := s.GetTask(ctx, id3)

	if t1.TaskNumber != 1 || t2.TaskNumber != 2 || t3.TaskNumber != 3 {
		t.Fatalf("expected task numbers 1,2,3 got %d,%d,%d", t1.TaskNumber, t2.TaskNumber, t3.TaskNumber)
	}
	if t3.Identifier != "TSK-3" {
		t.Fatalf("expected identifier TSK-3, got %q", t3.Identifier)
	}
}

func TestClaimTaskAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Race task", CreatedBy: "lead-1",
	})

	// First claim succeeds.
	if err := s.ClaimTask(ctx, taskID, "agent-a"); err != nil {
		t.Fatalf("first ClaimTask should succeed: %v", err)
	}

	// Second claim fails (task already in_progress).
	if err := s.ClaimTask(ctx, taskID, "agent-b"); err == nil {
		t.Fatal("second ClaimTask should fail on already-claimed task")
	}

	task, err := s.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask after claim: %v", err)
	}
	if task == nil {
		t.Fatal("expected task after claim, got nil")
	}
	if task.Status != "in_progress" {
		t.Fatalf("expected status in_progress, got %q", task.Status)
	}
	if task.AssignedTo != "agent-a" {
		t.Fatalf("expected assigned_to agent-a, got %q", task.AssignedTo)
	}
}

func TestCompleteTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Complete me", CreatedBy: "lead-1",
	})

	// Can't complete a pending task directly.
	if err := s.CompleteTask(ctx, taskID, "result"); err == nil {
		t.Fatal("expected error completing pending task")
	}

	// Claim then complete.
	s.ClaimTask(ctx, taskID, "agent-a")
	if err := s.CompleteTask(ctx, taskID, "done!"); err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}

	task, _ := s.GetTask(ctx, taskID)
	if task.Status != "completed" {
		t.Fatalf("expected completed, got %q", task.Status)
	}
	if task.Result != "done!" {
		t.Fatalf("expected result 'done!', got %q", task.Result)
	}
	if task.CompletedAt == nil {
		t.Fatal("expected CompletedAt to be set")
	}
}

func TestCompleteTaskRequireApproval(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Review me", CreatedBy: "lead-1",
		RequireApproval: true,
	})

	s.ClaimTask(ctx, taskID, "agent-a")
	s.CompleteTask(ctx, taskID, "needs review")

	task, _ := s.GetTask(ctx, taskID)
	if task.Status != "in_review" {
		t.Fatalf("expected in_review for require_approval task, got %q", task.Status)
	}
}

func TestCancelTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Cancel me", CreatedBy: "lead-1",
	})
	if err := s.CancelTask(ctx, taskID); err != nil {
		t.Fatalf("CancelTask: %v", err)
	}
	task, _ := s.GetTask(ctx, taskID)
	if task.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %q", task.Status)
	}
}

func TestDependencyChain(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	// A → B → C (C blocked by B, B blocked by A)
	idA, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task A", CreatedBy: "lead-1"})
	idB, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task B", CreatedBy: "lead-1", BlockedBy: idA})
	idC, _ := s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Task C", CreatedBy: "lead-1", BlockedBy: idB})

	// B and C should be blocked.
	taskB, _ := s.GetTask(ctx, idB)
	taskC, _ := s.GetTask(ctx, idC)
	if taskB.Status != "blocked" {
		t.Fatalf("expected B blocked, got %q", taskB.Status)
	}
	if taskC.Status != "blocked" {
		t.Fatalf("expected C blocked, got %q", taskC.Status)
	}

	// Complete A → B should unblock.
	s.ClaimTask(ctx, idA, "agent-a")
	s.CompleteTask(ctx, idA, "done")

	unblocked, err := s.UnblockTasks(ctx, idA)
	if err != nil {
		t.Fatalf("UnblockTasks: %v", err)
	}
	if len(unblocked) != 1 || unblocked[0].ID != idB {
		t.Fatalf("expected B to unblock, got %v", unblocked)
	}

	// C should still be blocked (B not completed yet).
	taskC, _ = s.GetTask(ctx, idC)
	if taskC.Status != "blocked" {
		t.Fatalf("expected C still blocked, got %q", taskC.Status)
	}

	// Complete B → C should unblock.
	s.ClaimTask(ctx, idB, "agent-b")
	s.CompleteTask(ctx, idB, "done")
	unblocked, _ = s.UnblockTasks(ctx, idB)
	if len(unblocked) != 1 || unblocked[0].ID != idC {
		t.Fatalf("expected C to unblock, got %v", unblocked)
	}
}

func TestSubtasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	parentID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Parent task", CreatedBy: "lead-1",
	})

	childID1, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Child 1", CreatedBy: "lead-1", ParentID: parentID,
	})
	childID2, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Child 2", CreatedBy: "lead-1", ParentID: parentID,
	})

	children, err := s.GetTasksByParent(ctx, parentID)
	if err != nil {
		t.Fatalf("GetTasksByParent: %v", err)
	}
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(children))
	}
	// Verify IDs match (order by task_number).
	ids := map[string]bool{childID1: true, childID2: true}
	for _, c := range children {
		if !ids[c.ID] {
			t.Fatalf("unexpected child ID %q", c.ID)
		}
	}
}

func TestRetryTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Retry me", CreatedBy: "lead-1", MaxRetries: 2,
	})

	// Claim and fail.
	s.ClaimTask(ctx, taskID, "agent-a")
	s.UpdateTask(ctx, taskID, map[string]any{"status": "failed", "error_message": "timeout"})

	// Retry should work.
	if err := s.RetryTask(ctx, taskID); err != nil {
		t.Fatalf("RetryTask: %v", err)
	}
	task, _ := s.GetTask(ctx, taskID)
	if task.Status != "pending" {
		t.Fatalf("expected pending after retry, got %q", task.Status)
	}
	if task.RetryCount != 1 {
		t.Fatalf("expected retry_count 1, got %d", task.RetryCount)
	}

	// Second failure + retry (retry_count=1, max_retries=2 → still allowed).
	s.ClaimTask(ctx, taskID, "agent-a")
	s.UpdateTask(ctx, taskID, map[string]any{"status": "failed"})
	if err := s.RetryTask(ctx, taskID); err != nil {
		t.Fatalf("second RetryTask should succeed (count=1 < max=2): %v", err)
	}
	task, _ = s.GetTask(ctx, taskID)
	if task.RetryCount != 2 {
		t.Fatalf("expected retry_count 2, got %d", task.RetryCount)
	}

	// Third failure + retry should fail (retry_count=2 >= max_retries=2).
	s.ClaimTask(ctx, taskID, "agent-a")
	s.UpdateTask(ctx, taskID, map[string]any{"status": "failed"})
	if err := s.RetryTask(ctx, taskID); err == nil {
		t.Fatal("expected error: max retries reached")
	}
}

func TestProgressUpdate(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Progress task", CreatedBy: "lead-1",
	})

	s.UpdateTaskProgress(ctx, taskID, 50, "halfway there")

	task, _ := s.GetTask(ctx, taskID)
	if task.ProgressPercent != 50 {
		t.Fatalf("expected progress 50, got %d", task.ProgressPercent)
	}

	// Check comment was created.
	comments, _ := s.ListComments(ctx, taskID)
	if len(comments) != 1 {
		t.Fatalf("expected 1 progress comment, got %d", len(comments))
	}
	if comments[0].CommentType != "status" {
		t.Fatalf("expected comment type 'status', got %q", comments[0].CommentType)
	}
}

func TestCommentsCRUD(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Commented task", CreatedBy: "lead-1",
	})

	cid, err := s.CreateComment(ctx, store.TeamTaskComment{
		TaskID:  taskID,
		AgentID: "agent-a",
		Content: "Looking into this",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if cid == "" {
		t.Fatal("expected comment ID")
	}

	s.CreateComment(ctx, store.TeamTaskComment{
		TaskID:  taskID,
		AgentID: "agent-b",
		Content: "Found the issue",
	})

	comments, err := s.ListComments(ctx, taskID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}
}

func TestSearchTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Research AI papers", CreatedBy: "lead-1"})
	s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Write summary", CreatedBy: "lead-1"})
	s.CreateTask(ctx, store.TeamTask{TeamID: teamID, Title: "Review AI findings", CreatedBy: "lead-1"})

	results, err := s.SearchTasks(ctx, teamID, "AI")
	if err != nil {
		t.Fatalf("SearchTasks: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'AI', got %d", len(results))
	}
}

func TestUpdateTeam(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	err := s.UpdateTeam(ctx, teamID, map[string]any{
		"description": "Updated description",
		"settings":    `{"max_concurrent": 5}`,
	})
	if err != nil {
		t.Fatalf("UpdateTeam: %v", err)
	}

	team, _ := s.GetTeam(ctx, teamID)
	if team.Description != "Updated description" {
		t.Fatalf("expected updated description, got %q", team.Description)
	}
	if team.Settings != `{"max_concurrent": 5}` {
		t.Fatalf("expected updated settings, got %q", team.Settings)
	}
}

func TestBlockedByCreatesBlockedStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	teamID := createTestTeam(t, s, "lead-1")

	idA, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Blocker", CreatedBy: "lead-1",
	})
	idB, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Blocked", CreatedBy: "lead-1", BlockedBy: idA,
	})

	task, _ := s.GetTask(ctx, idB)
	if task.Status != "blocked" {
		t.Fatalf("expected blocked status when blocked_by is set, got %q", task.Status)
	}

	// GetBlockedTasks should return it.
	blocked, err := s.GetBlockedTasks(ctx, teamID)
	if err != nil {
		t.Fatalf("GetBlockedTasks: %v", err)
	}
	if len(blocked) != 1 || blocked[0].ID != idB {
		t.Fatalf("expected 1 blocked task, got %d", len(blocked))
	}
}
