package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	nonceBytes       = 16
	nonceExpiry      = 180 * time.Second
	cleanupInterval  = 60 * time.Second
	cleanupEveryNth  = 10
)

// PendingConsent holds context for a consent request awaiting response.
type PendingConsent struct {
	Nonce     string
	AgentID   string
	SessionID string
	Group     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NonceManager generates and validates one-time consent nonces.
type NonceManager struct {
	mu      sync.Mutex
	pending map[string]*PendingConsent
	calls   atomic.Int64
}

// NewNonceManager creates a nonce manager with a background cleanup ticker.
// Cancel the context to stop the ticker.
func NewNonceManager(ctx context.Context) *NonceManager {
	m := &NonceManager{
		pending: make(map[string]*PendingConsent),
	}
	go m.cleanupLoop(ctx)
	return m
}

// Generate creates a new nonce for a consent request.
func (m *NonceManager) Generate(agentID, sessionID, group string) (*PendingConsent, error) {
	b := make([]byte, nonceBytes)
	if _, err := rand.Read(b); err != nil {
		return nil, fmt.Errorf("generating consent nonce: %w", err)
	}
	nonce := hex.EncodeToString(b)
	now := time.Now()

	pc := &PendingConsent{
		Nonce:     nonce,
		AgentID:   agentID,
		SessionID: sessionID,
		Group:     group,
		CreatedAt: now,
		ExpiresAt: now.Add(nonceExpiry),
	}

	m.mu.Lock()
	m.pending[nonce] = pc
	m.mu.Unlock()

	// Lazy cleanup on every Nth call.
	if m.calls.Add(1)%cleanupEveryNth == 0 {
		go m.Cleanup()
	}

	return pc, nil
}

// Validate checks and consumes a nonce. Returns the pending consent context
// or error (expired, not found, already used).
func (m *NonceManager) Validate(nonce string) (*PendingConsent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	pc, ok := m.pending[nonce]
	if !ok {
		return nil, fmt.Errorf("consent nonce not found or already used")
	}

	// Consume immediately (single-use).
	delete(m.pending, nonce)

	if time.Now().After(pc.ExpiresAt) {
		return nil, fmt.Errorf("consent nonce expired")
	}

	return pc, nil
}

// Cleanup removes expired nonces.
func (m *NonceManager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for nonce, pc := range m.pending {
		if now.After(pc.ExpiresAt) {
			delete(m.pending, nonce)
		}
	}
}

// PendingCount returns the number of pending nonces (for testing).
func (m *NonceManager) PendingCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.pending)
}

func (m *NonceManager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Cleanup()
		}
	}
}
