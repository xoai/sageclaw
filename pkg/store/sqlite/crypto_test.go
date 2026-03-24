package sqlite

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	plaintext := []byte("my-secret-api-key-12345")
	ciphertext, err := Encrypt(plaintext, key)
	if err != nil {
		t.Fatalf("encrypting: %v", err)
	}

	// Ciphertext should be different from plaintext.
	if bytes.Equal(ciphertext, plaintext) {
		t.Fatal("ciphertext should differ from plaintext")
	}

	decrypted, err := Decrypt(ciphertext, key)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if !bytes.Equal(decrypted, plaintext) {
		t.Fatalf("expected %q, got %q", plaintext, decrypted)
	}
}

func TestDecrypt_WrongKey(t *testing.T) {
	key1, _ := GenerateKey()
	key2, _ := GenerateKey()

	ciphertext, _ := Encrypt([]byte("secret"), key1)
	_, err := Decrypt(ciphertext, key2)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("expected ErrDecryptFailed, got: %v", err)
	}
}

func TestDecrypt_TruncatedCiphertext(t *testing.T) {
	key, _ := GenerateKey()
	_, err := Decrypt([]byte("short"), key)
	if !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("expected ErrDecryptFailed, got: %v", err)
	}
}

func TestStoreCredential_RoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key, _ := GenerateKey()

	err := s.StoreCredential(ctx, "anthropic_api_key", []byte("sk-ant-test-key"), key)
	if err != nil {
		t.Fatalf("storing: %v", err)
	}

	got, err := s.GetCredential(ctx, "anthropic_api_key", key)
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if !bytes.Equal(got, []byte("sk-ant-test-key")) {
		t.Fatalf("expected sk-ant-test-key, got %s", got)
	}
}

func TestStoreCredential_Update(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key, _ := GenerateKey()

	s.StoreCredential(ctx, "token", []byte("v1"), key)
	s.StoreCredential(ctx, "token", []byte("v2"), key)

	got, _ := s.GetCredential(ctx, "token", key)
	if !bytes.Equal(got, []byte("v2")) {
		t.Fatalf("expected v2, got %s", got)
	}
}

func TestEncryptDecryptCredentials_RoundTrip(t *testing.T) {
	key, _ := GenerateKey()

	creds := map[string]string{
		"token":      "bot123:ABC",
		"app_token":  "xapp-456",
		"secret_key": "s3cret",
	}

	blob, err := EncryptCredentials(creds, key)
	if err != nil {
		t.Fatalf("encrypting credentials: %v", err)
	}
	if len(blob) == 0 {
		t.Fatal("expected non-empty blob")
	}

	got, err := DecryptCredentials(blob, key)
	if err != nil {
		t.Fatalf("decrypting credentials: %v", err)
	}

	for k, v := range creds {
		if got[k] != v {
			t.Fatalf("key %s: expected %s, got %s", k, v, got[k])
		}
	}
}

func TestDecryptCredentials_EmptyBlob(t *testing.T) {
	key, _ := GenerateKey()
	got, err := DecryptCredentials(nil, key)
	if err != nil {
		t.Fatalf("expected nil error for empty blob, got: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil map for empty blob, got: %v", got)
	}
}

func TestMergeCredentials(t *testing.T) {
	key, _ := GenerateKey()

	// Start with one field.
	initial := map[string]string{"token": "old-token"}
	blob, _ := EncryptCredentials(initial, key)

	// Merge: add a new field, update existing.
	update := map[string]string{"token": "new-token", "secret": "s3cret"}
	merged, err := MergeCredentials(blob, update, key)
	if err != nil {
		t.Fatalf("merging: %v", err)
	}

	got, _ := DecryptCredentials(merged, key)
	if got["token"] != "new-token" {
		t.Fatalf("expected token=new-token, got %s", got["token"])
	}
	if got["secret"] != "s3cret" {
		t.Fatalf("expected secret=s3cret, got %s", got["secret"])
	}
}

func TestMergeCredentials_EmptyExisting(t *testing.T) {
	key, _ := GenerateKey()

	update := map[string]string{"token": "fresh"}
	merged, err := MergeCredentials(nil, update, key)
	if err != nil {
		t.Fatalf("merging with nil existing: %v", err)
	}

	got, _ := DecryptCredentials(merged, key)
	if got["token"] != "fresh" {
		t.Fatalf("expected token=fresh, got %s", got["token"])
	}
}
