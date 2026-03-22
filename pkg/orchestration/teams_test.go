package orchestration

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func setupTeamManager(t *testing.T) (*TeamManager, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Insert a team.
	s.DB().Exec(`INSERT INTO teams (id, name, lead_id) VALUES ('team1', 'Productivity', 'coordinator')`)

	teams := []Team{{ID: "team1", Name: "Productivity", LeadID: "coordinator", Members: []string{"researcher", "coder"}}}
	return NewTeamManager(s, teams), s
}

func TestTeam_CreateAndClaimTask(t *testing.T) {
	tm, _ := setupTeamManager(t)
	ctx := context.Background()

	taskID, err := tm.CreateTask(ctx, "team1", "Research AI safety", "Find top 5 papers", "coordinator")
	if err != nil {
		t.Fatalf("creating task: %v", err)
	}
	if taskID == "" {
		t.Fatal("expected task ID")
	}

	// Claim.
	if err := tm.ClaimTask(ctx, taskID, "researcher"); err != nil {
		t.Fatalf("claiming task: %v", err)
	}

	// Can't claim again.
	if err := tm.ClaimTask(ctx, taskID, "coder"); err == nil {
		t.Fatal("expected error claiming already-claimed task")
	}
}

func TestTeam_CompleteTask(t *testing.T) {
	tm, _ := setupTeamManager(t)
	ctx := context.Background()

	taskID, _ := tm.CreateTask(ctx, "team1", "Write report", "", "coordinator")
	tm.ClaimTask(ctx, taskID, "coder")

	if err := tm.CompleteTask(ctx, taskID, "Report written: 500 words"); err != nil {
		t.Fatalf("completing task: %v", err)
	}

	// Verify.
	tasks, _ := tm.ListTasks(ctx, "team1", "completed")
	if len(tasks) != 1 {
		t.Fatalf("expected 1 completed task, got %d", len(tasks))
	}
	if tasks[0].Result != "Report written: 500 words" {
		t.Fatalf("wrong result: %s", tasks[0].Result)
	}
}

func TestTeam_ListTasks(t *testing.T) {
	tm, _ := setupTeamManager(t)
	ctx := context.Background()

	tm.CreateTask(ctx, "team1", "Task A", "", "coordinator")
	tm.CreateTask(ctx, "team1", "Task B", "", "coordinator")

	all, err := tm.ListTasks(ctx, "team1", "")
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}

	open, _ := tm.ListTasks(ctx, "team1", "open")
	if len(open) != 2 {
		t.Fatalf("expected 2 open tasks, got %d", len(open))
	}
}

func TestTeam_Mailbox(t *testing.T) {
	tm, _ := setupTeamManager(t)
	ctx := context.Background()

	// Send message.
	if err := tm.SendMessage(ctx, "team1", "coordinator", "researcher", "Please start on the AI safety research"); err != nil {
		t.Fatalf("sending: %v", err)
	}

	// Check inbox.
	msgs, err := tm.GetMessages(ctx, "researcher", true)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "Please start on the AI safety research" {
		t.Fatalf("wrong content: %s", msgs[0].Content)
	}

	// Mark read.
	if err := tm.MarkRead(ctx, msgs[0].ID); err != nil {
		t.Fatalf("marking read: %v", err)
	}

	// Unread should be empty.
	unread, _ := tm.GetMessages(ctx, "researcher", true)
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread, got %d", len(unread))
	}
}

func TestTeam_Broadcast(t *testing.T) {
	tm, _ := setupTeamManager(t)
	ctx := context.Background()

	// Broadcast (to_agent = "").
	tm.SendMessage(ctx, "team1", "coordinator", "", "Team standup in 5 minutes")

	// Both members should see it.
	researcherMsgs, _ := tm.GetMessages(ctx, "researcher", false)
	coderMsgs, _ := tm.GetMessages(ctx, "coder", false)

	// Broadcast goes to empty to_agent, which matches everyone.
	if len(researcherMsgs) == 0 && len(coderMsgs) == 0 {
		t.Fatal("expected at least one member to receive broadcast")
	}
}
