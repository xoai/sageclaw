package security

import (
	"context"
	"crypto/rand"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// PairedDevice represents a verified channel+chat_id.
type PairedDevice struct {
	Channel  string `json:"channel"`
	ChatID   string `json:"chat_id"`
	PairedAt string `json:"paired_at"`
	Label    string `json:"label"`
}

// PairingManager handles channel device pairing.
type PairingManager struct {
	db      *sql.DB
	enabled bool
}

// NewPairingManager creates a new pairing manager.
// If enabled is false, all channels are treated as paired (open access).
func NewPairingManager(db *sql.DB, enabled bool) *PairingManager {
	return &PairingManager{db: db, enabled: enabled}
}

// IsEnabled returns whether pairing is enforced.
func (pm *PairingManager) IsEnabled() bool {
	return pm.enabled
}

// GenerateCode creates a pairing code for a channel.
// Format: SAGE-XXXX (4 alphanumeric chars, uppercase).
func (pm *PairingManager) GenerateCode(channel string) (string, error) {
	// Clean expired codes first.
	pm.CleanExpired()

	code := generatePairingCode()
	expiresAt := time.Now().Add(10 * time.Minute).Format(time.RFC3339)

	_, err := pm.db.Exec(
		`INSERT INTO pairing_codes (code, channel, expires_at) VALUES (?, ?, ?)`,
		code, channel, expiresAt)
	if err != nil {
		return "", fmt.Errorf("generating pairing code: %w", err)
	}

	return code, nil
}

// VerifyCode checks if a code is valid and pairs the chat_id if so.
// Returns true if pairing succeeded.
func (pm *PairingManager) VerifyCode(ctx context.Context, channel, chatID, message string) (bool, error) {
	// Normalize: extract code from message (might have whitespace or other text).
	code := extractCode(message)
	if code == "" {
		return false, nil
	}

	// Check if code exists and hasn't expired.
	var codeChannel string
	var expiresAt string
	err := pm.db.QueryRowContext(ctx,
		`SELECT channel, expires_at FROM pairing_codes WHERE code = ?`, code).Scan(&codeChannel, &expiresAt)
	if err != nil {
		return false, nil // Code not found — not an error, just not a pairing attempt.
	}

	// Check expiry.
	expires, _ := time.Parse(time.RFC3339, expiresAt)
	if time.Now().After(expires) {
		pm.db.Exec(`DELETE FROM pairing_codes WHERE code = ?`, code)
		return false, nil
	}

	// Check channel matches.
	if codeChannel != channel {
		return false, nil
	}

	// Pair the device.
	_, err = pm.db.ExecContext(ctx,
		`INSERT INTO paired_channels (channel, chat_id) VALUES (?, ?)
		 ON CONFLICT(channel, chat_id) DO UPDATE SET paired_at = datetime('now')`,
		channel, chatID)
	if err != nil {
		return false, fmt.Errorf("pairing device: %w", err)
	}

	// Delete the used code.
	pm.db.Exec(`DELETE FROM pairing_codes WHERE code = ?`, code)

	return true, nil
}

// IsPaired checks if a channel+chatID combination is verified.
// Returns true for always-trusted channels (cli, web).
func (pm *PairingManager) IsPaired(ctx context.Context, channel, chatID string) bool {
	if !pm.enabled {
		return true
	}

	// CLI and Web are always trusted (local access).
	if channel == "cli" || channel == "web" || channel == "mcp" {
		return true
	}

	var count int
	pm.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM paired_channels WHERE channel = ? AND chat_id = ?`,
		channel, chatID).Scan(&count)

	return count > 0
}

// Unpair removes a paired device.
func (pm *PairingManager) Unpair(ctx context.Context, channel, chatID string) error {
	_, err := pm.db.ExecContext(ctx,
		`DELETE FROM paired_channels WHERE channel = ? AND chat_id = ?`,
		channel, chatID)
	return err
}

// ListPaired returns all paired devices, optionally filtered by channel.
func (pm *PairingManager) ListPaired(ctx context.Context, channel string) ([]PairedDevice, error) {
	query := `SELECT channel, chat_id, paired_at, COALESCE(label,'') FROM paired_channels`
	var args []any
	if channel != "" {
		query += ` WHERE channel = ?`
		args = append(args, channel)
	}
	query += ` ORDER BY paired_at DESC`

	rows, err := pm.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []PairedDevice
	for rows.Next() {
		var d PairedDevice
		rows.Scan(&d.Channel, &d.ChatID, &d.PairedAt, &d.Label)
		devices = append(devices, d)
	}
	return devices, nil
}

// CleanExpired removes expired pairing codes.
func (pm *PairingManager) CleanExpired() {
	pm.db.Exec(`DELETE FROM pairing_codes WHERE expires_at < datetime('now')`)
}

// generatePairingCode creates a code like "SAGE-7X4K".
func generatePairingCode() string {
	const chars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // No I, O, 0, 1 (avoid confusion)
	b := make([]byte, 4)
	rand.Read(b)
	code := make([]byte, 4)
	for i := range code {
		code[i] = chars[int(b[i])%len(chars)]
	}
	return "SAGE-" + string(code)
}

// extractCode finds a SAGE-XXXX pattern in a message.
func extractCode(message string) string {
	msg := strings.ToUpper(strings.TrimSpace(message))
	// Direct match.
	if len(msg) == 9 && strings.HasPrefix(msg, "SAGE-") {
		return msg
	}
	// Search within message.
	idx := strings.Index(msg, "SAGE-")
	if idx >= 0 && idx+9 <= len(msg) {
		candidate := msg[idx : idx+9]
		// Verify it's alphanumeric after SAGE-.
		valid := true
		for _, c := range candidate[5:] {
			if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '9')) {
				valid = false
				break
			}
		}
		if valid {
			return candidate
		}
	}
	return ""
}
