package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// Type alias for backward compatibility.
type CronJob = store.CronJob

// CreateCronJob creates a new cron job and returns its ID.
func (s *Store) CreateCronJob(ctx context.Context, agentID, schedule, prompt string) (string, error) {
	id := newID()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO cron_jobs (id, agent_id, schedule, prompt) VALUES (?, ?, ?, ?)`,
		id, agentID, schedule, prompt,
	)
	if err != nil {
		return "", fmt.Errorf("inserting cron job: %w", err)
	}
	return id, nil
}

// ListCronJobs returns all cron jobs.
func (s *Store) ListCronJobs(ctx context.Context) ([]CronJob, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, agent_id, schedule, prompt, enabled FROM cron_jobs ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("listing cron jobs: %w", err)
	}
	defer rows.Close()

	var jobs []CronJob
	for rows.Next() {
		var j CronJob
		var enabled int
		if err := rows.Scan(&j.ID, &j.AgentID, &j.Schedule, &j.Prompt, &enabled); err != nil {
			return nil, fmt.Errorf("scanning cron job: %w", err)
		}
		j.Enabled = enabled == 1
		jobs = append(jobs, j)
	}
	return jobs, rows.Err()
}

// GetCronJob returns a single cron job by ID.
func (s *Store) GetCronJob(ctx context.Context, id string) (*CronJob, error) {
	var j CronJob
	var enabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, agent_id, schedule, prompt, enabled FROM cron_jobs WHERE id = ?`, id,
	).Scan(&j.ID, &j.AgentID, &j.Schedule, &j.Prompt, &enabled)
	if err != nil {
		return nil, fmt.Errorf("cron job not found: %w", err)
	}
	j.Enabled = enabled == 1
	return &j, nil
}

// UpdateCronJob updates schedule and/or prompt for a cron job.
// Pass nil to leave a field unchanged.
func (s *Store) UpdateCronJob(ctx context.Context, id string, schedule, prompt *string) error {
	if schedule == nil && prompt == nil {
		return fmt.Errorf("nothing to update: provide schedule or prompt")
	}

	var sets []string
	var args []any
	if schedule != nil {
		sets = append(sets, "schedule = ?")
		args = append(args, *schedule)
	}
	if prompt != nil {
		sets = append(sets, "prompt = ?")
		args = append(args, *prompt)
	}
	args = append(args, id)

	query := "UPDATE cron_jobs SET " + sets[0]
	for _, s := range sets[1:] {
		query += ", " + s
	}
	query += " WHERE id = ?"

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating cron job: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("cron job not found: %s", id)
	}
	return nil
}

// GetCronLastRun returns the last run time for a cron job.
func (s *Store) GetCronLastRun(ctx context.Context, id string) (time.Time, error) {
	var lastRun *string
	err := s.db.QueryRowContext(ctx, `SELECT last_run FROM cron_jobs WHERE id = ?`, id).Scan(&lastRun)
	if err != nil {
		return time.Time{}, fmt.Errorf("querying cron last_run: %w", err)
	}
	if lastRun == nil {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, *lastRun)
	if err != nil {
		return time.Time{}, nil
	}
	return t, nil
}

// UpdateCronLastRun updates the last run timestamp for a cron job.
func (s *Store) UpdateCronLastRun(ctx context.Context, id string, t time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE cron_jobs SET last_run = ? WHERE id = ?`,
		t.Format(time.RFC3339), id,
	)
	return err
}

// DeleteCronJob deletes a cron job by ID.
func (s *Store) DeleteCronJob(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM cron_jobs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting cron job: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("cron job not found: %s", id)
	}
	return nil
}
