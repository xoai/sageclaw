package relay

import (
	"crypto/subtle"
	"database/sql"
	"fmt"
)

// TokenStore validates tunnel tokens and manages subdomain reservations.
type TokenStore interface {
	// Validate checks if a token is valid and returns its subdomain.
	Validate(token string) (subdomain string, ok bool)
	// Register associates a token with a subdomain and instance ID.
	Register(token, subdomain, instanceID string) error
	// Touch updates last_seen for a token.
	Touch(token string) error
	// Cleanup removes tokens inactive for > 30 days.
	Cleanup() error
}

// SQLiteTokenStore persists tokens in a SQLite database (for managed relay).
type SQLiteTokenStore struct {
	db *sql.DB
}

// NewSQLiteTokenStore creates the token store and initializes the schema.
func NewSQLiteTokenStore(dbPath string) (*SQLiteTokenStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open token db: %w", err)
	}

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS tunnel_tokens (
			token       TEXT PRIMARY KEY,
			subdomain   TEXT NOT NULL UNIQUE,
			instance_id TEXT NOT NULL,
			created_at  TEXT NOT NULL DEFAULT (datetime('now')),
			last_seen   TEXT NOT NULL DEFAULT (datetime('now'))
		);
		CREATE INDEX IF NOT EXISTS idx_tunnel_tokens_subdomain ON tunnel_tokens(subdomain);
	`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("init token schema: %w", err)
	}

	return &SQLiteTokenStore{db: db}, nil
}

func (s *SQLiteTokenStore) Validate(token string) (string, bool) {
	var subdomain string
	err := s.db.QueryRow("SELECT subdomain FROM tunnel_tokens WHERE token = ?", token).Scan(&subdomain)
	if err != nil {
		return "", false
	}
	return subdomain, true
}

func (s *SQLiteTokenStore) Register(token, subdomain, instanceID string) error {
	_, err := s.db.Exec(
		`INSERT INTO tunnel_tokens (token, subdomain, instance_id) VALUES (?, ?, ?)
		 ON CONFLICT(token) DO UPDATE SET subdomain = excluded.subdomain, instance_id = excluded.instance_id, last_seen = datetime('now')`,
		token, subdomain, instanceID)
	return err
}

func (s *SQLiteTokenStore) Touch(token string) error {
	_, err := s.db.Exec("UPDATE tunnel_tokens SET last_seen = datetime('now') WHERE token = ?", token)
	return err
}

func (s *SQLiteTokenStore) Cleanup() error {
	_, err := s.db.Exec("DELETE FROM tunnel_tokens WHERE last_seen < datetime('now', '-30 days')")
	return err
}

func (s *SQLiteTokenStore) Close() error {
	return s.db.Close()
}

// SharedSecretTokenStore validates against a single shared secret (for self-hosted relay).
type SharedSecretTokenStore struct {
	secret string
}

// NewSharedSecretTokenStore creates a store that validates a single shared secret.
func NewSharedSecretTokenStore(secret string) *SharedSecretTokenStore {
	return &SharedSecretTokenStore{secret: secret}
}

func (s *SharedSecretTokenStore) Validate(token string) (string, bool) {
	if subtle.ConstantTimeCompare([]byte(token), []byte(s.secret)) == 1 {
		return "", true // subdomain allocated by instance_id hash
	}
	return "", false
}

func (s *SharedSecretTokenStore) Register(_, _, _ string) error { return nil }
func (s *SharedSecretTokenStore) Touch(_ string) error          { return nil }
func (s *SharedSecretTokenStore) Cleanup() error                { return nil }
