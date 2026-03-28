package tunnel

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

// EnsureInstanceID loads or generates a stable instance identifier.
// The ID is stored in the settings table and persists across restarts.
// It determines the tunnel subdomain (via SHA-256 hash on the relay).
func EnsureInstanceID(db *sql.DB) (string, error) {
	var id string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", "instance_id").Scan(&id)
	if err == nil && id != "" {
		return id, nil
	}

	id = uuid.NewString()
	_, err = db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		"instance_id", id)
	if err != nil {
		return "", fmt.Errorf("storing instance_id: %w", err)
	}
	return id, nil
}
