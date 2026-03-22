package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func (s *Store) CreateTask(ctx context.Context, task store.TeamTask) (string, error) {
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	status := "open"
	if task.BlockedBy != "" {
		status = "blocked"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, title, description, status, assigned_to, created_by, blocked_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, task.TeamID, task.Title, task.Description, status, task.AssignedTo, task.CreatedBy, task.BlockedBy, now, now)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}
	return id, nil
}

func (s *Store) ClaimTask(ctx context.Context, taskID, agentID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'claimed', assigned_to = ?, updated_at = ? WHERE id = ? AND status = 'open'`,
		agentID, now, taskID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s is not open or does not exist", taskID)
	}
	return nil
}

func (s *Store) CompleteTask(ctx context.Context, taskID string, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'completed', result = ?, updated_at = ? WHERE id = ? AND status = 'claimed'`,
		result, now, taskID)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s is not claimed or does not exist", taskID)
	}
	return nil
}

func (s *Store) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, taskID)
	return err
}

func (s *Store) ListTasks(ctx context.Context, teamID string, status string) ([]store.TeamTask, error) {
	query := `SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
		created_by, COALESCE(result,''), COALESCE(blocked_by,''), created_at, updated_at FROM team_tasks WHERE team_id = ?`
	args := []any{teamID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []store.TeamTask
	for rows.Next() {
		var t store.TeamTask
		var createdAt, updatedAt string
		if err := rows.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status,
			&t.AssignedTo, &t.CreatedBy, &t.Result, &t.BlockedBy, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) SendTeamMessage(ctx context.Context, msg store.TeamMessage) error {
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_messages (id, team_id, from_agent, to_agent, content, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, msg.TeamID, msg.FromAgent, msg.ToAgent, msg.Content, now)
	return err
}

func (s *Store) GetTeamMessages(ctx context.Context, agentID string, unreadOnly bool) ([]store.TeamMessage, error) {
	query := `SELECT id, team_id, from_agent, COALESCE(to_agent,''), content, read, created_at
		FROM team_messages WHERE (to_agent = ? OR to_agent = '' OR to_agent IS NULL)`
	args := []any{agentID}
	if unreadOnly {
		query += ` AND read = 0`
	}
	query += ` ORDER BY created_at DESC LIMIT 50`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []store.TeamMessage
	for rows.Next() {
		var m store.TeamMessage
		var readInt int
		var createdAt string
		if err := rows.Scan(&m.ID, &m.TeamID, &m.FromAgent, &m.ToAgent, &m.Content, &readInt, &createdAt); err != nil {
			return nil, err
		}
		m.Read = readInt == 1
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (s *Store) MarkMessageRead(ctx context.Context, messageID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE team_messages SET read = 1 WHERE id = ?`, messageID)
	return err
}
