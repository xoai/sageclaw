package security

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupPairingDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`CREATE TABLE paired_channels (
		channel TEXT NOT NULL, chat_id TEXT NOT NULL, paired_at TEXT NOT NULL DEFAULT (datetime('now')),
		label TEXT DEFAULT '', PRIMARY KEY (channel, chat_id))`)
	db.Exec(`CREATE TABLE pairing_codes (
		code TEXT PRIMARY KEY, channel TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (datetime('now')), expires_at TEXT NOT NULL)`)
	return db
}

func TestPairingFlow(t *testing.T) {
	db := setupPairingDB(t)
	defer db.Close()
	ctx := context.Background()

	pm := NewPairingManager(db, true)

	// Should not be paired initially.
	if pm.IsPaired(ctx, "telegram", "user123") {
		t.Error("should not be paired initially")
	}

	// CLI should always be paired.
	if !pm.IsPaired(ctx, "cli", "anything") {
		t.Error("CLI should always be paired")
	}

	// Web should always be paired.
	if !pm.IsPaired(ctx, "web", "anything") {
		t.Error("Web should always be paired")
	}

	// Generate code.
	code, err := pm.GenerateCode("telegram")
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	if len(code) != 9 || code[:5] != "SAGE-" {
		t.Errorf("invalid code format: %q", code)
	}

	// Verify with wrong code.
	ok, _ := pm.VerifyCode(ctx, "telegram", "user123", "SAGE-XXXX")
	if ok {
		t.Error("should not verify with wrong code")
	}

	// Verify with correct code.
	ok, err = pm.VerifyCode(ctx, "telegram", "user123", code)
	if err != nil {
		t.Fatalf("VerifyCode: %v", err)
	}
	if !ok {
		t.Error("should verify with correct code")
	}

	// Should be paired now.
	if !pm.IsPaired(ctx, "telegram", "user123") {
		t.Error("should be paired after verification")
	}

	// Code should be consumed (can't reuse).
	ok, _ = pm.VerifyCode(ctx, "telegram", "other456", code)
	if ok {
		t.Error("should not verify with consumed code")
	}

	// List paired devices.
	devices, _ := pm.ListPaired(ctx, "telegram")
	if len(devices) != 1 || devices[0].ChatID != "user123" {
		t.Errorf("ListPaired = %v, want 1 device with user123", devices)
	}

	// Unpair.
	pm.Unpair(ctx, "telegram", "user123")
	if pm.IsPaired(ctx, "telegram", "user123") {
		t.Error("should not be paired after unpair")
	}
}

func TestPairingDisabled(t *testing.T) {
	db := setupPairingDB(t)
	defer db.Close()
	ctx := context.Background()

	pm := NewPairingManager(db, false)

	// When disabled, everything is paired.
	if !pm.IsPaired(ctx, "telegram", "random_stranger") {
		t.Error("should be paired when pairing is disabled")
	}
}

func TestExtractCode(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"SAGE-7X4K", "SAGE-7X4K"},
		{"sage-7x4k", "SAGE-7X4K"},
		{"  SAGE-7X4K  ", "SAGE-7X4K"},
		{"Hi! My code is SAGE-7X4K thanks", "SAGE-7X4K"},
		{"no code here", ""},
		{"SAGE-", ""},
		{"SAGE-AB", ""},
	}

	for _, tt := range tests {
		got := extractCode(tt.input)
		if got != tt.want {
			t.Errorf("extractCode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPairingCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code := generatePairingCode()
		if len(code) != 9 {
			t.Errorf("code length = %d, want 9", len(code))
		}
		if code[:5] != "SAGE-" {
			t.Errorf("code prefix = %q, want SAGE-", code[:5])
		}
		if seen[code] {
			t.Errorf("duplicate code: %s", code)
		}
		seen[code] = true
	}
}
