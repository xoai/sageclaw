package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func TestWorkflow_CreateAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	wf := store.TeamWorkflow{
		TeamID:      "team-1",
		SessionID:   "sess-1",
		UserMessage: "Research AI safety",
	}

	id, err := s.CreateWorkflow(ctx, wf)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	got, err := s.GetWorkflow(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil {
		t.Fatal("expected workflow")
	}
	if got.State != "analyze" {
		t.Errorf("state = %q, want analyze", got.State)
	}
	if got.Version != 0 {
		t.Errorf("version = %d, want 0", got.Version)
	}
	if got.UserMessage != "Research AI safety" {
		t.Errorf("user_message = %q", got.UserMessage)
	}
}

func TestWorkflow_GetNotFound(t *testing.T) {
	s := newTestStore(t)
	got, err := s.GetWorkflow(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for missing workflow")
	}
}

func TestWorkflow_UpdateState(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	err := s.UpdateWorkflowState(ctx, id, "plan", 0, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetWorkflow(ctx, id)
	if got.State != "plan" {
		t.Errorf("state = %q, want plan", got.State)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
}

func TestWorkflow_UpdateState_VersionConflict(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	// Advance to version 1.
	_ = s.UpdateWorkflowState(ctx, id, "plan", 0, nil)

	// Try to update with stale version 0.
	err := s.UpdateWorkflowState(ctx, id, "create", 0, nil)
	if err == nil {
		t.Fatal("expected version conflict error")
	}
}

func TestWorkflow_UpdateState_WithFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	err := s.UpdateWorkflowState(ctx, id, "create", 0, map[string]any{
		"plan_json":    `{"tasks":[]}`,
		"announcement": "Delegating to team",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := s.GetWorkflow(ctx, id)
	if got.PlanJSON != `{"tasks":[]}` {
		t.Errorf("plan_json = %q", got.PlanJSON)
	}
	if got.Announcement != "Delegating to team" {
		t.Errorf("announcement = %q", got.Announcement)
	}
}

func TestWorkflow_GetActive(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	active, err := s.GetActiveWorkflow(ctx, "team-1", "sess-1")
	if err != nil {
		t.Fatalf("get active: %v", err)
	}
	if active == nil {
		t.Fatal("expected active workflow")
	}
	if active.State != "analyze" {
		t.Errorf("state = %q", active.State)
	}

	// No active for different team.
	none, _ := s.GetActiveWorkflow(ctx, "team-2", "sess-1")
	if none != nil {
		t.Error("expected nil for different team")
	}
}

func TestWorkflow_GetActive_TerminalExcluded(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})
	_ = s.CancelWorkflow(ctx, id)

	active, _ := s.GetActiveWorkflow(ctx, "team-1", "sess-1")
	if active != nil {
		t.Error("cancelled workflow should not be active")
	}
}

func TestWorkflow_Cancel(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	err := s.CancelWorkflow(ctx, id)
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}

	got, _ := s.GetWorkflow(ctx, id)
	if got.State != "cancelled" {
		t.Errorf("state = %q, want cancelled", got.State)
	}
	if got.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}
}

func TestWorkflow_Cancel_AlreadyTerminal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})
	_ = s.CancelWorkflow(ctx, id)

	// Second cancel should fail.
	err := s.CancelWorkflow(ctx, id)
	if err == nil {
		t.Fatal("expected error cancelling already-cancelled workflow")
	}
}

func TestWorkflow_UpdateState_DisallowedField(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	err := s.UpdateWorkflowState(ctx, id, "plan", 0, map[string]any{
		"evil_field; DROP TABLE team_workflows --": "malicious",
	})
	if err == nil {
		t.Fatal("expected error for disallowed field name")
	}
}

func TestWorkflow_ListStale(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create workflow — its state_entered_at is "now".
	s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})

	// With 0 timeout, no workflows should be stale.
	stale, err := s.ListStaleWorkflows(ctx, 1*time.Hour)
	if err != nil {
		t.Fatalf("list stale: %v", err)
	}
	if len(stale) != 0 {
		t.Errorf("expected 0 stale, got %d", len(stale))
	}

	// With very short timeout, workflow should appear stale (its state_entered_at is in the past).
	// Force the state_entered_at to be old.
	s.db.ExecContext(ctx, `UPDATE team_workflows SET state_entered_at = datetime('now', '-2 hours')`)
	stale, _ = s.ListStaleWorkflows(ctx, 1*time.Hour)
	if len(stale) != 1 {
		t.Errorf("expected 1 stale, got %d", len(stale))
	}
}
