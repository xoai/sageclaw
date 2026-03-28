package tunnel

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
)

const tokenBytes = 32

// EnsureToken loads or generates a tunnel authentication token.
// The token is stored in the settings table and sent to the relay
// during the WebSocket handshake.
func EnsureToken(db *sql.DB) (string, error) {
	var tok string
	err := db.QueryRow("SELECT value FROM settings WHERE key = ?", "tunnel_token").Scan(&tok)
	if err == nil && tok != "" {
		return tok, nil
	}

	tok, err = generateToken()
	if err != nil {
		return "", err
	}

	_, err = db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		"tunnel_token", tok)
	if err != nil {
		return "", fmt.Errorf("storing tunnel_token: %w", err)
	}
	return tok, nil
}

// RotateToken generates a new token, stores it, and returns it.
func RotateToken(db *sql.DB) (string, error) {
	tok, err := generateToken()
	if err != nil {
		return "", err
	}

	_, err = db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		"tunnel_token", tok)
	if err != nil {
		return "", fmt.Errorf("storing tunnel_token: %w", err)
	}
	return tok, nil
}

func generateToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("generating tunnel token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
