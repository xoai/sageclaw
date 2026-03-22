package activity

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"time"
)

// Status represents the Activity lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusThinking  Status = "thinking"
	StatusActing    Status = "acting"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
	StatusTimeout   Status = "timeout"
)

// Activity is the user-facing unit of work.
type Activity struct {
	ID             string
	SessionID      string
	AgentID        string
	Status         Status
	Summary        string
	InputTokens    int
	OutputTokens   int
	CacheCreation  int
	CacheRead      int
	CostUSD        float64
	Iterations     int
	ToolCalls      int
	ParentID       string
	ErrorMessage   string
	StartedAt      time.Time
	CompletedAt    *time.Time
	TimeoutSeconds int
}

// Tracker manages Activity lifecycle against the database.
type Tracker struct {
	db *sql.DB
}

// NewTracker creates a new Activity tracker.
func NewTracker(db *sql.DB) *Tracker {
	return &Tracker{db: db}
}

// Start creates a new Activity in pending status. Returns the Activity ID.
func (t *Tracker) Start(ctx context.Context, sessionID, agentID, parentID string, timeoutSec int) (string, error) {
	id := newID()
	if timeoutSec <= 0 {
		timeoutSec = 300
	}
	var parentVal *string
	if parentID != "" {
		parentVal = &parentID
	}
	_, err := t.db.ExecContext(ctx,
		`INSERT INTO activities (id, session_id, agent_id, status, parent_id, timeout_seconds)
		 VALUES (?, ?, ?, 'pending', ?, ?)`,
		id, sessionID, agentID, parentVal, timeoutSec)
	if err != nil {
		return "", fmt.Errorf("creating activity: %w", err)
	}
	return id, nil
}

// UpdateStatus transitions the Activity to a new status.
func (t *Tracker) UpdateStatus(ctx context.Context, id string, status Status) error {
	_, err := t.db.ExecContext(ctx,
		`UPDATE activities SET status = ? WHERE id = ?`, string(status), id)
	return err
}

// RecordIteration increments the iteration count and updates token usage.
func (t *Tracker) RecordIteration(ctx context.Context, id string, inputTokens, outputTokens, cacheCreation, cacheRead int, costUSD float64) error {
	_, err := t.db.ExecContext(ctx,
		`UPDATE activities SET
			iterations = iterations + 1,
			input_tokens = input_tokens + ?,
			output_tokens = output_tokens + ?,
			cache_creation = cache_creation + ?,
			cache_read = cache_read + ?,
			cost_usd = cost_usd + ?,
			status = CASE WHEN status = 'pending' THEN 'thinking' ELSE status END
		 WHERE id = ?`,
		inputTokens, outputTokens, cacheCreation, cacheRead, costUSD, id)
	return err
}

// RecordToolCall increments the tool call count and sets status to acting.
func (t *Tracker) RecordToolCall(ctx context.Context, id string) error {
	_, err := t.db.ExecContext(ctx,
		`UPDATE activities SET tool_calls = tool_calls + 1, status = 'acting' WHERE id = ?`, id)
	return err
}

// Complete marks the Activity as completed with an optional summary.
func (t *Tracker) Complete(ctx context.Context, id, summary string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := t.db.ExecContext(ctx,
		`UPDATE activities SET status = 'completed', summary = ?, completed_at = ? WHERE id = ?`,
		summary, now, id)
	return err
}

// Fail marks the Activity as failed with an error message.
func (t *Tracker) Fail(ctx context.Context, id, errMsg string, isTimeout bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	status := StatusFailed
	if isTimeout {
		status = StatusTimeout
	}
	_, err := t.db.ExecContext(ctx,
		`UPDATE activities SET status = ?, error_message = ?, completed_at = ? WHERE id = ?`,
		string(status), errMsg, now, id)
	return err
}

// Get retrieves an Activity by ID.
func (t *Tracker) Get(ctx context.Context, id string) (*Activity, error) {
	a := &Activity{}
	var parentID, errMsg, summary sql.NullString
	var completedAt sql.NullString
	var startedAt string
	err := t.db.QueryRowContext(ctx,
		`SELECT id, session_id, agent_id, status, summary, input_tokens, output_tokens,
			cache_creation, cache_read, cost_usd, iterations, tool_calls, parent_id,
			error_message, started_at, completed_at, timeout_seconds
		 FROM activities WHERE id = ?`, id).Scan(
		&a.ID, &a.SessionID, &a.AgentID, &a.Status, &summary,
		&a.InputTokens, &a.OutputTokens, &a.CacheCreation, &a.CacheRead,
		&a.CostUSD, &a.Iterations, &a.ToolCalls, &parentID,
		&errMsg, &startedAt, &completedAt, &a.TimeoutSeconds)
	if err != nil {
		return nil, err
	}
	a.Summary = summary.String
	a.ParentID = parentID.String
	a.ErrorMessage = errMsg.String
	a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
	if completedAt.Valid {
		ct, _ := time.Parse(time.RFC3339, completedAt.String)
		a.CompletedAt = &ct
	}
	return a, nil
}

// ListBySession returns activities for a session, newest first.
func (t *Tracker) ListBySession(ctx context.Context, sessionID string, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := t.db.QueryContext(ctx,
		`SELECT id, session_id, agent_id, status, COALESCE(summary,''), input_tokens, output_tokens,
			cache_creation, cache_read, cost_usd, iterations, tool_calls,
			COALESCE(parent_id,''), COALESCE(error_message,''),
			started_at, completed_at, timeout_seconds
		 FROM activities WHERE session_id = ? ORDER BY started_at DESC LIMIT ?`,
		sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

// ListRecent returns the most recent activities across all sessions.
func (t *Tracker) ListRecent(ctx context.Context, limit int) ([]Activity, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := t.db.QueryContext(ctx,
		`SELECT id, session_id, agent_id, status, COALESCE(summary,''), input_tokens, output_tokens,
			cache_creation, cache_read, cost_usd, iterations, tool_calls,
			COALESCE(parent_id,''), COALESCE(error_message,''),
			started_at, completed_at, timeout_seconds
		 FROM activities ORDER BY started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivities(rows)
}

func scanActivities(rows *sql.Rows) ([]Activity, error) {
	var activities []Activity
	for rows.Next() {
		var a Activity
		var completedAt sql.NullString
		var startedAt string
		if err := rows.Scan(&a.ID, &a.SessionID, &a.AgentID, &a.Status, &a.Summary,
			&a.InputTokens, &a.OutputTokens, &a.CacheCreation, &a.CacheRead,
			&a.CostUSD, &a.Iterations, &a.ToolCalls, &a.ParentID, &a.ErrorMessage,
			&startedAt, &completedAt, &a.TimeoutSeconds); err != nil {
			return nil, err
		}
		a.StartedAt, _ = time.Parse(time.RFC3339, startedAt)
		if completedAt.Valid {
			ct, _ := time.Parse(time.RFC3339, completedAt.String)
			a.CompletedAt = &ct
		}
		activities = append(activities, a)
	}
	return activities, rows.Err()
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
