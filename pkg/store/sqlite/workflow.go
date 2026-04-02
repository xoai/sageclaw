package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xoai/sageclaw/pkg/store"
)

// Terminal workflow states — no further transitions allowed.
var terminalStates = map[string]bool{
	"complete":  true,
	"cancelled": true,
	"failed":    true,
}

// allowedWorkflowFields is the allowlist of column names that can be updated via fields map.
var allowedWorkflowFields = map[string]bool{
	"plan_json":    true,
	"task_ids":     true,
	"user_message": true,
	"announcement": true,
	"results_json": true,
	"error":        true,
}

func (s *Store) CreateWorkflow(ctx context.Context, w store.TeamWorkflow) (string, error) {
	if w.ID == "" {
		w.ID = uuid.NewString()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO team_workflows (id, team_id, session_id, state, version, user_message, state_entered_at)
		VALUES (?, ?, ?, 'analyze', 0, ?, datetime('now'))`,
		w.ID, w.TeamID, w.SessionID, w.UserMessage)
	if err != nil {
		return "", fmt.Errorf("create workflow: %w", err)
	}
	return w.ID, nil
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (*store.TeamWorkflow, error) {
	w := &store.TeamWorkflow{}
	var stateEnteredAt, createdAt, updatedAt string
	var completedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, session_id, state, version,
			COALESCE(plan_json,''), COALESCE(task_ids,''),
			COALESCE(user_message,''), COALESCE(announcement,''),
			COALESCE(results_json,''), COALESCE(error,''),
			state_entered_at, created_at, updated_at, completed_at
		FROM team_workflows WHERE id = ?`, id).Scan(
		&w.ID, &w.TeamID, &w.SessionID, &w.State, &w.Version,
		&w.PlanJSON, &w.TaskIDs, &w.UserMessage, &w.Announcement,
		&w.ResultsJSON, &w.Error,
		&stateEnteredAt, &createdAt, &updatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get workflow: %w", err)
	}
	w.StateEnteredAt, _ = time.Parse("2006-01-02 15:04:05", stateEnteredAt)
	w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	if completedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", completedAt.String)
		w.CompletedAt = &t
	}
	return w, nil
}

func (s *Store) UpdateWorkflowState(ctx context.Context, id, state string, version int, fields map[string]any) error {
	setClauses := []string{"state = ?", "version = version + 1", "updated_at = datetime('now')", "state_entered_at = datetime('now')"}
	args := []any{state}

	if terminalStates[state] {
		setClauses = append(setClauses, "completed_at = datetime('now')")
	}
	for k, v := range fields {
		if !allowedWorkflowFields[k] {
			return fmt.Errorf("update workflow: disallowed field %q", k)
		}
		setClauses = append(setClauses, k+" = ?")
		args = append(args, v)
	}
	args = append(args, id, version)

	result, err := s.db.ExecContext(ctx, fmt.Sprintf(
		"UPDATE team_workflows SET %s WHERE id = ? AND version = ?",
		strings.Join(setClauses, ", ")),
		args...)
	if err != nil {
		return fmt.Errorf("update workflow state: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("workflow %s: version conflict (expected %d)", id, version)
	}
	return nil
}

func (s *Store) GetActiveWorkflow(ctx context.Context, teamID, sessionID string) (*store.TeamWorkflow, error) {
	w := &store.TeamWorkflow{}
	var stateEnteredAt, createdAt, updatedAt string
	var completedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, team_id, session_id, state, version,
			COALESCE(plan_json,''), COALESCE(task_ids,''),
			COALESCE(user_message,''), COALESCE(announcement,''),
			COALESCE(results_json,''), COALESCE(error,''),
			state_entered_at, created_at, updated_at, completed_at
		FROM team_workflows
		WHERE team_id = ? AND session_id = ? AND state NOT IN ('complete','cancelled','failed')
		ORDER BY created_at DESC LIMIT 1`, teamID, sessionID).Scan(
		&w.ID, &w.TeamID, &w.SessionID, &w.State, &w.Version,
		&w.PlanJSON, &w.TaskIDs, &w.UserMessage, &w.Announcement,
		&w.ResultsJSON, &w.Error,
		&stateEnteredAt, &createdAt, &updatedAt, &completedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get active workflow: %w", err)
	}
	w.StateEnteredAt, _ = time.Parse("2006-01-02 15:04:05", stateEnteredAt)
	w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	if completedAt.Valid {
		t, _ := time.Parse("2006-01-02 15:04:05", completedAt.String)
		w.CompletedAt = &t
	}
	return w, nil
}

func (s *Store) ListStaleWorkflows(ctx context.Context, timeout time.Duration) ([]store.TeamWorkflow, error) {
	cutoff := time.Now().Add(-timeout).UTC().Format("2006-01-02 15:04:05")
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, team_id, session_id, state, version,
			COALESCE(plan_json,''), COALESCE(task_ids,''),
			COALESCE(user_message,''), COALESCE(announcement,''),
			COALESCE(results_json,''), COALESCE(error,''),
			state_entered_at, created_at, updated_at, completed_at
		FROM team_workflows
		WHERE state NOT IN ('complete','cancelled','failed')
		  AND state_entered_at < ?`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list stale workflows: %w", err)
	}
	defer rows.Close()

	var result []store.TeamWorkflow
	for rows.Next() {
		var w store.TeamWorkflow
		var stateEnteredAt, createdAt, updatedAt string
		var completedAt sql.NullString
		if err := rows.Scan(
			&w.ID, &w.TeamID, &w.SessionID, &w.State, &w.Version,
			&w.PlanJSON, &w.TaskIDs, &w.UserMessage, &w.Announcement,
			&w.ResultsJSON, &w.Error,
			&stateEnteredAt, &createdAt, &updatedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan stale workflow: %w", err)
		}
		w.StateEnteredAt, _ = time.Parse("2006-01-02 15:04:05", stateEnteredAt)
		w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		if completedAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", completedAt.String)
			w.CompletedAt = &t
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (s *Store) ListNonTerminalWorkflows(ctx context.Context) ([]store.TeamWorkflow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, team_id, session_id, state, version,
			COALESCE(plan_json,''), COALESCE(task_ids,''),
			COALESCE(user_message,''), COALESCE(announcement,''),
			COALESCE(results_json,''), COALESCE(error,''),
			state_entered_at, created_at, updated_at, completed_at
		FROM team_workflows
		WHERE state NOT IN ('complete','cancelled','failed')`)
	if err != nil {
		return nil, fmt.Errorf("list non-terminal workflows: %w", err)
	}
	defer rows.Close()

	var result []store.TeamWorkflow
	for rows.Next() {
		var w store.TeamWorkflow
		var stateEnteredAt, createdAt, updatedAt string
		var completedAt sql.NullString
		if err := rows.Scan(
			&w.ID, &w.TeamID, &w.SessionID, &w.State, &w.Version,
			&w.PlanJSON, &w.TaskIDs, &w.UserMessage, &w.Announcement,
			&w.ResultsJSON, &w.Error,
			&stateEnteredAt, &createdAt, &updatedAt, &completedAt,
		); err != nil {
			return nil, fmt.Errorf("scan non-terminal workflow: %w", err)
		}
		w.StateEnteredAt, _ = time.Parse("2006-01-02 15:04:05", stateEnteredAt)
		w.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		w.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		if completedAt.Valid {
			t, _ := time.Parse("2006-01-02 15:04:05", completedAt.String)
			w.CompletedAt = &t
		}
		result = append(result, w)
	}
	return result, rows.Err()
}

func (s *Store) CancelWorkflow(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE team_workflows
		SET state = 'cancelled', version = version + 1,
			updated_at = datetime('now'), completed_at = datetime('now'),
			state_entered_at = datetime('now')
		WHERE id = ? AND state NOT IN ('complete','cancelled','failed')`, id)
	if err != nil {
		return fmt.Errorf("cancel workflow: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("workflow %s: already terminal or not found", id)
	}
	return nil
}
