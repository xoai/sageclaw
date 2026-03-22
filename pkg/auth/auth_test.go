package auth

import (
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestAuth(t *testing.T) *Auth {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	a, err := New(store.DB())
	if err != nil {
		t.Fatalf("creating auth: %v", err)
	}
	return a
}

// --- JWT tests ---

func TestJWT_SignAndVerify(t *testing.T) {
	secret := []byte("test-secret-32-bytes-long-xxxxx")
	token, err := SignJWT(secret, time.Hour)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if token == "" {
		t.Fatal("expected token")
	}

	claims, err := VerifyJWT(token, secret)
	if err != nil {
		t.Fatalf("verifying: %v", err)
	}
	if claims.IssuedAt == 0 {
		t.Fatal("expected iat")
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	token, _ := SignJWT([]byte("secret1-32-bytes-long-xxxxxxxxx"), time.Hour)
	_, err := VerifyJWT(token, []byte("secret2-32-bytes-long-xxxxxxxxx"))
	if err != ErrTokenInvalid {
		t.Fatalf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestJWT_Expired(t *testing.T) {
	token, _ := SignJWT([]byte("test-secret-32-bytes-long-xxxxx"), -time.Hour)
	_, err := VerifyJWT(token, []byte("test-secret-32-bytes-long-xxxxx"))
	if err != ErrTokenExpired {
		t.Fatalf("expected ErrTokenExpired, got %v", err)
	}
}

func TestJWT_InvalidToken(t *testing.T) {
	_, err := VerifyJWT("not.a.valid.token", []byte("secret"))
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Auth manager tests ---

func TestAuth_SetupAndLogin(t *testing.T) {
	a := newTestAuth(t)

	if a.IsSetup() {
		t.Fatal("should not be setup initially")
	}

	if err := a.Setup("mypassword123"); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if !a.IsSetup() {
		t.Fatal("should be setup after Setup()")
	}

	// Login with correct password.
	token, err := a.Login("mypassword123")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if token == "" {
		t.Fatal("expected token")
	}

	// Verify token.
	if err := a.Verify(token); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestAuth_WrongPassword(t *testing.T) {
	a := newTestAuth(t)
	a.Setup("correctpassword")

	_, err := a.Login("wrongpassword")
	if err != ErrWrongPassword {
		t.Fatalf("expected ErrWrongPassword, got %v", err)
	}
}

func TestAuth_DoubleSetup(t *testing.T) {
	a := newTestAuth(t)
	a.Setup("password123")

	err := a.Setup("anotherpassword")
	if err != ErrAlreadySetup {
		t.Fatalf("expected ErrAlreadySetup, got %v", err)
	}
}

func TestAuth_ShortPassword(t *testing.T) {
	a := newTestAuth(t)
	err := a.Setup("short")
	if err == nil {
		t.Fatal("expected error for short password")
	}
}

func TestAuth_ChangePassword(t *testing.T) {
	a := newTestAuth(t)
	a.Setup("oldpassword1")

	if err := a.ChangePassword("oldpassword1", "newpassword1"); err != nil {
		t.Fatalf("change: %v", err)
	}

	// Old password should fail.
	_, err := a.Login("oldpassword1")
	if err != ErrWrongPassword {
		t.Fatalf("expected ErrWrongPassword for old password, got %v", err)
	}

	// New password should work.
	token, err := a.Login("newpassword1")
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if token == "" {
		t.Fatal("expected token")
	}
}

func TestMaskKey(t *testing.T) {
	tests := []struct{ in, out string }{
		{"sk-ant-api03-very-long-key-here-1234", "sk-a***1234"},
		{"short", "***"},
		{"12345678", "***"},
		{"123456789", "1234***6789"},
	}
	for _, tt := range tests {
		got := MaskKey(tt.in)
		if got != tt.out {
			t.Errorf("MaskKey(%q) = %q, want %q", tt.in, got, tt.out)
		}
	}
}
