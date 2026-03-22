package sqlite

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStore_OpenClose(t *testing.T) {
	s := newTestStore(t)
	if s.db == nil {
		t.Fatal("expected db to be non-nil")
	}
}

func TestStore_MigrationsIdempotent(t *testing.T) {
	s := newTestStore(t)
	// Running migrate again should be a no-op.
	if err := s.migrate(); err != nil {
		t.Fatalf("second migration run failed: %v", err)
	}
}

func TestStore_CreateAndGetSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "telegram", "chat123", "agent1")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	if sess.ID == "" {
		t.Fatal("expected session ID to be set")
	}
	if sess.Channel != "telegram" {
		t.Fatalf("expected channel telegram, got %s", sess.Channel)
	}

	got, err := s.GetSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("getting session: %v", err)
	}
	if got.ID != sess.ID {
		t.Fatalf("expected ID %s, got %s", sess.ID, got.ID)
	}
	if got.ChatID != "chat123" {
		t.Fatalf("expected chatID chat123, got %s", got.ChatID)
	}
}

func TestStore_FindSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.CreateSession(ctx, "telegram", "chat456", "agent1")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	found, err := s.FindSession(ctx, "telegram", "chat456")
	if err != nil {
		t.Fatalf("finding session: %v", err)
	}
	if found.ChatID != "chat456" {
		t.Fatalf("expected chatID chat456, got %s", found.ChatID)
	}
}

func TestStore_AppendAndGetMessages(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "telegram", "chat789", "agent1")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "hi there"}}},
	}
	if err := s.AppendMessages(ctx, sess.ID, msgs); err != nil {
		t.Fatalf("appending messages: %v", err)
	}

	got, err := s.GetMessages(ctx, sess.ID, 0)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "user" {
		t.Fatalf("expected first message role user, got %s", got[0].Role)
	}
	if got[0].Content[0].Text != "hello" {
		t.Fatalf("expected first message text hello, got %s", got[0].Content[0].Text)
	}
	if got[1].Content[0].Text != "hi there" {
		t.Fatalf("expected second message text 'hi there', got %s", got[1].Content[0].Text)
	}
}

func TestStore_GetMessages_Limit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "telegram", "chat_limit", "agent1")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}

	// Append 5 messages.
	for i := 0; i < 5; i++ {
		msg := []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg"}}},
		}
		if err := s.AppendMessages(ctx, sess.ID, msg); err != nil {
			t.Fatalf("appending message %d: %v", i, err)
		}
	}

	// Get last 3.
	got, err := s.GetMessages(ctx, sess.ID, 3)
	if err != nil {
		t.Fatalf("getting messages: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(got))
	}
}
