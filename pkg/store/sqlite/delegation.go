package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func (s *Store) GetDelegationLinks(ctx context.Context, sourceAgentID string) ([]store.DelegationLink, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, source_id, target_id, direction, max_concurrent FROM delegation_links WHERE source_id = ?`,
		sourceAgentID)
	if err != nil {
		return nil, fmt.Errorf("querying delegation links: %w", err)
	}
	defer rows.Close()

	var links []store.DelegationLink
	for rows.Next() {
		var l store.DelegationLink
		if err := rows.Scan(&l.ID, &l.SourceID, &l.TargetID, &l.Direction, &l.MaxConcurrent); err != nil {
			return nil, err
		}
		links = append(links, l)
	}
	return links, rows.Err()
}

func (s *Store) IncrementDelegation(ctx context.Context, linkID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO delegation_state (link_id, active_count) VALUES (?, 1)
		 ON CONFLICT(link_id) DO UPDATE SET active_count = active_count + 1`, linkID)
	return err
}

func (s *Store) DecrementDelegation(ctx context.Context, linkID string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE delegation_state SET active_count = MAX(0, active_count - 1) WHERE link_id = ?`, linkID)
	return err
}

func (s *Store) GetDelegationCount(ctx context.Context, linkID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE((SELECT active_count FROM delegation_state WHERE link_id = ?), 0)`, linkID).Scan(&count)
	return count, err
}

func (s *Store) RecordDelegation(ctx context.Context, entry store.DelegationRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO delegation_history (id, link_id, source_id, target_id, prompt, result, status, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.LinkID, entry.SourceID, entry.TargetID, entry.Prompt,
		entry.Result, entry.Status, entry.StartedAt.Format(time.RFC3339),
		nilTime(entry.CompletedAt))
	return err
}

func (s *Store) GetDelegationHistory(ctx context.Context, agentID string, limit int) ([]store.DelegationRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, link_id, source_id, target_id, prompt, COALESCE(result,''), status, started_at, completed_at
		 FROM delegation_history WHERE source_id = ? OR target_id = ? ORDER BY started_at DESC LIMIT ?`,
		agentID, agentID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []store.DelegationRecord
	for rows.Next() {
		var r store.DelegationRecord
		var startedAt, completedAt *string
		if err := rows.Scan(&r.ID, &r.LinkID, &r.SourceID, &r.TargetID, &r.Prompt,
			&r.Result, &r.Status, &startedAt, &completedAt); err != nil {
			return nil, err
		}
		if startedAt != nil {
			r.StartedAt, _ = time.Parse(time.RFC3339, *startedAt)
		}
		if completedAt != nil && *completedAt != "" {
			t, _ := time.Parse(time.RFC3339, *completedAt)
			r.CompletedAt = &t
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func nilTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}
