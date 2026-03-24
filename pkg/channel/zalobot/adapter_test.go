package zalobot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// mockBus captures published envelopes.
type mockBus struct {
	mu       sync.Mutex
	inbound  []bus.Envelope
	outbound []func(bus.Envelope)
}

func (m *mockBus) PublishInbound(_ context.Context, env bus.Envelope) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inbound = append(m.inbound, env)
	return nil
}
func (m *mockBus) SubscribeInbound(_ context.Context, _ func(bus.Envelope)) error { return nil }
func (m *mockBus) PublishOutbound(_ context.Context, env bus.Envelope) error {
	m.mu.Lock()
	handlers := append([]func(bus.Envelope){}, m.outbound...)
	m.mu.Unlock()
	for _, h := range handlers {
		h(env)
	}
	return nil
}
func (m *mockBus) SubscribeOutbound(_ context.Context, fn func(bus.Envelope)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.outbound = append(m.outbound, fn)
	return nil
}

func TestGetMe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/getMe" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(APIResponse[*BotUser]{
			OK: true,
			Result: &BotUser{
				ID:          "123456",
				AccountName: "bot.TestBot",
				AccountType: "BASIC",
			},
		})
	}))
	defer ts.Close()

	a := New("zb_test", "fake-token", WithBaseURL(ts.URL))
	bot, err := a.GetMe(context.Background())
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}
	if bot.ID != "123456" {
		t.Errorf("expected ID 123456, got %s", bot.ID)
	}
	if bot.AccountName != "bot.TestBot" {
		t.Errorf("expected account_name bot.TestBot, got %s", bot.AccountName)
	}
}

func TestHandleTextMessage(t *testing.T) {
	mb := &mockBus{}
	a := New("zb_test", "fake-token")
	a.msgBus = mb

	update := Update{
		EventName: "message.text.received",
		Message: ZBMessage{
			MessageID: "msg_001",
			From:      ZBFrom{ID: "user_1", DisplayName: "Alice"},
			Chat:      ZBChat{ID: "chat_1", ChatType: "PRIVATE"},
			Text:      "Hello bot!",
		},
	}

	a.handleUpdate(context.Background(), update)

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.inbound) != 1 {
		t.Fatalf("expected 1 inbound envelope, got %d", len(mb.inbound))
	}

	env := mb.inbound[0]
	if env.Channel != "zb_test" {
		t.Errorf("expected channel zb_test, got %s", env.Channel)
	}
	if env.ChatID != "chat_1" {
		t.Errorf("expected chat_id chat_1, got %s", env.ChatID)
	}
	if env.Kind != "dm" {
		t.Errorf("expected kind dm, got %s", env.Kind)
	}
	if !env.Mentioned {
		t.Error("expected mentioned=true for DM")
	}
	if len(env.Messages) != 1 || env.Messages[0].Content[0].Text != "Hello bot!" {
		t.Errorf("unexpected message content: %+v", env.Messages)
	}
}

func TestHandleGroupMessage(t *testing.T) {
	mb := &mockBus{}
	a := New("zb_test", "fake-token")
	a.msgBus = mb

	update := Update{
		EventName: "message.text.received",
		Message: ZBMessage{
			MessageID: "msg_002",
			From:      ZBFrom{ID: "user_2", DisplayName: "Bob"},
			Chat:      ZBChat{ID: "group_1", ChatType: "GROUP"},
			Text:      "Hey everyone",
		},
	}

	a.handleUpdate(context.Background(), update)

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.inbound) != 1 {
		t.Fatalf("expected 1 inbound envelope, got %d", len(mb.inbound))
	}

	env := mb.inbound[0]
	if env.Kind != "group" {
		t.Errorf("expected kind group, got %s", env.Kind)
	}
	if env.Mentioned {
		t.Error("expected mentioned=false for group without @mention")
	}
}

func TestHandleImageMessage(t *testing.T) {
	mb := &mockBus{}
	a := New("zb_test", "fake-token")
	a.msgBus = mb

	update := Update{
		EventName: "message.image.received",
		Message: ZBMessage{
			MessageID: "msg_003",
			From:      ZBFrom{ID: "user_1", DisplayName: "Alice"},
			Chat:      ZBChat{ID: "chat_1", ChatType: "PRIVATE"},
			Photo:     "https://example.com/photo.jpg",
			Caption:   "Check this out",
		},
	}

	a.handleUpdate(context.Background(), update)

	mb.mu.Lock()
	defer mb.mu.Unlock()
	if len(mb.inbound) != 1 {
		t.Fatalf("expected 1 inbound, got %d", len(mb.inbound))
	}

	content := mb.inbound[0].Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks (caption + image), got %d", len(content))
	}
	if content[0].Text != "Check this out" {
		t.Errorf("expected caption, got %s", content[0].Text)
	}
	if content[1].Text != "[Image attached]" {
		t.Errorf("expected [Image attached], got %s", content[1].Text)
	}
}

func TestSendMessage(t *testing.T) {
	var gotBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&gotBody)
		json.NewEncoder(w).Encode(APIResponse[SendResult]{
			OK:     true,
			Result: SendResult{MessageID: "resp_001"},
		})
	}))
	defer ts.Close()

	a := New("zb_test", "fake-token", WithBaseURL(ts.URL))
	err := a.sendMessage("chat_1", "Hello user!")
	if err != nil {
		t.Fatalf("sendMessage failed: %v", err)
	}
	if gotBody["chat_id"] != "chat_1" {
		t.Errorf("expected chat_id chat_1, got %s", gotBody["chat_id"])
	}
	if gotBody["text"] != "Hello user!" {
		t.Errorf("expected text 'Hello user!', got %s", gotBody["text"])
	}
}

func TestChunkText(t *testing.T) {
	// Short text — no chunking.
	chunks := chunkText("hello", 2000)
	if len(chunks) != 1 || chunks[0] != "hello" {
		t.Errorf("expected 1 chunk, got %d", len(chunks))
	}

	// Long text — should chunk at 2000.
	long := strings.Repeat("a", 4500)
	chunks = chunkText(long, 2000)
	if len(chunks) != 3 {
		t.Errorf("expected 3 chunks for 4500 chars, got %d", len(chunks))
	}
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != 4500 {
		t.Errorf("expected total 4500 chars, got %d", total)
	}
}

func TestSendResponse(t *testing.T) {
	var sentMessages []map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/getMe" {
			json.NewEncoder(w).Encode(APIResponse[*BotUser]{OK: true, Result: &BotUser{ID: "1", AccountName: "bot.Test"}})
			return
		}
		if r.URL.Path == "/sendMessage" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			sentMessages = append(sentMessages, body)
			json.NewEncoder(w).Encode(APIResponse[SendResult]{OK: true, Result: SendResult{MessageID: "r1"}})
			return
		}
	}))
	defer ts.Close()

	mb := &mockBus{}
	a := New("zb_test", "fake-token", WithBaseURL(ts.URL))

	// Start to subscribe outbound.
	a.Start(context.Background(), mb)
	defer a.Stop(context.Background())

	// Publish outbound for this adapter.
	mb.PublishOutbound(context.Background(), bus.Envelope{
		Channel: "zb_test",
		ChatID:  "chat_99",
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Response text"}}},
		},
	})

	if len(sentMessages) != 1 {
		t.Fatalf("expected 1 sent message, got %d", len(sentMessages))
	}
	if sentMessages[0]["chat_id"] != "chat_99" {
		t.Errorf("expected chat_id chat_99, got %s", sentMessages[0]["chat_id"])
	}
	if sentMessages[0]["text"] != "Response text" {
		t.Errorf("expected 'Response text', got %s", sentMessages[0]["text"])
	}
}

func TestGetUpdates_SingleObject(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// API returns a single update object, not an array.
		json.NewEncoder(w).Encode(APIResponse[Update]{
			OK: true,
			Result: Update{
				EventName: "message.text.received",
				Message: ZBMessage{
					MessageID: "msg_100",
					From:      ZBFrom{ID: "user_1", DisplayName: "Alice"},
					Chat:      ZBChat{ID: "chat_1", ChatType: "PRIVATE"},
					Text:      "Hello!",
				},
			},
		})
	}))
	defer ts.Close()

	a := New("zb_test", "fake-token", WithBaseURL(ts.URL))
	updates, err := a.getUpdates(context.Background())
	if err != nil {
		t.Fatalf("getUpdates failed: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if updates[0].Message.Text != "Hello!" {
		t.Errorf("expected 'Hello!', got %s", updates[0].Message.Text)
	}
}

func TestGetUpdates_Timeout408(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(APIResponse[Update]{
			OK:          false,
			ErrorCode:   408,
			Description: "Request timeout",
		})
	}))
	defer ts.Close()

	a := New("zb_test", "fake-token", WithBaseURL(ts.URL))
	updates, err := a.getUpdates(context.Background())
	if err != nil {
		t.Fatalf("408 should not be an error, got: %v", err)
	}
	if len(updates) != 0 {
		t.Errorf("expected 0 updates for timeout, got %d", len(updates))
	}
}

func TestNormalizeMessage_Sticker(t *testing.T) {
	msg := normalizeMessage("message.sticker.received", &ZBMessage{
		Sticker: "sticker_id_123",
	})
	if len(msg.Content) != 1 || msg.Content[0].Text != "[Sticker]" {
		t.Errorf("expected [Sticker], got %+v", msg.Content)
	}
}

func TestNormalizeMessage_Empty(t *testing.T) {
	msg := normalizeMessage("message.unsupported.received", &ZBMessage{})
	if len(msg.Content) != 1 || msg.Content[0].Text != "(empty message)" {
		t.Errorf("expected (empty message), got %+v", msg.Content)
	}
}
