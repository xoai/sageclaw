package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// mockTeamExecutor implements TeamTaskExecutor for testing.
type mockTeamExecutor struct {
	store store.Store
}

func (m *mockTeamExecutor) Dispatch(ctx context.Context, task store.TeamTask) (string, error) {
	// Simple dispatch: just insert the task into the store.
	return m.store.CreateTask(ctx, task)
}

func (m *mockTeamExecutor) LaunchIfReady(ctx context.Context, task store.TeamTask) {
	// No-op in tests.
}

func (m *mockTeamExecutor) EmitTaskFailed(ctx context.Context, teamID, taskID string) {
	// No-op in tests.
}

// setupTeamTasks creates a test registry with team_tasks tool, a team, and agents.
func setupTeamTasks(t *testing.T) (*Registry, *sqlite.Store, string) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Create team with lead and member.
	teamID := insertTestTeam(t, s, "lead-agent", "member-a", "member-b")

	executor := &mockTeamExecutor{store: s}
	reg := NewRegistry()
	RegisterTeamTasks(reg, s, executor)

	return reg, s, teamID
}

// insertTestTeam creates a team in the DB for testing.
func insertTestTeam(t *testing.T, s *sqlite.Store, leadID string, members ...string) string {
	t.Helper()
	ctx := context.Background()
	teamID := "test-team-id"

	membersJSON := `[]`
	if len(members) > 0 {
		parts := make([]string, len(members))
		for i, m := range members {
			parts[i] = `"` + m + `"`
		}
		membersJSON = `[` + strings.Join(parts, ",") + `]`
	}

	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO teams (id, name, lead_id, config, description, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', 'active', '{}', datetime('now'), datetime('now'))`,
		teamID, "Test Team", leadID, `{"members":`+membersJSON+`}`)
	if err != nil {
		t.Fatalf("creating test team: %v", err)
	}
	return teamID
}

func ctxWithAgent(agentID string) context.Context {
	return context.WithValue(context.Background(), agentIDKey{}, agentID)
}

func TestTeamTasks_Create(t *testing.T) {
	reg, _, teamID := setupTeamTasks(t)

	// Lead creates a task.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Research topic","description":"Find relevant papers","assignee":"member-a","priority":5}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.IsError {
		t.Fatalf("create returned error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Task created") {
		t.Fatalf("unexpected result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "TSK-") {
		t.Fatalf("expected TSK identifier in result: %s", result.Content)
	}
}

func TestTeamTasks_Create_MemberDenied(t *testing.T) {
	reg, _, teamID := setupTeamTasks(t)

	// Member tries to create a task — should be denied.
	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"My task"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for member creating task, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "lead role") {
		t.Fatalf("expected role error, got: %s", result.Content)
	}
}

func TestTeamTasks_ListAndGet(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)
	ctx := ctxWithAgent("lead-agent")

	// Create a task.
	s.CreateTask(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Test task",
		Status:     "pending",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
		Identifier: "TSK-1",
		TaskNumber: 1,
	})

	// List all tasks.
	result, err := reg.Execute(ctx, "team_tasks",
		json.RawMessage(`{"action":"list","team_id":"`+teamID+`"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if result.IsError {
		t.Fatalf("list error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Test task") {
		t.Fatalf("expected task in list: %s", result.Content)
	}
	if !strings.Contains(result.Content, "member-a") {
		t.Fatalf("expected assignee in list: %s", result.Content)
	}

	// List with status filter.
	result, err = reg.Execute(ctx, "team_tasks",
		json.RawMessage(`{"action":"list","team_id":"`+teamID+`","status":"completed"}`))
	if err != nil {
		t.Fatalf("list filtered: %v", err)
	}
	if !strings.Contains(result.Content, "No completed tasks") {
		t.Fatalf("expected no completed tasks, got: %s", result.Content)
	}
}

func TestTeamTasks_Complete_ByMember(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	// Create and claim a task.
	taskID, err := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Work item",
		Status:     "pending",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	s.ClaimTask(context.Background(), taskID, "member-a")

	// Member completes the task.
	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"complete","task_id":"`+taskID+`","result":"Done with findings"}`))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if result.IsError {
		t.Fatalf("complete error: %s", result.Content)
	}

	// Verify task is completed.
	task, _ := s.GetTask(context.Background(), taskID)
	if task.Status != "completed" {
		t.Fatalf("expected completed, got %s", task.Status)
	}
	if task.Result != "Done with findings" {
		t.Fatalf("expected result, got %q", task.Result)
	}
}

func TestTeamTasks_Complete_WrongAgent(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	taskID, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Work",
		Status:     "in_progress",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
	})

	// Member-b tries to complete member-a's task.
	result, err := reg.Execute(ctxWithAgent("member-b"), "team_tasks",
		json.RawMessage(`{"action":"complete","task_id":"`+taskID+`","result":"hijack"}`))
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for wrong agent, got: %s", result.Content)
	}
}

func TestTeamTasks_Progress(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	taskID, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Long task",
		Status:     "in_progress",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
	})

	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"progress","task_id":"`+taskID+`","percent":60,"text":"Halfway done"}`))
	if err != nil {
		t.Fatalf("progress: %v", err)
	}
	if result.IsError {
		t.Fatalf("progress error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "60%") {
		t.Fatalf("expected 60%% in result: %s", result.Content)
	}
}

func TestTeamTasks_Cancel(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	taskID, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Cancel me",
		Status:     "pending",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
	})

	// Lead cancels.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"cancel","task_id":"`+taskID+`"}`))
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if result.IsError {
		t.Fatalf("cancel error: %s", result.Content)
	}

	task, _ := s.GetTask(context.Background(), taskID)
	if task.Status != "cancelled" {
		t.Fatalf("expected cancelled, got %s", task.Status)
	}
}

func TestTeamTasks_Comment(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	taskID, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Commented task",
		Status:     "in_progress",
		AssignedTo: "member-a",
		CreatedBy:  "lead-agent",
	})

	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"comment","task_id":"`+taskID+`","text":"Need help with section 3"}`))
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if result.IsError {
		t.Fatalf("comment error: %s", result.Content)
	}

	comments, _ := s.ListComments(context.Background(), taskID)
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}
	if comments[0].Content != "Need help with section 3" {
		t.Fatalf("unexpected comment: %q", comments[0].Content)
	}
}

func TestTeamTasks_ApproveReject(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	// Create a task in_review status.
	taskID, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:          teamID,
		Title:           "Review me",
		Status:          "in_review",
		AssignedTo:      "member-a",
		CreatedBy:       "lead-agent",
		RequireApproval: true,
		Result:          "Draft result",
	})

	// Lead approves.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"approve","task_id":"`+taskID+`"}`))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if result.IsError {
		t.Fatalf("approve error: %s", result.Content)
	}

	task, _ := s.GetTask(context.Background(), taskID)
	if task.Status != "completed" {
		t.Fatalf("expected completed after approve, got %s", task.Status)
	}

	// Create another task for rejection.
	taskID2, _ := s.CreateTask(context.Background(), store.TeamTask{
		TeamID:     teamID,
		Title:      "Reject me",
		Status:     "in_review",
		AssignedTo: "member-b",
		CreatedBy:  "lead-agent",
		Result:     "Bad result",
	})

	result, err = reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"reject","task_id":"`+taskID2+`","text":"Needs more detail"}`))
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if result.IsError {
		t.Fatalf("reject error: %s", result.Content)
	}

	task, _ = s.GetTask(context.Background(), taskID2)
	if task.Status != "pending" {
		t.Fatalf("expected pending after reject (re-queued), got %s", task.Status)
	}
}

func TestTeamTasks_Search(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)

	s.CreateTask(context.Background(), store.TeamTask{
		TeamID: teamID, Title: "Research AI safety", Status: "pending", CreatedBy: "lead-agent",
	})
	s.CreateTask(context.Background(), store.TeamTask{
		TeamID: teamID, Title: "Write unit tests", Status: "pending", CreatedBy: "lead-agent",
	})

	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"search","team_id":"`+teamID+`","query":"AI"}`))
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.IsError {
		t.Fatalf("search error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "AI safety") {
		t.Fatalf("expected AI safety in results: %s", result.Content)
	}
}

func TestTeamTasks_UnknownAction(t *testing.T) {
	reg, _, _ := setupTeamTasks(t)

	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"explode"}`))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for unknown action, got: %s", result.Content)
	}
}

func TestTeamTasks_DuplicatePrevention(t *testing.T) {
	reg, _, teamID := setupTeamTasks(t)

	// First create succeeds.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Duplicate test","assignee":"member-a"}`))
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	if result.IsError {
		t.Fatalf("first create error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Task created") {
		t.Fatalf("expected 'Task created', got: %s", result.Content)
	}

	// Second create with same title+assignee is rejected.
	result, err = reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Duplicate test","assignee":"member-a"}`))
	if err != nil {
		t.Fatalf("duplicate create: %v", err)
	}
	if !strings.Contains(result.Content, "already exists") {
		t.Fatalf("expected 'already exists', got: %s", result.Content)
	}

	// Same title but different assignee is allowed.
	result, err = reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Duplicate test","assignee":"member-b"}`))
	if err != nil {
		t.Fatalf("different assignee create: %v", err)
	}
	if result.IsError {
		t.Fatalf("different assignee should succeed: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Task created") {
		t.Fatalf("expected 'Task created', got: %s", result.Content)
	}
}

func TestTeamTasks_CreateWithParentID(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)
	ctx := context.Background()

	// Create parent task directly in DB.
	parentID, err := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Parent task", CreatedBy: "lead-agent", Status: "pending",
	})
	if err != nil {
		t.Fatalf("create parent: %v", err)
	}

	// Lead creates subtask with parent_id.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Subtask","assignee":"member-a","parent_id":"`+parentID+`"}`))
	if err != nil {
		t.Fatalf("create subtask: %v", err)
	}
	if result.IsError {
		t.Fatalf("create subtask error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Task created") {
		t.Fatalf("expected 'Task created', got: %s", result.Content)
	}

	// Verify subtask_count incremented on parent.
	parent, _ := s.GetTask(ctx, parentID)
	if parent.SubtaskCount != 1 {
		t.Fatalf("expected parent subtask_count 1, got %d", parent.SubtaskCount)
	}

	// Verify subtask has parent_id set.
	children, _ := s.GetTasksByParent(ctx, parentID)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if children[0].Title != "Subtask" {
		t.Fatalf("expected child title 'Subtask', got %q", children[0].Title)
	}
}

func TestTeamTasks_CreateWithInvalidParent(t *testing.T) {
	reg, _, teamID := setupTeamTasks(t)

	// Non-existent parent.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Orphan","assignee":"member-a","parent_id":"nonexistent"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for nonexistent parent, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "parent task not found") {
		t.Fatalf("expected 'parent task not found', got: %s", result.Content)
	}
}

func TestTeamTasks_CreateWithTerminalParent(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)
	ctx := context.Background()

	// Create a completed parent.
	parentID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Done parent", CreatedBy: "lead-agent", Status: "completed",
	})

	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Child of done","assignee":"member-a","parent_id":"`+parentID+`"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for terminal parent, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "terminal state") {
		t.Fatalf("expected 'terminal state', got: %s", result.Content)
	}
}

func TestTeamTasks_CreateSelfAssign(t *testing.T) {
	reg, _, teamID := setupTeamTasks(t)

	// Lead tries to assign task to itself.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"`+teamID+`","subject":"Self task","assignee":"lead-agent"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for self-assign, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "handle this work directly") {
		t.Fatalf("expected 'handle this work directly' guidance, got: %s", result.Content)
	}
}

func TestTeamTasks_CreateWithWrongTeamParent(t *testing.T) {
	reg, s, _ := setupTeamTasks(t)
	ctx := context.Background()

	// Create a second team with its own task.
	otherTeamID := "other-team-id"
	_, err := s.DB().ExecContext(ctx,
		`INSERT INTO teams (id, name, lead_id, config, description, status, settings, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', 'active', '{}', datetime('now'), datetime('now'))`,
		otherTeamID, "Other Team", "other-lead", `{"members":[]}`)
	if err != nil {
		t.Fatalf("creating other team: %v", err)
	}
	otherParentID, err := s.CreateTask(ctx, store.TeamTask{
		TeamID: otherTeamID, Title: "Other team task", CreatedBy: "other-lead", Status: "pending",
	})
	if err != nil {
		t.Fatalf("create other task: %v", err)
	}

	// Try to create a subtask in test-team-id with parent from other-team-id.
	result, err := reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"create","team_id":"test-team-id","subject":"Cross-team child","assignee":"member-a","parent_id":"`+otherParentID+`"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for wrong-team parent, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "different team") {
		t.Fatalf("expected 'different team', got: %s", result.Content)
	}
}

func TestTeamTasks_SendAndInbox(t *testing.T) {
	reg, _, _ := setupTeamTasks(t)

	// Member sends a message to team.
	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"send","text":"Need help with research"}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if result.IsError {
		t.Fatalf("send error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "broadcast") {
		t.Fatalf("expected broadcast confirmation, got: %s", result.Content)
	}

	// Lead sends a direct message to member-a.
	result, err = reg.Execute(ctxWithAgent("lead-agent"), "team_tasks",
		json.RawMessage(`{"action":"send","to_agent":"member-a","text":"Check the API docs"}`))
	if err != nil {
		t.Fatalf("send DM: %v", err)
	}
	if !strings.Contains(result.Content, "member-a") {
		t.Fatalf("expected DM confirmation, got: %s", result.Content)
	}

	// Member-a checks inbox (should see the DM + broadcast).
	result, err = reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"inbox"}`))
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	if result.IsError {
		t.Fatalf("inbox error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Check the API docs") {
		t.Fatalf("expected DM in inbox, got: %s", result.Content)
	}

	// Check inbox again — should be empty (messages marked read).
	result, err = reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"inbox"}`))
	if err != nil {
		t.Fatalf("inbox 2: %v", err)
	}
	if !strings.Contains(result.Content, "No unread") {
		t.Fatalf("expected no unread after read, got: %s", result.Content)
	}
}

func TestTeamTasks_SendNoText(t *testing.T) {
	reg, _, _ := setupTeamTasks(t)

	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"send"}`))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error for missing text, got: %s", result.Content)
	}
}

func TestTeamTasks_BlockerEscalation(t *testing.T) {
	reg, s, teamID := setupTeamTasks(t)
	ctx := context.Background()

	// Create a task.
	taskID, _ := s.CreateTask(ctx, store.TeamTask{
		TeamID: teamID, Title: "Task with blocker", CreatedBy: "lead-agent",
		AssignedTo: "member-a",
	})
	s.ClaimTask(ctx, taskID, "member-a")

	// Member adds a blocker comment.
	result, err := reg.Execute(ctxWithAgent("member-a"), "team_tasks",
		json.RawMessage(`{"action":"comment","task_id":"`+taskID+`","text":"blocker: cannot proceed without API keys","status":"blocker"}`))
	if err != nil {
		t.Fatalf("blocker comment: %v", err)
	}
	if !strings.Contains(result.Content, "Blocker added") {
		t.Fatalf("expected 'Blocker added', got: %s", result.Content)
	}

	// Verify task is now failed.
	task, _ := s.GetTask(ctx, taskID)
	if task.Status != "failed" {
		t.Fatalf("expected failed after blocker, got %q", task.Status)
	}
	if !strings.Contains(task.ErrorMessage, "[blocker]") {
		t.Fatalf("expected [blocker] in error message, got: %s", task.ErrorMessage)
	}
}
