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
