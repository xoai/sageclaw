package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"time"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	totpSecretBytes = 20 // 160-bit secret per RFC 6238
	totpDigits      = 6  // 6-digit code
	totpPeriod      = 30 // 30-second time step
	totpSkew        = 1  // accept ±1 time step
	totpIssuer      = "SageClaw"
)

// TOTP manages time-based one-time password authentication.
type TOTP struct {
	db     *sql.DB
	encKey []byte // 32-byte XChaCha20-Poly1305 encryption key
}

// NewTOTP creates a TOTP manager. The encKey encrypts the TOTP secret at rest.
func NewTOTP(db *sql.DB, encKey []byte) *TOTP {
	return &TOTP{db: db, encKey: encKey}
}

// Setup generates a TOTP secret and stores it encrypted after verifying the password.
// Returns the base32-encoded secret and an otpauth:// URI for QR code generation.
func (t *TOTP) Setup(password string) (secret string, uri string, err error) {
	if err := t.verifyPassword(password); err != nil {
		return "", "", err
	}

	raw := make([]byte, totpSecretBytes)
	if err := readRandom(raw); err != nil {
		return "", "", fmt.Errorf("generating TOTP secret: %w", err)
	}

	secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)

	// Encrypt and store.
	if err := t.storeSecret(secret); err != nil {
		return "", "", fmt.Errorf("storing TOTP secret: %w", err)
	}

	uri = fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=%s&digits=%d&period=%d",
		totpIssuer, secret, totpIssuer, totpDigits, totpPeriod)

	return secret, uri, nil
}

// Verify checks a 6-digit TOTP code. Accepts ±1 time step tolerance.
func (t *TOTP) Verify(code string) bool {
	secret, err := t.loadSecret()
	if err != nil || secret == "" {
		return false
	}

	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		return false
	}

	now := time.Now().Unix() / totpPeriod
	for i := -totpSkew; i <= totpSkew; i++ {
		expected := generateCode(raw, now+int64(i))
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

// IsEnabled returns true if TOTP is configured.
func (t *TOTP) IsEnabled() bool {
	secret, _ := t.loadSecret()
	return secret != ""
}

// Disable removes TOTP after verifying the password.
func (t *TOTP) Disable(password string) error {
	if err := t.verifyPassword(password); err != nil {
		return err
	}
	// Store empty value to indicate disabled.
	return t.setSetting("totp_secret", "")
}

// storeSecret encrypts and persists the TOTP secret.
func (t *TOTP) storeSecret(secret string) error {
	if len(t.encKey) == 0 {
		// Fallback: store plaintext if no encryption key (should not happen in production).
		return t.setSetting("totp_secret", secret)
	}

	aead, err := chacha20poly1305.NewX(t.encKey)
	if err != nil {
		return fmt.Errorf("creating cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if err := readRandom(nonce); err != nil {
		return fmt.Errorf("generating nonce: %w", err)
	}

	ciphertext := aead.Seal(nonce, nonce, []byte(secret), nil)
	return t.setSetting("totp_secret", hex.EncodeToString(ciphertext))
}

// loadSecret decrypts and returns the stored TOTP secret.
func (t *TOTP) loadSecret() (string, error) {
	stored, err := t.getSetting("totp_secret")
	if err != nil || stored == "" {
		return "", err
	}

	if len(t.encKey) == 0 {
		// Fallback: plaintext (legacy or no key).
		return stored, nil
	}

	ciphertext, err := hex.DecodeString(stored)
	if err != nil {
		// Not hex — might be a legacy plaintext secret. Return as-is.
		return stored, nil
	}

	aead, err := chacha20poly1305.NewX(t.encKey)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		// Too short for encrypted data — treat as plaintext.
		return stored, nil
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		// Decryption failed — might be plaintext from before encryption was added.
		return stored, nil
	}

	return string(plaintext), nil
}

// generateCode implements HOTP (RFC 4226) which TOTP builds on.
func generateCode(secret []byte, counter int64) string {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	code := binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff

	mod := uint32(math.Pow10(totpDigits))
	otp := code % mod

	return fmt.Sprintf("%0*d", totpDigits, otp)
}

func (t *TOTP) verifyPassword(password string) error {
	hash, err := t.getSetting("password_hash")
	if err != nil || hash == "" {
		return ErrNotSetup
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (t *TOTP) getSetting(key string) (string, error) {
	var value string
	err := t.db.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (t *TOTP) setSetting(key, value string) error {
	_, err := t.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, value)
	return err
}
