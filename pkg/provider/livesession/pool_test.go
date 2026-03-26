package livesession

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

// mockLiveProvider creates mock live sessions for testing.
type mockLiveProvider struct {
	mu       sync.Mutex
	sessions int
}

func (m *mockLiveProvider) Name() string { return "mock-live" }

func (m *mockLiveProvider) OpenSession(ctx context.Context, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	m.mu.Lock()
	m.sessions++
	id := m.sessions
	m.mu.Unlock()

	ch := make(chan provider.LiveEvent, 8)
	return &mockLiveSession{id: id, events: ch}, nil
}

type mockLiveSession struct {
	id     int
	events chan provider.LiveEvent
	closed bool
	mu     sync.Mutex
}

func (s *mockLiveSession) ID() string                                             { return "mock-session" }
func (s *mockLiveSession) Send(ctx context.Context, msg provider.LiveMessage) error { return nil }
func (s *mockLiveSession) Receive() <-chan provider.LiveEvent                      { return s.events }
func (s *mockLiveSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.events)
	}
	return nil
}

func TestPool_GetOrCreate_New(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 1*time.Second)
	defer pool.Close()

	cfg := provider.LiveSessionConfig{Model: "test-model"}
	sess, err := pool.GetOrCreate(context.Background(), "session-1", cfg)
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if sess == nil {
		t.Fatal("session is nil")
	}
	if pool.Size() != 1 {
		t.Errorf("pool size = %d, want 1", pool.Size())
	}
	if mp.sessions != 1 {
		t.Errorf("provider opened %d sessions, want 1", mp.sessions)
	}
}

func TestPool_GetOrCreate_Reuse(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 5*time.Second)
	defer pool.Close()

	cfg := provider.LiveSessionConfig{Model: "test"}
	sess1, _ := pool.GetOrCreate(context.Background(), "s1", cfg)
	sess2, _ := pool.GetOrCreate(context.Background(), "s1", cfg)

	if sess1 != sess2 {
		t.Error("expected same session on reuse")
	}
	if mp.sessions != 1 {
		t.Errorf("provider opened %d sessions, want 1 (reuse)", mp.sessions)
	}
}

func TestPool_GetOrCreate_DifferentSessions(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 5*time.Second)
	defer pool.Close()

	cfg := provider.LiveSessionConfig{Model: "test"}
	pool.GetOrCreate(context.Background(), "s1", cfg)
	pool.GetOrCreate(context.Background(), "s2", cfg)

	if pool.Size() != 2 {
		t.Errorf("pool size = %d, want 2", pool.Size())
	}
	if mp.sessions != 2 {
		t.Errorf("provider opened %d sessions, want 2", mp.sessions)
	}
}

func TestPool_IdleEviction(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 100*time.Millisecond)
	defer pool.Close()

	cfg := provider.LiveSessionConfig{Model: "test"}
	pool.GetOrCreate(context.Background(), "s1", cfg)

	if pool.Size() != 1 {
		t.Fatalf("pool size = %d, want 1", pool.Size())
	}

	// Wait for idle eviction.
	time.Sleep(300 * time.Millisecond)

	if pool.Size() != 0 {
		t.Errorf("pool size after idle = %d, want 0", pool.Size())
	}
}

func TestPool_Remove(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 5*time.Second)
	defer pool.Close()

	cfg := provider.LiveSessionConfig{Model: "test"}
	pool.GetOrCreate(context.Background(), "s1", cfg)
	pool.Remove("s1")

	if pool.Size() != 0 {
		t.Errorf("pool size after remove = %d, want 0", pool.Size())
	}
}

func TestPool_Close(t *testing.T) {
	mp := &mockLiveProvider{}
	pool := NewPool(mp, 5*time.Second)

	cfg := provider.LiveSessionConfig{Model: "test"}
	pool.GetOrCreate(context.Background(), "s1", cfg)
	pool.GetOrCreate(context.Background(), "s2", cfg)

	pool.Close()

	if pool.Size() != 0 {
		t.Errorf("pool size after close = %d, want 0", pool.Size())
	}

	// Further GetOrCreate should fail.
	_, err := pool.GetOrCreate(context.Background(), "s3", cfg)
	if err == nil {
		t.Error("expected error after pool close")
	}
}

// Ensure mockLiveSession satisfies the interface.
var _ provider.LiveSession = (*mockLiveSession)(nil)
var _ provider.LiveProvider = (*mockLiveProvider)(nil)

// Silence unused import for canonical if needed.
var _ = canonical.Message{}
