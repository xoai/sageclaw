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
