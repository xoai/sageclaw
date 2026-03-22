package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotSetup      = errors.New("password not set up")
	ErrAlreadySetup  = errors.New("password already set up")
	ErrWrongPassword = errors.New("wrong password")
)

// Auth manages authentication state.
type Auth struct {
	db     *sql.DB
	secret []byte // JWT signing secret.
}

// New creates an Auth manager.
func New(db *sql.DB) (*Auth, error) {
	a := &Auth{db: db}

	// Load or generate JWT secret.
	secret, err := a.getSetting("jwt_secret")
	if err != nil || secret == "" {
		// Generate new secret.
		s := make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, s); err != nil {
			return nil, fmt.Errorf("generating secret: %w", err)
		}
		secretStr := fmt.Sprintf("%x", s)
		a.setSetting("jwt_secret", secretStr)
		a.secret = s
	} else {
		// Decode hex secret back to raw bytes.
		decoded, err := hex.DecodeString(secret)
		if err != nil {
			return nil, fmt.Errorf("decoding jwt secret: %w", err)
		}
		a.secret = decoded
	}

	return a, nil
}

// IsSetup returns true if a password has been configured.
func (a *Auth) IsSetup() bool {
	hash, _ := a.getSetting("password_hash")
	return hash != ""
}

// Setup creates the initial password. Fails if already set up.
func (a *Auth) Setup(password string) error {
	if a.IsSetup() {
		return ErrAlreadySetup
	}
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	return a.setSetting("password_hash", string(hash))
}

// Login verifies the password and returns a JWT token.
func (a *Auth) Login(password string) (string, error) {
	hash, err := a.getSetting("password_hash")
	if err != nil || hash == "" {
		return "", ErrNotSetup
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return "", ErrWrongPassword
	}

	return SignJWT(a.secret, 24*time.Hour)
}

// Verify checks a JWT token.
func (a *Auth) Verify(token string) error {
	_, err := VerifyJWT(token, a.secret)
	return err
}

// ChangePassword updates the password after verifying the old one.
func (a *Auth) ChangePassword(oldPassword, newPassword string) error {
	hash, err := a.getSetting("password_hash")
	if err != nil || hash == "" {
		return ErrNotSetup
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(oldPassword)); err != nil {
		return ErrWrongPassword
	}

	if len(newPassword) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	return a.setSetting("password_hash", string(newHash))
}

func (a *Auth) getSetting(key string) (string, error) {
	var value string
	err := a.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (a *Auth) setSetting(key, value string) error {
	_, err := a.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value)
	return err
}

func readRandom(b []byte) error {
	_, err := io.ReadFull(rand.Reader, b)
	return err
}
