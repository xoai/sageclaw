package tool

import (
	"database/sql"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

// ConsentGrant represents a persistent "always allow" consent grant.
type ConsentGrant struct {
	ID         string
	OwnerID    string
	Platform   string
	ToolGroup  string
	GrantedAt  string
	RevokedAt  string // empty if active
}

// PersistentConsentStore backs consent decisions with SQLite.
// Session-scoped decisions (allow-once, deny) stay in-memory for speed.
// "Always allow" decisions are persisted to survive restarts.
type PersistentConsentStore struct {
	mu      sync.RWMutex
	session map[string]map[string]bool // sessionID -> group -> granted
	denied  map[string]map[string]bool // sessionID -> group -> denied (separate from grants)
	db      *sql.DB
}

// NewPersistentConsentStore creates a consent store backed by SQLite.
func NewPersistentConsentStore(db *sql.DB) *PersistentConsentStore {
	return &PersistentConsentStore{
		session: make(map[string]map[string]bool),
		denied:  make(map[string]map[string]bool),
		db:      db,
	}
}

// HasConsent checks in order: safe auto-consent -> persistent "always" ->
// session-scoped grant -> not consented.
func (s *PersistentConsentStore) HasConsent(sessionID, ownerID, platform, group string) bool {
	// Safe tools never need consent.
	if GroupRisk[group] == RiskSafe {
		return true
	}

	// Check persistent "always allow" grants.
	if ownerID != "" && platform != "" && s.db != nil {
		var count int
		err := s.db.QueryRow(
			`SELECT COUNT(*) FROM consent_grants
			 WHERE owner_id = ? AND platform = ? AND tool_group = ? AND revoked_at IS NULL`,
			ownerID, platform, group).Scan(&count)
		if err == nil && count > 0 {
			return true
		}
	}

	// Check session-scoped grant.
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups, ok := s.session[sessionID]
	if !ok {
		return false
	}
	return groups[group]
}

// GrantOnce records session-scoped consent (current behavior).
func (s *PersistentConsentStore) GrantOnce(sessionID, group string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session[sessionID] == nil {
		s.session[sessionID] = make(map[string]bool)
	}
	s.session[sessionID][group] = true
}

// GrantAlways persists consent to SQLite (survives restarts).
// Upsert: UPDATE existing row on re-grant (preserves row ID for audit), INSERT only if no row.
func (s *PersistentConsentStore) GrantAlways(ownerID, platform, group string) error {
	if s.db == nil {
		return fmt.Errorf("no database for persistent consent")
	}

	// Try update first (re-grant after revoke).
	result, err := s.db.Exec(
		`UPDATE consent_grants SET revoked_at = NULL, granted_at = datetime('now')
		 WHERE owner_id = ? AND platform = ? AND tool_group = ?`,
		ownerID, platform, group)
	if err != nil {
		return fmt.Errorf("updating consent grant: %w", err)
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		return nil
	}

	// Insert new grant.
	_, err = s.db.Exec(
		`INSERT INTO consent_grants (id, owner_id, platform, tool_group)
		 VALUES (?, ?, ?, ?)`,
		uuid.NewString(), ownerID, platform, group)
	if err != nil {
		return fmt.Errorf("inserting consent grant: %w", err)
	}
	return nil
}

// Deny records a session-scoped denial. Denials are NOT persisted.
func (s *PersistentConsentStore) Deny(sessionID, group string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.denied[sessionID] == nil {
		s.denied[sessionID] = make(map[string]bool)
	}
	s.denied[sessionID][group] = true
}

// IsDenied checks if a group was explicitly denied in this session.
func (s *PersistentConsentStore) IsDenied(sessionID, group string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	groups, ok := s.denied[sessionID]
	if !ok {
		return false
	}
	return groups[group]
}

// Revoke removes a persistent "always" grant (sets revoked_at).
func (s *PersistentConsentStore) Revoke(ownerID, platform, group string) error {
	if s.db == nil {
		return fmt.Errorf("no database for persistent consent")
	}
	result, err := s.db.Exec(
		`UPDATE consent_grants SET revoked_at = datetime('now')
		 WHERE owner_id = ? AND platform = ? AND tool_group = ? AND revoked_at IS NULL`,
		ownerID, platform, group)
	if err != nil {
		return fmt.Errorf("revoking consent grant: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active grant found for %s/%s/%s", ownerID, platform, group)
	}
	return nil
}

// RevokeByID removes a persistent grant by its ID.
func (s *PersistentConsentStore) RevokeByID(id string) error {
	if s.db == nil {
		return fmt.Errorf("no database for persistent consent")
	}
	result, err := s.db.Exec(
		`UPDATE consent_grants SET revoked_at = datetime('now')
		 WHERE id = ? AND revoked_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("revoking consent grant: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("no active grant found: %s", id)
	}
	return nil
}

// ListGrants returns all active persistent grants, optionally filtered.
func (s *PersistentConsentStore) ListGrants(ownerID, platform string) ([]ConsentGrant, error) {
	if s.db == nil {
		return nil, fmt.Errorf("no database for persistent consent")
	}

	query := `SELECT id, owner_id, platform, tool_group, granted_at, COALESCE(revoked_at,'')
		FROM consent_grants WHERE revoked_at IS NULL`
	var args []any

	if ownerID != "" {
		query += ` AND owner_id = ?`
		args = append(args, ownerID)
	}
	if platform != "" {
		query += ` AND platform = ?`
		args = append(args, platform)
	}
	query += ` ORDER BY granted_at DESC`

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing consent grants: %w", err)
	}
	defer rows.Close()

	var grants []ConsentGrant
	for rows.Next() {
		var g ConsentGrant
		if err := rows.Scan(&g.ID, &g.OwnerID, &g.Platform, &g.ToolGroup, &g.GrantedAt, &g.RevokedAt); err != nil {
			return nil, fmt.Errorf("scanning consent grant: %w", err)
		}
		grants = append(grants, g)
	}
	return grants, nil
}

// ClearSession removes all session-scoped consent records.
func (s *PersistentConsentStore) ClearSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.session, sessionID)
	delete(s.denied, sessionID)
}
