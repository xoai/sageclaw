package sqlite

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
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

func TestStore_CreateSession_DefaultKindIsDm(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess, err := s.CreateSession(ctx, "tg_abc", "chat1", "agent1")
	if err != nil {
		t.Fatalf("creating session: %v", err)
	}
	if sess.Kind != "dm" {
		t.Fatalf("expected default kind 'dm', got %s", sess.Kind)
	}
}

func TestStore_DmAndGroupSeparateSessions(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	dm, err := s.CreateSessionWithKind(ctx, "tg_abc", "chat1", "agent1", "dm")
	if err != nil {
		t.Fatalf("creating dm session: %v", err)
	}

	group, err := s.CreateSessionWithKind(ctx, "tg_abc", "chat1", "agent1", "group")
	if err != nil {
		t.Fatalf("creating group session: %v", err)
	}

	if dm.ID == group.ID {
		t.Fatal("dm and group sessions should have different IDs")
	}
	if dm.Key == group.Key {
		t.Fatal("dm and group sessions should have different keys")
	}
}

func TestStore_FindSessionWithKind(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.CreateSessionWithKind(ctx, "tg_abc", "chat1", "agent1", "dm")
	s.CreateSessionWithKind(ctx, "tg_abc", "chat1", "agent1", "group")

	dm, err := s.FindSessionWithKind(ctx, "tg_abc", "chat1", "dm")
	if err != nil {
		t.Fatalf("finding dm session: %v", err)
	}
	if dm.Kind != "dm" {
		t.Fatalf("expected kind 'dm', got %s", dm.Kind)
	}

	group, err := s.FindSessionWithKind(ctx, "tg_abc", "chat1", "group")
	if err != nil {
		t.Fatalf("finding group session: %v", err)
	}
	if group.Kind != "group" {
		t.Fatalf("expected kind 'group', got %s", group.Kind)
	}
}

func TestStore_ThreadSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create parent group session.
	parent, err := s.CreateSessionWithKind(ctx, "tg_abc", "-100123", "agent1", "group")
	if err != nil {
		t.Fatalf("creating parent: %v", err)
	}

	// Create thread sub-session.
	thread, err := s.CreateSessionWithThread(ctx, "tg_abc", "-100123", "agent1", "99")
	if err != nil {
		t.Fatalf("creating thread session: %v", err)
	}

	if thread.SpawnedBy != parent.ID {
		t.Fatalf("expected SpawnedBy=%s, got %s", parent.ID, thread.SpawnedBy)
	}

	// Find by thread.
	found, err := s.FindSessionWithThread(ctx, "tg_abc", "-100123", "99")
	if err != nil {
		t.Fatalf("finding thread session: %v", err)
	}
	if found.ID != thread.ID {
		t.Fatalf("expected thread session ID %s, got %s", thread.ID, found.ID)
	}
}

func TestStore_ConnectionWithCredentials(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key, _ := GenerateKey()

	creds := map[string]string{"token": "bot123", "secret": "s3c"}
	blob, _ := EncryptCredentials(creds, key)

	conn := store.Connection{
		ID:           "tg_test1",
		Platform:     "telegram",
		Label:        "@testbot",
		Metadata:     "{}",
		Credentials:  blob,
		DmEnabled:    true,
		GroupEnabled: false,
		Status:       "active",
	}
	if err := s.CreateConnection(ctx, conn); err != nil {
		t.Fatalf("creating connection: %v", err)
	}

	got, err := s.GetConnection(ctx, "tg_test1")
	if err != nil {
		t.Fatalf("getting connection: %v", err)
	}
	if !got.DmEnabled {
		t.Fatal("expected DmEnabled=true")
	}
	if got.GroupEnabled {
		t.Fatal("expected GroupEnabled=false")
	}

	// Decrypt credentials.
	gotCreds, err := DecryptCredentials(got.Credentials, key)
	if err != nil {
		t.Fatalf("decrypting: %v", err)
	}
	if gotCreds["token"] != "bot123" {
		t.Fatalf("expected token=bot123, got %s", gotCreds["token"])
	}
	if gotCreds["secret"] != "s3c" {
		t.Fatalf("expected secret=s3c, got %s", gotCreds["secret"])
	}
}

func TestStore_UpdateConnectionPolicies(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	conn := store.Connection{
		ID: "tg_pol1", Platform: "telegram", Label: "test",
		Metadata: "{}", DmEnabled: true, GroupEnabled: true, Status: "active",
	}
	s.CreateConnection(ctx, conn)

	// Disable groups.
	err := s.UpdateConnection(ctx, "tg_pol1", map[string]any{"group_enabled": 0})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}

	got, _ := s.GetConnection(ctx, "tg_pol1")
	if got.GroupEnabled {
		t.Fatal("expected GroupEnabled=false after update")
	}
	if !got.DmEnabled {
		t.Fatal("DmEnabled should still be true")
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
