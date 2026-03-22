package orchestration

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func TestHandoff_Transfer(t *testing.T) {
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer s.Close()

	// Create a session.
	ctx := context.Background()
	sess, err := s.CreateSession(ctx, "telegram", "chat123", "default")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	agentNames := map[string]string{
		"default":  "SageClaw",
		"coder":    "Coding Agent",
	}
	h := NewHandoff(s, agentNames)

	// Transfer.
	if err := h.Transfer(ctx, sess.ID, "default", "coder", "needs code expertise"); err != nil {
		t.Fatalf("handoff: %v", err)
	}

	// Verify agent_id changed.
	updated, _ := s.GetSession(ctx, sess.ID)
	if updated.AgentID != "coder" {
		t.Fatalf("expected agent_id coder, got %s", updated.AgentID)
	}

	// Verify handoff message in history.
	msgs, _ := s.GetMessages(ctx, sess.ID, 0)
	if len(msgs) == 0 {
		t.Fatal("expected handoff message")
	}
	found := false
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "text" && len(c.Text) > 0 {
				if contains(c.Text, "handed off") && contains(c.Text, "code expertise") {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("expected handoff message with reason")
	}
}

func TestHandoff_UnknownTarget(t *testing.T) {
	s, _ := sqlite.New(":memory:")
	defer s.Close()

	ctx := context.Background()
	sess, _ := s.CreateSession(ctx, "cli", "local", "default")

	h := NewHandoff(s, map[string]string{"default": "SageClaw"})
	err := h.Transfer(ctx, sess.ID, "default", "nonexistent", "")
	if err == nil {
		t.Fatal("expected error for unknown target")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
