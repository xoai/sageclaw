package auth

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key         TEXT PRIMARY KEY,
			value       TEXT NOT NULL,
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// testEncKey returns a deterministic 32-byte key for testing.
func testEncKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

// --- TOTP Tests ---

func TestTOTPRFC6238Vector(t *testing.T) {
	// RFC 6238 Appendix B test vectors for HMAC-SHA1.
	// Secret = "12345678901234567890" (ASCII), time step = 30s.
	secret := []byte("12345678901234567890")

	tests := []struct {
		time    int64  // Unix time
		counter int64  // time / 30
		code    string // expected 6-digit code
	}{
		{59, 1, "287082"},
		{1111111109, 37037036, "081804"},
		{1111111111, 37037037, "050471"},
		{1234567890, 41152263, "005924"},
		{2000000000, 66666666, "279037"},
	}

	for _, tt := range tests {
		got := generateCode(secret, tt.counter)
		if got != tt.code {
			t.Errorf("time=%d counter=%d: got %q, want %q", tt.time, tt.counter, got, tt.code)
		}
	}
}

func TestTOTPGenerateCode(t *testing.T) {
	secret := []byte("12345678901234567890")
	code := generateCode(secret, 1)
	if len(code) != 6 {
		t.Errorf("code length: %d, want 6", len(code))
	}
	// Verify deterministic.
	code2 := generateCode(secret, 1)
	if code != code2 {
		t.Errorf("non-deterministic: %q vs %q", code, code2)
	}
	// Different counter → different code.
	code3 := generateCode(secret, 2)
	if code == code3 {
		t.Error("same code for different counters")
	}
}

func TestTOTPSetupAndVerify(t *testing.T) {
	db := testDB(t)

	// Setup password first.
	a, _ := New(db)
	a.Setup("testpass123")

	totp := NewTOTP(db, testEncKey(t))

	// Should not be enabled initially.
	if totp.IsEnabled() {
		t.Error("TOTP should not be enabled initially")
	}

	// Setup TOTP.
	secret, uri, err := totp.Setup("testpass123")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	if secret == "" {
		t.Error("expected non-empty secret")
	}
	if uri == "" {
		t.Error("expected non-empty URI")
	}
	if !totp.IsEnabled() {
		t.Error("TOTP should be enabled after setup")
	}

	// Verify with wrong code.
	if totp.Verify("000000") {
		// This might pass by coincidence, but extremely unlikely.
		t.Log("warning: 000000 matched (1 in 1M chance)")
	}

	// Verify with wrong password should fail setup.
	_, _, err = totp.Setup("wrongpass")
	if err == nil {
		t.Error("expected error for wrong password")
	}
}

func TestTOTPDisable(t *testing.T) {
	db := testDB(t)
	a, _ := New(db)
	a.Setup("testpass123")

	totp := NewTOTP(db, testEncKey(t))
	totp.Setup("testpass123")

	if !totp.IsEnabled() {
		t.Fatal("TOTP should be enabled")
	}

	// Wrong password should fail.
	if err := totp.Disable("wrongpass"); err == nil {
		t.Error("expected error for wrong password")
	}

	// Correct password.
	if err := totp.Disable("testpass123"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if totp.IsEnabled() {
		t.Error("TOTP should be disabled")
	}
}

// --- Rate Limiter Tests ---

func TestLoginLimiterAllows5(t *testing.T) {
	l := NewLoginLimiter()
	ip := "192.168.1.1"

	for i := 0; i < 5; i++ {
		if !l.Allow(ip) {
			t.Errorf("attempt %d should be allowed", i+1)
		}
	}

	// 6th should be blocked.
	if l.Allow(ip) {
		t.Error("6th attempt should be blocked")
	}
}

func TestLoginLimiterDifferentIPs(t *testing.T) {
	l := NewLoginLimiter()

	// Different IPs have separate buckets.
	for i := 0; i < 5; i++ {
		l.Allow("10.0.0.1")
	}
	// 10.0.0.1 is now rate-limited.
	if l.Allow("10.0.0.1") {
		t.Error("10.0.0.1 should be blocked")
	}
	// 10.0.0.2 should still be allowed.
	if !l.Allow("10.0.0.2") {
		t.Error("10.0.0.2 should be allowed")
	}
}

// --- ClientIP Tests ---

func TestClientIPDirect(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "192.168.1.100:54321",
		Header:     http.Header{},
	}
	ip := ClientIP(r, false)
	if ip != "192.168.1.100" {
		t.Errorf("got %q, want 192.168.1.100", ip)
	}
}

func TestClientIPTrustedProxy(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "127.0.0.1:54321",
		Header:     http.Header{"X-Forwarded-For": {"203.0.113.50, 127.0.0.1"}},
	}

	// With trustProxy=true, reads X-Forwarded-For.
	ip := ClientIP(r, true)
	if ip != "203.0.113.50" {
		t.Errorf("trusted: got %q, want 203.0.113.50", ip)
	}

	// Without trust, ignores X-Forwarded-For.
	ip = ClientIP(r, false)
	if ip != "127.0.0.1" {
		t.Errorf("untrusted: got %q, want 127.0.0.1", ip)
	}
}

// --- RotateSecret Tests ---

func TestRotateSecret(t *testing.T) {
	db := testDB(t)
	a, _ := New(db)
	a.Setup("testpass123")

	// Login and get a token.
	token1, err := a.Login("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(token1); err != nil {
		t.Fatal("token should be valid before rotation")
	}

	// Rotate secret.
	if err := a.RotateSecret(); err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}

	// Old token should be invalid.
	if err := a.Verify(token1); err == nil {
		t.Error("old token should be invalid after rotation")
	}

	// New login should work.
	token2, err := a.Login("testpass123")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(token2); err != nil {
		t.Fatal("new token should be valid")
	}
}

// --- LoginWithExpiry Tests ---

func TestLoginWithExpiry(t *testing.T) {
	db := testDB(t)
	a, _ := New(db)
	a.Setup("testpass123")

	token, err := a.LoginWithExpiry("testpass123", 4*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Verify(token); err != nil {
		t.Fatal("token should be valid")
	}

	// Wrong password.
	_, err = a.LoginWithExpiry("wrong", 4*time.Hour)
	if err == nil {
		t.Error("expected error for wrong password")
	}
}
