package tool

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// DefaultCooldown is the time after a deny before the same group can be re-prompted.
const DefaultCooldown = 60 * time.Second

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
// Session-scoped decisions (allow-once) stay in-memory for speed.
// "Always allow" decisions are persisted to survive restarts.
// Denials are tracked as cooldown timestamps (soft deny).
type PersistentConsentStore struct {
	mu       sync.RWMutex
	session  map[string]map[string]bool      // sessionID -> group -> granted
	cooldown map[string]map[string]time.Time // sessionID -> group -> deny timestamp
	db       *sql.DB
}

// NewPersistentConsentStore creates a consent store backed by SQLite.
func NewPersistentConsentStore(db *sql.DB) *PersistentConsentStore {
	return &PersistentConsentStore{
		session:  make(map[string]map[string]bool),
		cooldown: make(map[string]map[string]time.Time),
		db:       db,
	}
}

// HasConsent checks in order: persistent "always" -> session-scoped grant -> not consented.
func (s *PersistentConsentStore) HasConsent(sessionID, ownerID, platform, group string) bool {
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

// GrantOnce records session-scoped consent.
func (s *PersistentConsentStore) GrantOnce(sessionID, group string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session[sessionID] == nil {
		s.session[sessionID] = make(map[string]bool)
	}
	s.session[sessionID][group] = true
}

// GrantAlways persists consent to SQLite (survives restarts).
// Uses upsert to prevent duplicate rows under concurrent calls.
func (s *PersistentConsentStore) GrantAlways(ownerID, platform, group string) error {
	if s.db == nil {
		return fmt.Errorf("no database for persistent consent")
	}

	_, err := s.db.Exec(
		`INSERT INTO consent_grants (id, owner_id, platform, tool_group)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(owner_id, platform, tool_group)
		 DO UPDATE SET revoked_at = NULL, granted_at = datetime('now')`,
		uuid.NewString(), ownerID, platform, group)
	if err != nil {
		return fmt.Errorf("upserting consent grant: %w", err)
	}
	return nil
}

// Deny records a cooldown timestamp for a group. The agent can re-ask after the cooldown expires.
func (s *PersistentConsentStore) Deny(sessionID, group string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cooldown[sessionID] == nil {
		s.cooldown[sessionID] = make(map[string]time.Time)
	}
	s.cooldown[sessionID][group] = time.Now()
}

// InCooldown returns true if the group was denied within the cooldown window.
func (s *PersistentConsentStore) InCooldown(sessionID, group string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	deniedAt, ok := s.cooldown[sessionID][group]
	if !ok {
		return false
	}
	return time.Since(deniedAt) < DefaultCooldown
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

// ClearSession removes all session-scoped consent and cooldown records.
func (s *PersistentConsentStore) ClearSession(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.session, sessionID)
	delete(s.cooldown, sessionID)
}

// InvalidateSessionGrants clears session grants for groups not in the new profile.
// Always-consent group grants are preserved (they're valid across profile changes).
func (s *PersistentConsentStore) InvalidateSessionGrants(sessionID string, newProfileGroups map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grants := s.session[sessionID]
	for group := range grants {
		if !newProfileGroups[group] && !AlwaysConsentGroups[group] {
			delete(grants, group)
		}
	}
}
