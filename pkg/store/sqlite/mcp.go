package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

// UpsertMCPEntry inserts or updates an MCP registry entry.
func (s *Store) UpsertMCPEntry(ctx context.Context, entry store.MCPRegistryEntry) error {
	tags, _ := json.Marshal(entry.Tags)
	agents, _ := json.Marshal(entry.AgentIDs)

	var installedAt sql.NullString
	if entry.InstalledAt != nil {
		installedAt = sql.NullString{String: entry.InstalledAt.Format(time.RFC3339), Valid: true}
	}

	status := entry.Status
	if status == "" {
		status = "available"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_registry (id, name, description, category, connection,
			fallback_conn, config_schema, github_url, stars, tags, source,
			installed, enabled, status, status_error, agent_ids, installed_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			description = excluded.description,
			category = excluded.category,
			connection = excluded.connection,
			fallback_conn = excluded.fallback_conn,
			config_schema = excluded.config_schema,
			github_url = excluded.github_url,
			stars = excluded.stars,
			tags = excluded.tags,
			source = excluded.source,
			installed = excluded.installed,
			enabled = excluded.enabled,
			status = excluded.status,
			status_error = excluded.status_error,
			agent_ids = excluded.agent_ids,
			installed_at = excluded.installed_at,
			updated_at = datetime('now')`,
		entry.ID, entry.Name, entry.Description, entry.Category,
		entry.Connection, nilIfEmpty(entry.FallbackConn),
		nilIfEmpty(entry.ConfigSchema), entry.GitHubURL, entry.Stars,
		string(tags), entry.Source,
		boolToInt(entry.Installed), boolToInt(entry.Enabled),
		status, entry.StatusError,
		string(agents), installedAt,
	)
	return err
}

// GetMCPEntry returns a single MCP registry entry by ID.
func (s *Store) GetMCPEntry(ctx context.Context, id string) (*store.MCPRegistryEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, description, category, connection, fallback_conn,
			config_schema, github_url, stars, tags, source, installed, enabled,
			status, status_error, agent_ids, installed_at, updated_at
		FROM mcp_registry WHERE id = ?`, id)
	return scanMCPEntry(row)
}

// DeleteMCPEntry removes an MCP entry from the registry.
func (s *Store) DeleteMCPEntry(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM mcp_registry WHERE id = ?", id)
	return err
}

// DeleteMCPCredential removes a credential by key (actual DELETE, not overwrite).
func (s *Store) DeleteMCPCredential(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM credentials WHERE key = ?", name)
	return err
}

// ListMCPEntries returns filtered MCP registry entries.
func (s *Store) ListMCPEntries(ctx context.Context, filter store.MCPFilter) ([]store.MCPRegistryEntry, error) {
	query := "SELECT id, name, description, category, connection, fallback_conn, config_schema, github_url, stars, tags, source, installed, enabled, status, status_error, agent_ids, installed_at, updated_at FROM mcp_registry WHERE 1=1"
	var args []any

	if filter.Category != "" {
		query += " AND category = ?"
		args = append(args, filter.Category)
	}
	if filter.Installed != nil {
		query += " AND installed = ?"
		args = append(args, boolToInt(*filter.Installed))
	}
	if filter.Enabled != nil {
		query += " AND enabled = ?"
		args = append(args, boolToInt(*filter.Enabled))
	}
	if len(filter.Status) > 0 {
		placeholders := ""
		for i, st := range filter.Status {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, st)
		}
		query += " AND status IN (" + placeholders + ")"
	}
	if filter.Source != "" {
		query += " AND source = ?"
		args = append(args, filter.Source)
	}
	if filter.Query != "" {
		query += " AND (name LIKE ? OR description LIKE ? OR tags LIKE ?)"
		q := "%" + filter.Query + "%"
		args = append(args, q, q, q)
	}

	query += " ORDER BY stars DESC, name ASC"

	if filter.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, filter.Limit)
		if filter.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, filter.Offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []store.MCPRegistryEntry
	for rows.Next() {
		e, err := scanMCPEntryRow(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}
	return entries, rows.Err()
}

// SearchMCPEntries searches by name, description, and tags.
func (s *Store) SearchMCPEntries(ctx context.Context, query string, limit int) ([]store.MCPRegistryEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.ListMCPEntries(ctx, store.MCPFilter{Query: query, Limit: limit})
}

// SetMCPStatus is the sole writer for MCP lifecycle state transitions.
// It updates status, status_error, and derives installed/enabled booleans.
func (s *Store) SetMCPStatus(ctx context.Context, id, status, statusError string) error {
	installed := 0
	enabled := 0
	switch status {
	case "connected":
		installed = 1
		enabled = 1
	case "disabled":
		installed = 1
		enabled = 0
	}

	// Set installed_at on first transition to connected.
	if status == "connected" {
		_, err := s.db.ExecContext(ctx, `
			UPDATE mcp_registry SET
				status = ?, status_error = ?,
				installed = ?, enabled = ?,
				installed_at = CASE WHEN installed_at IS NULL OR installed_at = '' THEN datetime('now') ELSE installed_at END,
				updated_at = datetime('now')
			WHERE id = ?`,
			status, statusError, installed, enabled, id)
		return err
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE mcp_registry SET
			status = ?, status_error = ?,
			installed = ?, enabled = ?,
			updated_at = datetime('now')
		WHERE id = ?`,
		status, statusError, installed, enabled, id)
	return err
}

// SetMCPAgents updates the agent assignment list.
func (s *Store) SetMCPAgents(ctx context.Context, id string, agentIDs []string) error {
	agents, _ := json.Marshal(agentIDs)
	_, err := s.db.ExecContext(ctx, `
		UPDATE mcp_registry SET agent_ids = ?, updated_at = datetime('now')
		WHERE id = ?`, string(agents), id)
	return err
}

// CountMCPByCategory returns installed counts per category.
func (s *Store) CountMCPByCategory(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT category, COUNT(*) FROM mcp_registry
		WHERE status IN ('connected', 'disabled') GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var cat string
		var count int
		if err := rows.Scan(&cat, &count); err != nil {
			return nil, err
		}
		counts[cat] = count
	}
	return counts, rows.Err()
}

// CountMCPInstalled returns the total number of installed MCPs.
func (s *Store) CountMCPInstalled(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM mcp_registry WHERE status IN ('connected', 'disabled')").Scan(&count)
	return count, err
}

// GetMCPSeedVersion returns the current curated index version that was seeded.
func (s *Store) GetMCPSeedVersion(ctx context.Context) (int, error) {
	var val string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM mcp_seed_meta WHERE key = 'version'").Scan(&val)
	if err != nil {
		return 0, err
	}
	v, _ := strconv.Atoi(val)
	return v, nil
}

// SetMCPSeedVersion records the curated index version that was seeded.
func (s *Store) SetMCPSeedVersion(ctx context.Context, version int) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO mcp_seed_meta (key, value) VALUES ('version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		strconv.Itoa(version))
	return err
}

// --- Helpers ---

type mcpScanner interface {
	Scan(dest ...any) error
}

func scanMCPEntry(row *sql.Row) (*store.MCPRegistryEntry, error) {
	return scanMCPFrom(row)
}

func scanMCPEntryRow(rows *sql.Rows) (*store.MCPRegistryEntry, error) {
	return scanMCPFrom(rows)
}

func scanMCPFrom(s mcpScanner) (*store.MCPRegistryEntry, error) {
	var e store.MCPRegistryEntry
	var tagsJSON, agentsJSON string
	var fallbackConn, configSchema sql.NullString
	var installed, enabled int
	var statusError sql.NullString
	var installedAt, updatedAt sql.NullString

	err := s.Scan(
		&e.ID, &e.Name, &e.Description, &e.Category,
		&e.Connection, &fallbackConn, &configSchema,
		&e.GitHubURL, &e.Stars, &tagsJSON, &e.Source,
		&installed, &enabled, &e.Status, &statusError,
		&agentsJSON, &installedAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	e.Installed = installed == 1
	e.Enabled = enabled == 1
	if statusError.Valid {
		e.StatusError = statusError.String
	}
	if fallbackConn.Valid {
		e.FallbackConn = fallbackConn.String
	}
	if configSchema.Valid {
		e.ConfigSchema = configSchema.String
	}
	json.Unmarshal([]byte(tagsJSON), &e.Tags)
	json.Unmarshal([]byte(agentsJSON), &e.AgentIDs)
	if installedAt.Valid {
		for _, fmt := range []string{time.RFC3339, "2006-01-02 15:04:05"} {
			if t, err := time.Parse(fmt, installedAt.String); err == nil {
				e.InstalledAt = &t
				break
			}
		}
	}
	if updatedAt.Valid {
		if t, err := time.Parse("2006-01-02 15:04:05", updatedAt.String); err == nil {
			e.UpdatedAt = t
		}
	}
	return &e, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

