package sqlite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// Type alias for backward compatibility.
type Memory = store.Memory

func contentHash(content string) string {
	h := sha256.Sum256([]byte(strings.TrimSpace(strings.ToLower(content))))
	return hex.EncodeToString(h[:])
}

// WriteMemory stores a new memory. Returns the ID and whether it was a duplicate.
func (s *Store) WriteMemory(ctx context.Context, content, title string, tags []string) (string, bool, error) {
	id := newID()
	hash := contentHash(content)

	if title == "" && len(content) > 0 {
		title = content
		if len(title) > 80 {
			title = title[:80]
		}
	}

	if tags == nil {
		tags = []string{}
	}
	tagsJSON, _ := json.Marshal(tags)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO memories (id, title, content, tags, content_hash, created_at, updated_at, accessed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, title, content, string(tagsJSON), hash, now, now, now,
	)
	if err != nil {
		// Check for duplicate via content_hash unique constraint.
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			var existingID string
			s.db.QueryRowContext(ctx,
				`SELECT id FROM memories WHERE content_hash = ?`, hash,
			).Scan(&existingID)
			return existingID, true, nil
		}
		return "", false, fmt.Errorf("inserting memory: %w", err)
	}
	return id, false, nil
}

// GetMemory retrieves a memory by ID and increments access count.
func (s *Store) GetMemory(ctx context.Context, id string) (*Memory, error) {
	m := &Memory{}
	var tagsJSON, createdAt, updatedAt, accessedAt string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, title, content, tags, content_hash, created_at, updated_at, accessed_at, access_count
		 FROM memories WHERE id = ?`, id,
	).Scan(&m.ID, &m.Title, &m.Content, &tagsJSON, &m.ContentHash,
		&createdAt, &updatedAt, &accessedAt, &m.AccessCount)
	if err != nil {
		return nil, fmt.Errorf("querying memory: %w", err)
	}

	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	m.AccessedAt, _ = time.Parse(time.RFC3339, accessedAt)
	json.Unmarshal([]byte(tagsJSON), &m.Tags)

	// Update access tracking.
	now := time.Now().UTC().Format(time.RFC3339)
	s.db.ExecContext(ctx,
		`UPDATE memories SET accessed_at = ?, access_count = access_count + 1 WHERE id = ?`,
		now, id,
	)

	return m, nil
}

// DeleteMemory removes a memory by ID.
func (s *Store) DeleteMemory(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM memories WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting memory: %w", err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("memory not found: %s", id)
	}
	return nil
}

// ListMemories returns memories optionally filtered by tags.
func (s *Store) ListMemories(ctx context.Context, filterTags []string, limit, offset int) ([]Memory, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}

	query := `SELECT id, title, content, tags, content_hash, created_at, updated_at, accessed_at, access_count
		FROM memories`
	var args []any

	if len(filterTags) > 0 {
		// Use json_each with OR + HAVING for AND logic across tags.
		var placeholders []string
		for _, tag := range filterTags {
			placeholders = append(placeholders, "?")
			args = append(args, tag)
		}
		query = `SELECT m.id, m.title, m.content, m.tags, m.content_hash,
			m.created_at, m.updated_at, m.accessed_at, m.access_count
			FROM memories m, json_each(m.tags)
			WHERE json_each.value IN (` + strings.Join(placeholders, ", ") + `)
			GROUP BY m.id HAVING COUNT(DISTINCT json_each.value) = ?`
		args = append(args, len(filterTags))
	}

	query += ` ORDER BY updated_at DESC LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		m := Memory{}
		var tagsJSON, createdAt, updatedAt, accessedAt string
		if err := rows.Scan(&m.ID, &m.Title, &m.Content, &tagsJSON, &m.ContentHash,
			&createdAt, &updatedAt, &accessedAt, &m.AccessCount); err != nil {
			return nil, fmt.Errorf("scanning memory: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		m.AccessedAt, _ = time.Parse(time.RFC3339, accessedAt)
		json.Unmarshal([]byte(tagsJSON), &m.Tags)
		memories = append(memories, m)
	}
	return memories, rows.Err()
}

// SearchMemories performs a FTS5 BM25 search. Returns memories ranked by relevance.
func (s *Store) SearchMemories(ctx context.Context, query string, limit int) ([]Memory, []float64, error) {
	if limit <= 0 {
		limit = 10
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT m.id, m.title, m.content, m.tags, m.content_hash,
			m.created_at, m.updated_at, m.accessed_at, m.access_count,
			bm25(memories_fts) AS score
		 FROM memories_fts fts
		 JOIN memories m ON m.rowid = fts.rowid
		 WHERE memories_fts MATCH ?
		 ORDER BY score
		 LIMIT ?`,
		query, limit,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("searching memories: %w", err)
	}
	defer rows.Close()

	var memories []Memory
	var scores []float64
	for rows.Next() {
		m := Memory{}
		var tagsJSON, createdAt, updatedAt, accessedAt string
		var score float64
		if err := rows.Scan(&m.ID, &m.Title, &m.Content, &tagsJSON, &m.ContentHash,
			&createdAt, &updatedAt, &accessedAt, &m.AccessCount, &score); err != nil {
			return nil, nil, fmt.Errorf("scanning memory: %w", err)
		}
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		m.AccessedAt, _ = time.Parse(time.RFC3339, accessedAt)
		json.Unmarshal([]byte(tagsJSON), &m.Tags)
		memories = append(memories, m)
		scores = append(scores, score)
	}
	return memories, scores, rows.Err()
}
