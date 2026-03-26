package activity

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	tmpFile := t.TempDir() + "/test.db"
	db, err := sql.Open("sqlite", tmpFile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close(); os.Remove(tmpFile) })

	// Create required tables.
	db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY)`)
	db.Exec(`INSERT INTO sessions (id) VALUES ('sess-1')`)
	db.Exec(`CREATE TABLE activities (
		id TEXT PRIMARY KEY, session_id TEXT NOT NULL, agent_id TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending', summary TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0,
		cache_creation INTEGER NOT NULL DEFAULT 0, cache_read INTEGER NOT NULL DEFAULT 0,
		cost_usd REAL NOT NULL DEFAULT 0.0, iterations INTEGER NOT NULL DEFAULT 0,
		tool_calls INTEGER NOT NULL DEFAULT 0, parent_id TEXT,
		error_message TEXT, started_at TEXT NOT NULL DEFAULT (datetime('now')),
		completed_at TEXT, timeout_seconds INTEGER NOT NULL DEFAULT 300,
		audio_input_ms INTEGER NOT NULL DEFAULT 0,
		audio_output_ms INTEGER NOT NULL DEFAULT 0
	)`)

	return db
}

func TestTracker_FullLifecycle(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	// Start.
	id, err := tracker.Start(ctx, "sess-1", "agent-1", "", 0)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if id == "" {
		t.Fatal("expected non-empty ID")
	}

	// Verify initial state.
	a, err := tracker.Get(ctx, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Status != StatusPending {
		t.Fatalf("expected pending, got %s", a.Status)
	}

	// Record iteration.
	if err := tracker.RecordIteration(ctx, id, 100, 50, 10, 5, 0.001); err != nil {
		t.Fatalf("RecordIteration: %v", err)
	}
	a, _ = tracker.Get(ctx, id)
	if a.Status != StatusThinking {
		t.Fatalf("expected thinking after iteration, got %s", a.Status)
	}
	if a.InputTokens != 100 || a.OutputTokens != 50 {
		t.Fatalf("wrong tokens: in=%d out=%d", a.InputTokens, a.OutputTokens)
	}
	if a.Iterations != 1 {
		t.Fatalf("expected 1 iteration, got %d", a.Iterations)
	}

	// Record tool call.
	if err := tracker.RecordToolCall(ctx, id); err != nil {
		t.Fatalf("RecordToolCall: %v", err)
	}
	a, _ = tracker.Get(ctx, id)
	if a.Status != StatusActing {
		t.Fatalf("expected acting after tool call, got %s", a.Status)
	}
	if a.ToolCalls != 1 {
		t.Fatalf("expected 1 tool call, got %d", a.ToolCalls)
	}

	// Complete.
	if err := tracker.Complete(ctx, id, "Said hello"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	a, _ = tracker.Get(ctx, id)
	if a.Status != StatusCompleted {
		t.Fatalf("expected completed, got %s", a.Status)
	}
	if a.Summary != "Said hello" {
		t.Fatalf("wrong summary: %s", a.Summary)
	}
	if a.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
}

func TestTracker_Fail(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	id, _ := tracker.Start(ctx, "sess-1", "agent-1", "", 0)
	tracker.Fail(ctx, id, "something broke", false)

	a, _ := tracker.Get(ctx, id)
	if a.Status != StatusFailed {
		t.Fatalf("expected failed, got %s", a.Status)
	}
	if a.ErrorMessage != "something broke" {
		t.Fatalf("wrong error: %s", a.ErrorMessage)
	}
}

func TestTracker_Timeout(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	id, _ := tracker.Start(ctx, "sess-1", "agent-1", "", 0)
	tracker.Fail(ctx, id, "timed out after 300s", true)

	a, _ := tracker.Get(ctx, id)
	if a.Status != StatusTimeout {
		t.Fatalf("expected timeout, got %s", a.Status)
	}
}

func TestTracker_ListBySession(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	tracker.Start(ctx, "sess-1", "agent-1", "", 0)
	tracker.Start(ctx, "sess-1", "agent-1", "", 0)

	activities, err := tracker.ListBySession(ctx, "sess-1", 10)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(activities) != 2 {
		t.Fatalf("expected 2, got %d", len(activities))
	}
}

func TestTracker_ListRecent(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	tracker.Start(ctx, "sess-1", "agent-1", "", 0)

	activities, err := tracker.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecent: %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("expected 1, got %d", len(activities))
	}
}

func TestTracker_ParentChild(t *testing.T) {
	db := setupTestDB(t)
	tracker := NewTracker(db)
	ctx := context.Background()

	parentID, _ := tracker.Start(ctx, "sess-1", "agent-1", "", 0)
	childID, _ := tracker.Start(ctx, "sess-1", "agent-2", parentID, 0)

	child, _ := tracker.Get(ctx, childID)
	if child.ParentID != parentID {
		t.Fatalf("expected parent %s, got %s", parentID, child.ParentID)
	}
}
