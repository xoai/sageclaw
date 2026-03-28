package agent

import (
	"context"
	"testing"
	"time"
)

func TestNonceManager_Generate(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	pc, err := m.Generate("agent1", "session1", "runtime")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if pc.Nonce == "" {
		t.Error("nonce should not be empty")
	}
	if len(pc.Nonce) != 32 { // 16 bytes hex-encoded
		t.Errorf("nonce should be 32 hex chars, got %d", len(pc.Nonce))
	}
	if pc.AgentID != "agent1" {
		t.Errorf("agentID = %q, want %q", pc.AgentID, "agent1")
	}
	if pc.SessionID != "session1" {
		t.Errorf("sessionID = %q, want %q", pc.SessionID, "session1")
	}
	if pc.Group != "runtime" {
		t.Errorf("group = %q, want %q", pc.Group, "runtime")
	}
	if pc.ExpiresAt.Sub(pc.CreatedAt) != 180*time.Second {
		t.Error("expiry should be 180s after creation")
	}
}

func TestNonceManager_Uniqueness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		pc, err := m.Generate("a", "s", "g")
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if seen[pc.Nonce] {
			t.Fatalf("duplicate nonce at iteration %d", i)
		}
		seen[pc.Nonce] = true
	}
}

func TestNonceManager_ValidateSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	pc, _ := m.Generate("agent1", "session1", "fs")
	nonce := pc.Nonce

	result, err := m.Validate(nonce)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.AgentID != "agent1" {
		t.Errorf("agentID = %q, want %q", result.AgentID, "agent1")
	}
	if result.Group != "fs" {
		t.Errorf("group = %q, want %q", result.Group, "fs")
	}
}

func TestNonceManager_SingleUse(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	pc, _ := m.Generate("a", "s", "g")
	nonce := pc.Nonce

	// First validate succeeds.
	if _, err := m.Validate(nonce); err != nil {
		t.Fatalf("first validate should succeed: %v", err)
	}

	// Second validate fails (consumed).
	if _, err := m.Validate(nonce); err == nil {
		t.Error("second validate should fail (nonce consumed)")
	}
}

func TestNonceManager_NotFound(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	_, err := m.Validate("nonexistent")
	if err == nil {
		t.Error("should error for unknown nonce")
	}
}

func TestNonceManager_Expired(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	pc, _ := m.Generate("a", "s", "g")
	// Manually expire it.
	m.mu.Lock()
	m.pending[pc.Nonce].ExpiresAt = time.Now().Add(-1 * time.Second)
	m.mu.Unlock()

	_, err := m.Validate(pc.Nonce)
	if err == nil {
		t.Error("should error for expired nonce")
	}
}

func TestNonceManager_Cleanup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := NewNonceManager(ctx)

	// Generate some nonces and expire them.
	for i := 0; i < 5; i++ {
		pc, _ := m.Generate("a", "s", "g")
		m.mu.Lock()
		m.pending[pc.Nonce].ExpiresAt = time.Now().Add(-1 * time.Second)
		m.mu.Unlock()
	}

	// Generate a valid one.
	m.Generate("a", "s", "g")

	if m.PendingCount() != 6 {
		t.Fatalf("expected 6 pending, got %d", m.PendingCount())
	}

	m.Cleanup()

	if m.PendingCount() != 1 {
		t.Errorf("expected 1 pending after cleanup, got %d", m.PendingCount())
	}
}

func TestNonceManager_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	m := NewNonceManager(ctx)

	// Generate a nonce to verify manager works.
	_, err := m.Generate("a", "s", "g")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Cancel stops the background ticker (no panic, no leak).
	cancel()
	time.Sleep(10 * time.Millisecond) // Let goroutine exit.

	// Manager still works for non-background operations.
	_, err = m.Generate("a", "s", "g")
	if err != nil {
		t.Fatalf("Generate after cancel: %v", err)
	}
}
