package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xoai/sageclaw/pkg/channel"
)

func TestRenderConsent_SendsInlineKeyboard(t *testing.T) {
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sentBody = r.FormValue("reply_markup")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New("tg_test", "token", WithBaseURL(srv.URL))

	err := a.RenderConsent(context.Background(), channel.ConsentPromptRequest{
		ChatID:      "12345",
		OwnerUserID: "user1",
		Nonce:       "abc123",
		ToolName:    "shell_exec",
		Group:       "runtime",
		RiskLevel:   "sensitive",
		Explanation: "Can execute commands.",
		Options:     channel.DefaultConsentOptions("abc123"),
	})
	if err != nil {
		t.Fatalf("RenderConsent: %v", err)
	}

	// Verify keyboard was sent.
	var kb inlineKeyboardMarkup
	if err := json.Unmarshal([]byte(sentBody), &kb); err != nil {
		t.Fatalf("parsing keyboard: %v", err)
	}
	if len(kb.InlineKeyboard) != 3 {
		t.Errorf("expected 3 button rows, got %d", len(kb.InlineKeyboard))
	}

	// Check callback data format.
	expected := []string{
		"consent:abc123:once",
		"consent:abc123:always",
		"consent:abc123:deny",
	}
	for i, row := range kb.InlineKeyboard {
		if len(row) != 1 {
			t.Errorf("row %d: expected 1 button, got %d", i, len(row))
			continue
		}
		if row[0].CallbackData != expected[i] {
			t.Errorf("row %d: callback = %q, want %q", i, row[0].CallbackData, expected[i])
		}
	}
}

func TestHandleCallbackQuery_ConsentGrant(t *testing.T) {
	var gotNonce, gotTier string
	var gotGranted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New("tg_test", "token",
		WithBaseURL(srv.URL),
		WithOwnerUserID("111"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotGranted = granted
			gotTier = tier
		}),
	)

	a.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "query1",
		From: TelegramUser{ID: 111},
		Data: "consent:nonce123:once",
		Message: &TelegramMessage{
			MessageID: 1,
			Chat:      TelegramChat{ID: 12345},
		},
	})

	if gotNonce != "nonce123" {
		t.Errorf("nonce = %q, want nonce123", gotNonce)
	}
	if !gotGranted {
		t.Error("expected granted=true for once")
	}
	if gotTier != "once" {
		t.Errorf("tier = %q, want once", gotTier)
	}
}

func TestHandleCallbackQuery_ConsentDeny(t *testing.T) {
	var gotGranted bool
	var gotTier string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New("tg_test", "token",
		WithBaseURL(srv.URL),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotGranted = granted
			gotTier = tier
		}),
	)

	a.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "query1",
		From: TelegramUser{ID: 111},
		Data: "consent:nonce123:deny",
	})

	if gotGranted {
		t.Error("expected granted=false for deny")
	}
	if gotTier != "deny" {
		t.Errorf("tier = %q, want deny", gotTier)
	}
}

func TestHandleCallbackQuery_OwnerOnly(t *testing.T) {
	called := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New("tg_test", "token",
		WithBaseURL(srv.URL),
		WithOwnerUserID("111"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			called = true
		}),
	)

	// Wrong user tries to respond.
	a.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "query1",
		From: TelegramUser{ID: 999}, // Not the owner.
		Data: "consent:nonce123:once",
	})

	if called {
		t.Error("consent callback should NOT be called for non-owner")
	}
}

func TestHandleCallbackQuery_NonConsentIgnored(t *testing.T) {
	called := false

	a := New("tg_test", "token",
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			called = true
		}),
	)

	a.handleCallbackQuery(context.Background(), &CallbackQuery{
		ID:   "query1",
		From: TelegramUser{ID: 111},
		Data: "other:data",
	})

	if called {
		t.Error("consent callback should NOT be called for non-consent data")
	}
}
