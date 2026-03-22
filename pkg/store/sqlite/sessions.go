package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// Type aliases for backward compatibility.
type Session = store.Session

// StoredMessage represents a persisted message.
type StoredMessage struct {
	ID        int64
	SessionID string
	Role      string
	Content   []canonical.Content
	CreatedAt time.Time
}

// NewID generates a random 32-char hex ID.
func NewID() string {
	return newID()
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// CreateSession creates a new session or returns an existing one with the same key.
func (s *Store) CreateSession(ctx context.Context, channel, chatID, agentID string) (*Session, error) {
	return s.CreateSessionWithKind(ctx, channel, chatID, agentID, "direct")
}

// CreateSessionWithKind creates a session with a specific kind (direct, subagent, cron).
func (s *Store) CreateSessionWithKind(ctx context.Context, channel, chatID, agentID, kind string) (*Session, error) {
	key := store.SessionKey(agentID, channel, kind, chatID)

	// Check for existing session with this key.
	existing, err := s.FindSessionByKey(ctx, key)
	if err == nil && existing != nil {
		// Update timestamp and return existing.
		s.db.ExecContext(ctx, `UPDATE sessions SET updated_at = datetime('now') WHERE id = ?`, existing.ID)
		existing.UpdatedAt = time.Now().UTC()
		return existing, nil
	}

	// Generate label.
	label := agentID + " on " + channel

	sess := &Session{
		ID:        newID(),
		Key:       key,
		Channel:   channel,
		ChatID:    chatID,
		AgentID:   agentID,
		Kind:      kind,
		Label:     label,
		Status:    "active",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
		Metadata:  map[string]string{},
	}

	metaJSON, _ := json.Marshal(sess.Metadata)
	now := sess.CreatedAt.Format(time.RFC3339)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, key, channel, chat_id, agent_id, kind, label, status, created_at, updated_at, metadata)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sess.ID, sess.Key, sess.Channel, sess.ChatID, sess.AgentID,
		sess.Kind, sess.Label, sess.Status, now, now, string(metaJSON),
	)
	if err != nil {
		return nil, fmt.Errorf("inserting session: %w", err)
	}
	return sess, nil
}

// FindSessionByKey finds a session by its composite key.
func (s *Store) FindSessionByKey(ctx context.Context, key string) (*Session, error) {
	sess := &Session{}
	var metaJSON, createdAt, updatedAt string
	var label, status, model, provider, spawnedBy *string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, key, channel, chat_id, agent_id, kind, label, status, model, provider,
			spawned_by, input_tokens, output_tokens, compaction_count, message_count,
			created_at, updated_at, metadata
		 FROM sessions WHERE key = ?`, key,
	).Scan(&sess.ID, &sess.Key, &sess.Channel, &sess.ChatID, &sess.AgentID,
		&sess.Kind, &label, &status, &model, &provider,
		&spawnedBy, &sess.InputTokens, &sess.OutputTokens, &sess.CompactionCount, &sess.MessageCount,
		&createdAt, &updatedAt, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("querying session by key: %w", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if label != nil { sess.Label = *label }
	if status != nil { sess.Status = *status }
	if model != nil { sess.Model = *model }
	if provider != nil { sess.Provider = *provider }
	if spawnedBy != nil { sess.SpawnedBy = *spawnedBy }
	json.Unmarshal([]byte(metaJSON), &sess.Metadata)
	return sess, nil
}

// UpdateSessionTokens updates token usage after an agent loop iteration.
func (s *Store) UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, model, provider string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sessions SET
			input_tokens = input_tokens + ?,
			output_tokens = output_tokens + ?,
			message_count = (SELECT COUNT(*) FROM messages WHERE session_id = ?),
			model = COALESCE(?, model),
			provider = COALESCE(?, provider),
			updated_at = datetime('now')
		 WHERE id = ?`,
		inputTokens, outputTokens, sessionID, model, provider, sessionID)
	return err
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, id string) (*Session, error) {
	sess := &Session{}
	var metaJSON, createdAt, updatedAt string
	var key, label, status, model, provider, spawnedBy *string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(key,''), channel, chat_id, agent_id, kind, label, status, model, provider,
			spawned_by, input_tokens, output_tokens, compaction_count, message_count,
			created_at, updated_at, metadata
		 FROM sessions WHERE id = ?`, id,
	).Scan(&sess.ID, &key, &sess.Channel, &sess.ChatID, &sess.AgentID,
		&sess.Kind, &label, &status, &model, &provider,
		&spawnedBy, &sess.InputTokens, &sess.OutputTokens, &sess.CompactionCount, &sess.MessageCount,
		&createdAt, &updatedAt, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("querying session: %w", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if key != nil { sess.Key = *key }
	if label != nil { sess.Label = *label }
	if status != nil { sess.Status = *status }
	if model != nil { sess.Model = *model }
	if provider != nil { sess.Provider = *provider }
	if spawnedBy != nil { sess.SpawnedBy = *spawnedBy }
	json.Unmarshal([]byte(metaJSON), &sess.Metadata)
	return sess, nil
}

// FindSession finds an active session by channel and chat ID.
func (s *Store) FindSession(ctx context.Context, channel, chatID string) (*Session, error) {
	return s.getSessionByChat(ctx, channel, chatID)
}

func (s *Store) getSessionByChat(ctx context.Context, channel, chatID string) (*Session, error) {
	sess := &Session{}
	var metaJSON, createdAt, updatedAt string
	var key, label, status, model, provider, spawnedBy *string
	err := s.db.QueryRowContext(ctx,
		`SELECT id, COALESCE(key,''), channel, chat_id, agent_id, kind, label, status, model, provider,
			spawned_by, input_tokens, output_tokens, compaction_count, message_count,
			created_at, updated_at, metadata
		 FROM sessions WHERE channel = ? AND chat_id = ? AND status = 'active' ORDER BY updated_at DESC LIMIT 1`,
		channel, chatID,
	).Scan(&sess.ID, &key, &sess.Channel, &sess.ChatID, &sess.AgentID,
		&sess.Kind, &label, &status, &model, &provider,
		&spawnedBy, &sess.InputTokens, &sess.OutputTokens, &sess.CompactionCount, &sess.MessageCount,
		&createdAt, &updatedAt, &metaJSON)
	if err != nil {
		return nil, fmt.Errorf("querying session by chat: %w", err)
	}

	sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	sess.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	if key != nil { sess.Key = *key }
	if label != nil { sess.Label = *label }
	if status != nil { sess.Status = *status }
	if model != nil { sess.Model = *model }
	if provider != nil { sess.Provider = *provider }
	if spawnedBy != nil { sess.SpawnedBy = *spawnedBy }
	json.Unmarshal([]byte(metaJSON), &sess.Metadata)
	return sess, nil
}

// AppendMessages stores messages for a session.
func (s *Store) AppendMessages(ctx context.Context, sessionID string, msgs []canonical.Message) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO messages (session_id, role, content, created_at) VALUES (?, ?, ?, ?)`,
	)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	for _, msg := range msgs {
		contentJSON, err := json.Marshal(msg.Content)
		if err != nil {
			return fmt.Errorf("marshaling content: %w", err)
		}
		if _, err := stmt.ExecContext(ctx, sessionID, msg.Role, string(contentJSON), now); err != nil {
			return fmt.Errorf("inserting message: %w", err)
		}
	}

	// Update session timestamp and message count.
	if _, err := tx.ExecContext(ctx,
		`UPDATE sessions SET updated_at = ?,
			message_count = (SELECT COUNT(*) FROM messages WHERE session_id = ?)
		 WHERE id = ?`, now, sessionID, sessionID,
	); err != nil {
		return fmt.Errorf("updating session: %w", err)
	}

	return tx.Commit()
}

// GetMessages retrieves messages for a session, ordered by ID.
func (s *Store) GetMessages(ctx context.Context, sessionID string, limit int) ([]canonical.Message, error) {
	query := `SELECT role, content FROM messages WHERE session_id = ? ORDER BY id ASC`
	args := []any{sessionID}
	if limit > 0 {
		// Get last N messages by using a subquery.
		query = `SELECT role, content FROM (
			SELECT role, content, id FROM messages WHERE session_id = ? ORDER BY id DESC LIMIT ?
		) sub ORDER BY id ASC`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying messages: %w", err)
	}
	defer rows.Close()

	var msgs []canonical.Message
	for rows.Next() {
		var role, contentJSON string
		if err := rows.Scan(&role, &contentJSON); err != nil {
			return nil, fmt.Errorf("scanning message: %w", err)
		}
		var content []canonical.Content
		if err := json.Unmarshal([]byte(contentJSON), &content); err != nil {
			return nil, fmt.Errorf("unmarshaling content: %w", err)
		}
		msgs = append(msgs, canonical.Message{Role: role, Content: content})
	}
	return msgs, rows.Err()
}
