package sqlite

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/chacha20poly1305"
)

var ErrDecryptFailed = errors.New("decryption failed")

// Encrypt encrypts plaintext using ChaCha20-Poly1305 with the given key.
func Encrypt(plaintext, key []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	// nonce is prepended to the ciphertext.
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Decrypt decrypts ciphertext encrypted with Encrypt.
func Decrypt(ciphertext, key []byte) ([]byte, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("creating cipher: %w", err)
	}

	nonceSize := aead.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, ErrDecryptFailed
	}

	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// GenerateKey generates a random 32-byte key for ChaCha20-Poly1305.
func GenerateKey() ([]byte, error) {
	key := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	return key, nil
}

// EncryptCredentials encrypts a credential map as JSON.
func EncryptCredentials(creds map[string]string, encKey []byte) ([]byte, error) {
	data, err := json.Marshal(creds)
	if err != nil {
		return nil, fmt.Errorf("marshaling credentials: %w", err)
	}
	return Encrypt(data, encKey)
}

// DecryptCredentials decrypts an encrypted credential blob to a map.
func DecryptCredentials(blob []byte, encKey []byte) (map[string]string, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	data, err := Decrypt(blob, encKey)
	if err != nil {
		return nil, err
	}
	var creds map[string]string
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("unmarshaling credentials: %w", err)
	}
	return creds, nil
}

// MergeCredentials decrypts existing credentials, merges new keys, and re-encrypts.
func MergeCredentials(existing []byte, update map[string]string, encKey []byte) ([]byte, error) {
	merged := make(map[string]string)

	// Decrypt existing if present.
	if len(existing) > 0 {
		old, err := DecryptCredentials(existing, encKey)
		if err != nil {
			return nil, fmt.Errorf("decrypting existing credentials: %w", err)
		}
		for k, v := range old {
			merged[k] = v
		}
	}

	// Merge new keys.
	for k, v := range update {
		merged[k] = v
	}

	return EncryptCredentials(merged, encKey)
}

// StoreCredential encrypts and stores a credential.
func (s *Store) StoreCredential(ctx context.Context, name string, value []byte, encKey []byte) error {
	encrypted, err := Encrypt(value, encKey)
	if err != nil {
		return fmt.Errorf("encrypting credential: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO credentials (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		name, encrypted,
	)
	return err
}

// GetCredential retrieves and decrypts a credential.
func (s *Store) GetCredential(ctx context.Context, name string, encKey []byte) ([]byte, error) {
	var encrypted []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM credentials WHERE key = ?`, name,
	).Scan(&encrypted)
	if err != nil {
		return nil, fmt.Errorf("querying credential: %w", err)
	}

	return Decrypt(encrypted, encKey)
}
