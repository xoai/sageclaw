package agent

import (
	"context"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestInjectTo_TargetsCorrectLoop(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
		"agent2": {AgentID: "agent2"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	// Ensure loops are created.
	l1 := pool.Get("agent1")
	l2 := pool.Get("agent2")
	if l1 == nil || l2 == nil {
		t.Fatal("expected both loops")
	}

	msg := canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: "targeted"}},
	}
	pool.InjectTo("agent1", msg)

	// agent1 should receive the message.
	select {
	case got := <-l1.injectChan:
		if got.Content[0].Text != "targeted" {
			t.Errorf("expected 'targeted', got %q", got.Content[0].Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("agent1 should have received the message")
	}

	// agent2 should NOT receive the message.
	select {
	case <-l2.injectChan:
		t.Error("agent2 should NOT have received the message")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestInjectTo_UnknownAgent(t *testing.T) {
	pool := NewLoopPool(map[string]Config{}, nil, nil, nil, nil, nil)
	// Should not panic.
	pool.InjectTo("nonexistent", canonical.Message{})
}

func TestPreAuthorizedGroups_Migration(t *testing.T) {
	cfg := Config{
		AgentID:        "test",
		NonInteractive: true,
	}
	l := NewLoop(cfg, nil, nil, nil, nil, nil)

	if len(l.config.PreAuthorizedGroups) != 1 || l.config.PreAuthorizedGroups[0] != "*" {
		t.Errorf("NonInteractive should migrate to PreAuthorizedGroups [\"*\"], got %v", l.config.PreAuthorizedGroups)
	}
}

func TestPreAuthorizedGroups_NoMigrationIfSet(t *testing.T) {
	cfg := Config{
		AgentID:             "test",
		NonInteractive:      true,
		PreAuthorizedGroups: []string{"memory"},
	}
	l := NewLoop(cfg, nil, nil, nil, nil, nil)

	if len(l.config.PreAuthorizedGroups) != 1 || l.config.PreAuthorizedGroups[0] != "memory" {
		t.Errorf("should not migrate if PreAuthorizedGroups already set, got %v", l.config.PreAuthorizedGroups)
	}
}

func TestContainsGroup(t *testing.T) {
	tests := []struct {
		groups []string
		target string
		want   bool
	}{
		{[]string{"*"}, "runtime", true},
		{[]string{"memory", "runtime"}, "runtime", true},
		{[]string{"memory"}, "runtime", false},
		{nil, "runtime", false},
		{[]string{}, "runtime", false},
	}
	for _, tt := range tests {
		if got := containsGroup(tt.groups, tt.target); got != tt.want {
			t.Errorf("containsGroup(%v, %q) = %v, want %v", tt.groups, tt.target, got, tt.want)
		}
	}
}

func TestResolveOwner_Cached(t *testing.T) {
	callCount := 0
	resolver := func(sessionID string) (string, string) {
		callCount++
		return "user123", "telegram"
	}

	l := NewLoop(Config{AgentID: "test"}, nil, nil, nil, nil, nil, WithOwnerResolver(resolver))

	ownerID, platform := l.resolveOwner("s1")
	if ownerID != "user123" || platform != "telegram" {
		t.Errorf("resolveOwner = (%q, %q), want (user123, telegram)", ownerID, platform)
	}

	// Second call should use cache.
	l.resolveOwner("s1")
	if callCount != 1 {
		t.Errorf("resolver called %d times, expected 1 (cached)", callCount)
	}

	// Different session triggers new call.
	l.resolveOwner("s2")
	if callCount != 2 {
		t.Errorf("resolver called %d times, expected 2 (new session)", callCount)
	}
}

func TestResolveOwner_NilResolver(t *testing.T) {
	l := NewLoop(Config{AgentID: "test"}, nil, nil, nil, nil, nil)

	ownerID, platform := l.resolveOwner("s1")
	if ownerID != "" || platform != "" {
		t.Errorf("nil resolver should return empty, got (%q, %q)", ownerID, platform)
	}
}

func TestNonceManager_IntegrationWithPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nm := NewNonceManager(ctx)
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)
	l := pool.Get("agent1")
	if l == nil {
		t.Fatal("expected loop")
	}

	// Generate nonce.
	pc, err := nm.Generate("agent1", "session1", "runtime")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Build consent token.
	token := "__consent__" + pc.Nonce + "_grant_once"

	// Inject targeted to agent1.
	pool.InjectTo("agent1", canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: token}},
	})

	// Verify it arrives.
	select {
	case msg := <-l.injectChan:
		if msg.Content[0].Text != token {
			t.Errorf("expected token %q, got %q", token, msg.Content[0].Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("should have received consent token")
	}

	// Validate nonce (single-use).
	result, err := nm.Validate(pc.Nonce)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", result.AgentID)
	}
}
