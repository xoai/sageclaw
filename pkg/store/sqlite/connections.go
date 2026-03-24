package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func (s *Store) CreateConnection(ctx context.Context, conn store.Connection) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO connections (id, platform, agent_id, label, metadata, credential_key, credentials, dm_enabled, group_enabled, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		conn.ID, conn.Platform, conn.AgentID, conn.Label, conn.Metadata, conn.CredentialKey, conn.Credentials,
		boolToInt(conn.DmEnabled), boolToInt(conn.GroupEnabled), conn.Status)
	if err != nil {
		return fmt.Errorf("creating connection: %w", err)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) GetConnection(ctx context.Context, id string) (*store.Connection, error) {
	var c store.Connection
	var agentID, createdAt, updatedAt string
	var dmEnabled, groupEnabled int
	err := s.db.QueryRowContext(ctx,
		`SELECT id, platform, COALESCE(agent_id,''), label, metadata, credential_key,
		        COALESCE(credentials, X''), COALESCE(dm_enabled,1), COALESCE(group_enabled,1),
		        status, created_at, updated_at
		 FROM connections WHERE id = ?`, id).
		Scan(&c.ID, &c.Platform, &agentID, &c.Label, &c.Metadata, &c.CredentialKey,
			&c.Credentials, &dmEnabled, &groupEnabled,
			&c.Status, &createdAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("connection not found: %w", err)
	}
	c.AgentID = agentID
	c.DmEnabled = dmEnabled != 0
	c.GroupEnabled = groupEnabled != 0
	c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	c.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	return &c, nil
}

func (s *Store) ListConnections(ctx context.Context, filter store.ConnectionFilter) ([]store.Connection, error) {
	query := `SELECT id, platform, COALESCE(agent_id,''), label, metadata, credential_key,
		COALESCE(credentials, X''), COALESCE(dm_enabled,1), COALESCE(group_enabled,1),
		status, created_at, updated_at
		FROM connections WHERE 1=1`
	var args []any

	if filter.Platform != "" {
		query += ` AND platform = ?`
		args = append(args, filter.Platform)
	}
	if filter.Status != "" {
		query += ` AND status = ?`
		args = append(args, filter.Status)
	}
	if filter.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, filter.AgentID)
	}
	query += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing connections: %w", err)
	}
	defer rows.Close()

	var conns []store.Connection
	for rows.Next() {
		var c store.Connection
		var agentID, createdAt, updatedAt string
		var dmEnabled, groupEnabled int
		if err := rows.Scan(&c.ID, &c.Platform, &agentID, &c.Label, &c.Metadata, &c.CredentialKey,
			&c.Credentials, &dmEnabled, &groupEnabled,
			&c.Status, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scanning connection: %w", err)
		}
		c.AgentID = agentID
		c.DmEnabled = dmEnabled != 0
		c.GroupEnabled = groupEnabled != 0
		c.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
		c.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
		conns = append(conns, c)
	}
	return conns, nil
}

func (s *Store) UpdateConnection(ctx context.Context, id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}

	query := `UPDATE connections SET updated_at = datetime('now')`
	var args []any

	for key, val := range fields {
		switch key {
		case "agent_id", "label", "metadata", "status", "credentials", "dm_enabled", "group_enabled":
			query += fmt.Sprintf(`, %s = ?`, key)
			args = append(args, val)
		default:
			return fmt.Errorf("unknown connection field: %s", key)
		}
	}
	query += ` WHERE id = ?`
	args = append(args, id)

	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("updating connection: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("connection not found: %s", id)
	}
	return nil
}

func (s *Store) DeleteConnection(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM connections WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("deleting connection: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("connection not found: %s", id)
	}
	return nil
}
