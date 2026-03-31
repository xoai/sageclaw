package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// --- Team CRUD ---

func (s *Store) GetTeam(ctx context.Context, teamID string) (*store.Team, error) {
	var t store.Team
	var createdAt, updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, lead_id, COALESCE(description,''), COALESCE(status,'active'),
			config, COALESCE(settings,'{}'), created_at, updated_at
		 FROM teams WHERE id = ?`, teamID).Scan(
		&t.ID, &t.Name, &t.LeadID, &t.Description, &t.Status,
		&t.Config, &t.Settings, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get team: %w", err)
	}
	if createdAt.Valid {
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}
	return &t, nil
}

func (s *Store) GetTeamByAgent(ctx context.Context, agentID string) (*store.Team, string, error) {
	// Check if agent is a lead first.
	var t store.Team
	var createdAt, updatedAt sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, lead_id, COALESCE(description,''), COALESCE(status,'active'),
			config, COALESCE(settings,'{}'), created_at, updated_at
		 FROM teams WHERE lead_id = ? AND COALESCE(status,'active') = 'active'`, agentID).Scan(
		&t.ID, &t.Name, &t.LeadID, &t.Description, &t.Status,
		&t.Config, &t.Settings, &createdAt, &updatedAt)
	if err == nil {
		if createdAt.Valid {
			t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}
		if updatedAt.Valid {
			t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}
		return &t, "lead", nil
	}
	if err != sql.ErrNoRows {
		return nil, "", fmt.Errorf("get team by agent (lead check): %w", err)
	}

	// Check if agent is a member via config JSON.
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, lead_id, COALESCE(description,''), COALESCE(status,'active'),
			config, COALESCE(settings,'{}'), created_at, updated_at
		 FROM teams WHERE COALESCE(status,'active') = 'active'`)
	if err != nil {
		return nil, "", fmt.Errorf("get team by agent (member scan): %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var team store.Team
		var ca, ua sql.NullString
		if err := rows.Scan(&team.ID, &team.Name, &team.LeadID, &team.Description,
			&team.Status, &team.Config, &team.Settings, &ca, &ua); err != nil {
			return nil, "", err
		}
		if ca.Valid {
			team.CreatedAt, _ = time.Parse(time.RFC3339, ca.String)
		}
		if ua.Valid {
			team.UpdatedAt, _ = time.Parse(time.RFC3339, ua.String)
		}
		// Parse config JSON for members array.
		var cfg map[string]any
		if err := json.Unmarshal([]byte(team.Config), &cfg); err != nil {
			continue
		}
		members, _ := cfg["members"].([]any)
		for _, m := range members {
			if mid, ok := m.(string); ok && mid == agentID {
				return &team, "member", nil
			}
		}
	}

	return nil, "", nil
}

// allowedTeamColumns prevents SQL injection via dynamic column names.
var allowedTeamColumns = map[string]bool{
	"name": true, "lead_id": true, "status": true, "config": true,
	"description": true, "settings": true, "updated_at": true,
}

func (s *Store) UpdateTeam(ctx context.Context, teamID string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for k, v := range fields {
		if !allowedTeamColumns[k] {
			return fmt.Errorf("disallowed column in UpdateTeam: %q", k)
		}
		setClauses = append(setClauses, k+" = ?")
		args = append(args, v)
	}
	args = append(args, teamID)
	query := fmt.Sprintf("UPDATE teams SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) ListTeamMembers(ctx context.Context, teamID string) ([]store.TeamMember, error) {
	team, err := s.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return nil, err
	}
	members := []store.TeamMember{
		{AgentID: team.LeadID, Role: "lead"},
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(team.Config), &cfg); err == nil {
		if mlist, ok := cfg["members"].([]any); ok {
			for _, m := range mlist {
				if mid, ok := m.(string); ok {
					members = append(members, store.TeamMember{AgentID: mid, Role: "member"})
				}
			}
		}
	}
	return members, nil
}

// --- Task lifecycle ---

func (s *Store) CreateTask(ctx context.Context, task store.TeamTask) (string, error) {
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	status := "pending"
	if task.BlockedBy != "" {
		status = "blocked"
	}
	if task.Status != "" {
		status = task.Status
	}

	// Auto-assign task number.
	taskNum := task.TaskNumber
	if taskNum == 0 {
		num, err := s.NextTaskNumber(ctx, task.TeamID)
		if err != nil {
			return "", fmt.Errorf("getting next task number: %w", err)
		}
		taskNum = num
	}

	identifier := task.Identifier
	if identifier == "" {
		identifier = fmt.Sprintf("TSK-%d", taskNum)
	}

	requireApproval := 0
	if task.RequireApproval {
		requireApproval = 1
	}

	maxRetries := task.MaxRetries
	if maxRetries == 0 {
		maxRetries = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_tasks (id, team_id, title, description, status, assigned_to, created_by,
			blocked_by, parent_id, priority, owner_agent_id, batch_id, task_number, identifier,
			progress_percent, require_approval, session_id, retry_count, max_retries, error_message,
			created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, task.TeamID, task.Title, task.Description, status, task.AssignedTo, task.CreatedBy,
		task.BlockedBy, nilIfEmpty(task.ParentID), task.Priority, task.OwnerAgentID, task.BatchID,
		taskNum, identifier, task.ProgressPercent, requireApproval,
		task.SessionID, task.RetryCount, maxRetries, task.ErrorMessage, now, now)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}
	return id, nil
}

func (s *Store) GetTask(ctx context.Context, taskID string) (*store.TeamTask, error) {
	var t store.TeamTask
	var completedAt sql.NullString
	var claimedAt sql.NullString
	var parentID sql.NullString
	var requireApproval int
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks WHERE id = ?`, taskID).Scan(
		&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status, &t.AssignedTo,
		&t.CreatedBy, &t.Result, &t.BlockedBy, &parentID,
		&t.Priority, &t.OwnerAgentID, &t.BatchID, &t.TaskNumber,
		&t.Identifier, &t.ProgressPercent, &requireApproval,
		&t.SessionID, &t.RetryCount, &t.MaxRetries, &t.ErrorMessage,
		&t.SubtaskCount, &t.DispatchAttempts, &claimedAt, &completedAt, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	if parentID.Valid {
		t.ParentID = parentID.String
	}
	t.RequireApproval = requireApproval == 1
	if claimedAt.Valid {
		ca, _ := time.Parse(time.RFC3339, claimedAt.String)
		t.ClaimedAt = &ca
	}
	if completedAt.Valid {
		ct, _ := time.Parse(time.RFC3339, completedAt.String)
		t.CompletedAt = &ct
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &t, nil
}

// allowedTaskColumns prevents SQL injection via dynamic column names.
var allowedTaskColumns = map[string]bool{
	"status": true, "assigned_to": true, "priority": true, "result": true,
	"error_message": true, "session_id": true, "blocked_by": true,
	"progress_percent": true, "completed_at": true, "claimed_at": true,
	"dispatch_attempts": true, "updated_at": true,
}

func (s *Store) UpdateTask(ctx context.Context, taskID string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC().Format(time.RFC3339)
	setClauses := make([]string, 0, len(fields))
	args := make([]any, 0, len(fields)+1)
	for k, v := range fields {
		if !allowedTaskColumns[k] {
			return fmt.Errorf("disallowed column in UpdateTask: %q", k)
		}
		setClauses = append(setClauses, k+" = ?")
		args = append(args, v)
	}
	args = append(args, taskID)
	query := fmt.Sprintf("UPDATE team_tasks SET %s WHERE id = ?", strings.Join(setClauses, ", "))
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}

func (s *Store) UpdateTaskProgress(ctx context.Context, taskID string, percent int, text string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET progress_percent = ?, updated_at = ? WHERE id = ?`,
		percent, now, taskID)
	if err != nil {
		return err
	}
	// Store progress text as a system comment if non-empty.
	if text != "" {
		_, err = s.CreateComment(ctx, store.TeamTaskComment{
			TaskID:      taskID,
			CommentType: "status",
			Content:     text,
		})
	}
	return err
}

func (s *Store) ClaimTask(ctx context.Context, taskID, agentID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	// Retry on SQLITE_BUSY — task dispatch races with session saves.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		result, err := s.db.ExecContext(ctx,
			`UPDATE team_tasks SET status = 'in_progress', assigned_to = ?, claimed_at = ?, updated_at = ? WHERE id = ? AND status = 'pending'`,
			agentID, now, now, taskID)
		if err != nil {
			if strings.Contains(err.Error(), "SQLITE_BUSY") || strings.Contains(err.Error(), "database is locked") {
				lastErr = err
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(100*(attempt+1)) * time.Millisecond):
				}
				continue
			}
			return err
		}
		rows, _ := result.RowsAffected()
		if rows == 0 {
			return fmt.Errorf("task %s is not pending or does not exist", taskID)
		}
		return nil
	}
	return fmt.Errorf("failed to claim task after retries: %w", lastErr)
}

func (s *Store) CompleteTask(ctx context.Context, taskID string, result string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	// Check if task requires approval — if so, go to in_review instead.
	var requireApproval int
	var currentStatus string
	err := s.db.QueryRowContext(ctx,
		`SELECT require_approval, status FROM team_tasks WHERE id = ?`, taskID).Scan(&requireApproval, &currentStatus)
	if err != nil {
		return fmt.Errorf("checking task for completion: %w", err)
	}
	if currentStatus != "in_progress" {
		return fmt.Errorf("task %s is not in_progress (status: %s)", taskID, currentStatus)
	}

	targetStatus := "completed"
	if requireApproval == 1 {
		targetStatus = "in_review"
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = ?, result = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
		targetStatus, result, now, now, taskID)
	return err
}

func (s *Store) CancelTask(ctx context.Context, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'cancelled', updated_at = ? WHERE id = ? AND status NOT IN ('completed', 'cancelled')`,
		now, taskID)
	return err
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
		created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
		priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
		COALESCE(identifier,''), progress_percent, require_approval,
		COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
		COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
	FROM team_tasks WHERE team_id = ?`
	args := []any{teamID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY priority DESC, created_at ASC`
	return s.scanTasks(ctx, query, args...)
}

func (s *Store) GetTasksByParent(ctx context.Context, parentID string) ([]store.TeamTask, error) {
	return s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks WHERE parent_id = ? ORDER BY task_number ASC`, parentID)
}

func (s *Store) GetBlockedTasks(ctx context.Context, teamID string) ([]store.TeamTask, error) {
	return s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks WHERE team_id = ? AND status = 'blocked'`, teamID)
}

func (s *Store) UnblockTasks(ctx context.Context, completedTaskID string) ([]store.TeamTask, error) {
	// Find tasks that list completedTaskID in their blocked_by.
	blocked, err := s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks WHERE status = 'blocked' AND blocked_by LIKE ?`,
		"%"+completedTaskID+"%")
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var unblocked []store.TeamTask

	for _, task := range blocked {
		// Check if ALL blockers are completed.
		// Try exact match first, fall back to prefix match for legacy
		// truncated IDs (8-char prefix from older tool responses).
		blockerIDs := splitBlockers(task.BlockedBy)
		allCompleted := true
		for _, bid := range blockerIDs {
			if bid == "" {
				continue
			}
			var bstatus string
			err := s.db.QueryRowContext(ctx,
				`SELECT status FROM team_tasks WHERE id = ?`, bid).Scan(&bstatus)
			if err != nil {
				// Prefix fallback for truncated IDs.
				err = s.db.QueryRowContext(ctx,
					`SELECT status FROM team_tasks WHERE id LIKE ? LIMIT 1`,
					bid+"%").Scan(&bstatus)
			}
			if err != nil || bstatus != "completed" {
				allCompleted = false
				break
			}
		}
		if allCompleted {
			_, err := s.db.ExecContext(ctx,
				`UPDATE team_tasks SET status = 'pending', updated_at = ? WHERE id = ? AND status = 'blocked'`,
				now, task.ID)
			if err == nil {
				task.Status = "pending"
				unblocked = append(unblocked, task)
			}
		}
	}
	return unblocked, nil
}

func (s *Store) RetryTask(ctx context.Context, taskID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'pending', retry_count = retry_count + 1,
			error_message = '', updated_at = ?
		 WHERE id = ? AND status = 'failed' AND retry_count < max_retries`,
		now, taskID)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("task %s cannot be retried (not failed or max retries reached)", taskID)
	}
	return nil
}

func (s *Store) SearchTasks(ctx context.Context, teamID, query string) ([]store.TeamTask, error) {
	// Escape LIKE metacharacters to prevent wildcard injection.
	escaped := strings.NewReplacer("%", "\\%", "_", "\\_").Replace(query)
	pattern := "%" + escaped + "%"
	return s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks WHERE team_id = ? AND (title LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\' OR identifier LIKE ? ESCAPE '\')
		 ORDER BY created_at DESC LIMIT 50`,
		teamID, pattern, pattern, pattern)
}

func (s *Store) NextTaskNumber(ctx context.Context, teamID string) (int, error) {
	var maxNum sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(task_number) FROM team_tasks WHERE team_id = ?`, teamID).Scan(&maxNum)
	if err != nil {
		return 1, err
	}
	if !maxNum.Valid {
		return 1, nil
	}
	return int(maxNum.Int64) + 1, nil
}

// --- Task comments ---

func (s *Store) CreateComment(ctx context.Context, comment store.TeamTaskComment) (string, error) {
	id := newID()
	now := time.Now().UTC().Format(time.RFC3339)
	commentType := comment.CommentType
	if commentType == "" {
		commentType = "note"
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO team_task_comments (id, task_id, agent_id, user_id, content, comment_type, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, comment.TaskID, nilIfEmpty(comment.AgentID), nilIfEmpty(comment.UserID),
		comment.Content, commentType, now)
	if err != nil {
		return "", fmt.Errorf("creating comment: %w", err)
	}
	return id, nil
}

func (s *Store) ListComments(ctx context.Context, taskID string) ([]store.TeamTaskComment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, task_id, COALESCE(agent_id,''), COALESCE(user_id,''), content,
			COALESCE(comment_type,'note'), created_at
		 FROM team_task_comments WHERE task_id = ? ORDER BY created_at ASC`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var comments []store.TeamTaskComment
	for rows.Next() {
		var c store.TeamTaskComment
		var createdAt string
		if err := rows.Scan(&c.ID, &c.TaskID, &c.AgentID, &c.UserID, &c.Content,
			&c.CommentType, &createdAt); err != nil {
			return nil, err
		}
		c.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// --- Team messages (legacy mailbox) ---

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

// --- Reliability ---

func (s *Store) RecoverStaleTasks(ctx context.Context, timeoutSeconds int) ([]store.TeamTask, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	timeout := fmt.Sprintf("%d", timeoutSeconds)

	// Find in_progress tasks that exceeded the timeout.
	// NULL claimed_at fallback handles pre-migration tasks.
	tasks, err := s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks
		 WHERE status = 'in_progress'
		   AND (
		     (claimed_at IS NOT NULL AND claimed_at < datetime('now', '-' || ? || ' seconds'))
		     OR (claimed_at IS NULL AND updated_at < datetime('now', '-' || ? || ' seconds'))
		   )`,
		timeout, timeout)
	if err != nil {
		return nil, fmt.Errorf("querying stale tasks: %w", err)
	}

	// Mark each stale task as failed.
	for i := range tasks {
		_, err := s.db.ExecContext(ctx,
			`UPDATE team_tasks SET status = 'failed', error_message = ?, updated_at = ? WHERE id = ? AND status = 'in_progress'`,
			"stale_timeout: task not completed within configured timeout", now, tasks[i].ID)
		if err != nil {
			return nil, fmt.Errorf("recovering task %s: %w", tasks[i].ID, err)
		}
		tasks[i].Status = "failed"
		tasks[i].ErrorMessage = "stale_timeout: task not completed within configured timeout"
	}
	return tasks, nil
}

func (s *Store) IncrementDispatchAttempt(ctx context.Context, taskID string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	// Single atomic operation: UPDATE + SELECT in one transaction to prevent TOCTOU.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE team_tasks SET dispatch_attempts = dispatch_attempts + 1, updated_at = ? WHERE id = ?`,
		now, taskID)
	if err != nil {
		return 0, fmt.Errorf("incrementing dispatch attempts: %w", err)
	}

	var count int
	err = tx.QueryRowContext(ctx,
		`SELECT dispatch_attempts FROM team_tasks WHERE id = ?`, taskID).Scan(&count)
	if err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return count, nil
}

func (s *Store) CancelDependentTasks(ctx context.Context, taskID string) ([]store.TeamTask, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// First, find dependent tasks that will be cancelled.
	// Use comma-delimited matching to prevent substring false positives.
	dependents, err := s.scanTasks(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks
		 WHERE (',' || blocked_by || ',') LIKE ('%,' || ? || ',%')
		   AND status IN ('pending', 'blocked')`,
		taskID)
	if err != nil {
		return nil, fmt.Errorf("finding dependents: %w", err)
	}
	if len(dependents) == 0 {
		return nil, nil
	}

	// Cancel them.
	_, err = s.db.ExecContext(ctx,
		`UPDATE team_tasks SET status = 'cancelled', error_message = 'parent_cancelled', updated_at = ?
		 WHERE (',' || blocked_by || ',') LIKE ('%,' || ? || ',%')
		   AND status IN ('pending', 'blocked')`,
		now, taskID)
	if err != nil {
		return nil, fmt.Errorf("cancelling dependents: %w", err)
	}

	// Update returned task snapshots.
	for i := range dependents {
		dependents[i].Status = "cancelled"
		dependents[i].ErrorMessage = "parent_cancelled"
	}
	return dependents, nil
}

func (s *Store) FindDuplicateTask(ctx context.Context, teamID, title, assignee string) (*store.TeamTask, error) {
	var t store.TeamTask
	var completedAt sql.NullString
	var claimedAt sql.NullString
	var parentID sql.NullString
	var requireApproval int
	var createdAt, updatedAt string

	err := s.db.QueryRowContext(ctx,
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), parent_id,
			priority, COALESCE(owner_agent_id,''), COALESCE(batch_id,''), task_number,
			COALESCE(identifier,''), progress_percent, require_approval,
			COALESCE(session_id,''), retry_count, max_retries, COALESCE(error_message,''),
			COALESCE(subtask_count,0), COALESCE(dispatch_attempts, 0), claimed_at, completed_at, created_at, updated_at
		 FROM team_tasks
		 WHERE team_id = ? AND title = ? AND COALESCE(assigned_to,'') = ?
		   AND status IN ('pending', 'in_progress', 'blocked')
		   AND created_at > datetime('now', '-5 minutes')
		 LIMIT 1`,
		teamID, title, assignee).Scan(
		&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status, &t.AssignedTo,
		&t.CreatedBy, &t.Result, &t.BlockedBy, &parentID,
		&t.Priority, &t.OwnerAgentID, &t.BatchID, &t.TaskNumber,
		&t.Identifier, &t.ProgressPercent, &requireApproval,
		&t.SessionID, &t.RetryCount, &t.MaxRetries, &t.ErrorMessage,
		&t.SubtaskCount, &t.DispatchAttempts, &claimedAt, &completedAt, &createdAt, &updatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find duplicate task: %w", err)
	}
	if parentID.Valid {
		t.ParentID = parentID.String
	}
	t.RequireApproval = requireApproval == 1
	if claimedAt.Valid {
		ca, _ := time.Parse(time.RFC3339, claimedAt.String)
		t.ClaimedAt = &ca
	}
	if completedAt.Valid {
		ct, _ := time.Parse(time.RFC3339, completedAt.String)
		t.CompletedAt = &ct
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &t, nil
}

// --- Helpers ---

func (s *Store) scanTasks(ctx context.Context, query string, args ...any) ([]store.TeamTask, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []store.TeamTask
	for rows.Next() {
		var t store.TeamTask
		var completedAt sql.NullString
		var claimedAt sql.NullString
		var parentID sql.NullString
		var requireApproval int
		var createdAt, updatedAt string

		if err := rows.Scan(&t.ID, &t.TeamID, &t.Title, &t.Description, &t.Status,
			&t.AssignedTo, &t.CreatedBy, &t.Result, &t.BlockedBy, &parentID,
			&t.Priority, &t.OwnerAgentID, &t.BatchID, &t.TaskNumber,
			&t.Identifier, &t.ProgressPercent, &requireApproval,
			&t.SessionID, &t.RetryCount, &t.MaxRetries, &t.ErrorMessage,
			&t.SubtaskCount, &t.DispatchAttempts, &claimedAt, &completedAt, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if parentID.Valid {
			t.ParentID = parentID.String
		}
		t.RequireApproval = requireApproval == 1
		if claimedAt.Valid {
			ca, _ := time.Parse(time.RFC3339, claimedAt.String)
			t.ClaimedAt = &ca
		}
		if completedAt.Valid {
			ct, _ := time.Parse(time.RFC3339, completedAt.String)
			t.CompletedAt = &ct
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) IncrementSubtaskCount(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET subtask_count = subtask_count + 1 WHERE id = ?`, taskID)
	return err
}

func (s *Store) DecrementSubtaskCount(ctx context.Context, taskID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE team_tasks SET subtask_count = MAX(0, subtask_count - 1) WHERE id = ?`, taskID)
	return err
}

func (s *Store) DeleteTask(ctx context.Context, taskID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check terminal state and get parent_id within the transaction.
	var parentID sql.NullString
	var status string
	err = tx.QueryRowContext(ctx,
		`SELECT status, parent_id FROM team_tasks WHERE id = ?`, taskID).Scan(&status, &parentID)
	if err == sql.ErrNoRows {
		return fmt.Errorf("task not found")
	}
	if err != nil {
		return fmt.Errorf("checking task: %w", err)
	}
	if status != "completed" && status != "cancelled" && status != "failed" {
		return fmt.Errorf("task is not in terminal state (status: %s)", status)
	}

	// Delete comments first (no FK cascade in SQLite).
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_task_comments WHERE task_id = ?`, taskID); err != nil {
		return fmt.Errorf("deleting comments: %w", err)
	}

	// Delete the task (terminal guard in WHERE for defense-in-depth).
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM team_tasks WHERE id = ? AND status IN ('completed','cancelled','failed')`,
		taskID); err != nil {
		return fmt.Errorf("deleting task: %w", err)
	}

	// Decrement parent's subtask_count if this task had a parent.
	if parentID.Valid && parentID.String != "" {
		if _, err := tx.ExecContext(ctx,
			`UPDATE team_tasks SET subtask_count = MAX(0, subtask_count - 1) WHERE id = ?`,
			parentID.String); err != nil {
			return fmt.Errorf("decrementing parent subtask_count: %w", err)
		}
	}

	return tx.Commit()
}

func (s *Store) DeleteTerminalTasks(ctx context.Context, teamID string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Decrement subtask_count for each parent atomically (single UPDATE per parent).
	rows, err := tx.QueryContext(ctx,
		`SELECT parent_id, COUNT(*) FROM team_tasks
		 WHERE team_id = ? AND status IN ('completed','cancelled','failed')
		   AND parent_id IS NOT NULL AND parent_id != ''
		 GROUP BY parent_id`, teamID)
	if err != nil {
		return 0, fmt.Errorf("querying parents: %w", err)
	}
	type parentCount struct {
		id    string
		count int
	}
	var parents []parentCount
	for rows.Next() {
		var pc parentCount
		if err := rows.Scan(&pc.id, &pc.count); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scanning parent: %w", err)
		}
		parents = append(parents, pc)
	}
	rows.Close()

	for _, pc := range parents {
		if _, err := tx.ExecContext(ctx,
			`UPDATE team_tasks SET subtask_count = MAX(0, subtask_count - ?) WHERE id = ?`,
			pc.count, pc.id); err != nil {
			return 0, fmt.Errorf("decrementing parent %s subtask_count: %w", pc.id, err)
		}
	}

	// Delete comments for terminal tasks.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM team_task_comments WHERE task_id IN
		 (SELECT id FROM team_tasks WHERE team_id = ? AND status IN ('completed','cancelled','failed'))`,
		teamID); err != nil {
		return 0, fmt.Errorf("deleting comments: %w", err)
	}

	// Delete the terminal tasks.
	result, err := tx.ExecContext(ctx,
		`DELETE FROM team_tasks WHERE team_id = ? AND status IN ('completed','cancelled','failed')`,
		teamID)
	if err != nil {
		return 0, fmt.Errorf("deleting terminal tasks: %w", err)
	}
	affected, _ := result.RowsAffected()

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}
	return int(affected), nil
}

func splitBlockers(blockedBy string) []string {
	if blockedBy == "" {
		return nil
	}
	parts := strings.Split(blockedBy, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// nilIfEmpty is defined in mcp.go.
