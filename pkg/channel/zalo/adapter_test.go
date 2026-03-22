package zalo

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	localbus "github.com/xoai/sageclaw/pkg/bus/local"
)

func TestWebhook_ReceivesMessage(t *testing.T) {
	msgBus := localbus.New()
	adapter := New("oa123", "", "token123")
	adapter.msgBus = msgBus

	var received []bus.Envelope
	done := make(chan struct{}, 1)
	msgBus.SubscribeInbound(context.Background(), func(env bus.Envelope) {
		received = append(received, env)
		select {
		case done <- struct{}{}:
		default:
		}
	})

	event := WebhookEvent{
		EventName: "user_send_text",
		Sender:    Sender{ID: "user456"},
		Message:   Message{Text: "Hello from Zalo"},
	}
	body, _ := json.Marshal(event)

	req := httptest.NewRequest("POST", "/webhook/zalo", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	adapter.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}

	if len(received) == 0 {
		t.Fatal("expected inbound message")
	}
	if received[0].Channel != "zalo" {
		t.Fatalf("expected zalo channel, got %s", received[0].Channel)
	}
	if received[0].ChatID != "user456" {
		t.Fatalf("expected user456, got %s", received[0].ChatID)
	}
}

func TestWebhook_VerifySignature(t *testing.T) {
	secret := "mysecret"
	adapter := New("oa123", secret, "token123")
	adapter.msgBus = localbus.New()

	event := WebhookEvent{EventName: "user_send_text", Sender: Sender{ID: "u1"}, Message: Message{Text: "hi"}}
	body, _ := json.Marshal(event)

	// Valid signature.
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := hex.EncodeToString(mac.Sum(nil))

	req := httptest.NewRequest("POST", "/webhook/zalo", bytes.NewReader(body))
	req.Header.Set("X-ZEvent-Signature", sig)
	w := httptest.NewRecorder()
	adapter.handleWebhook(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid sig, got %d", w.Code)
	}

	// Invalid signature.
	req2 := httptest.NewRequest("POST", "/webhook/zalo", bytes.NewReader(body))
	req2.Header.Set("X-ZEvent-Signature", "invalid")
	w2 := httptest.NewRecorder()
	adapter.handleWebhook(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for invalid sig, got %d", w2.Code)
	}
}

func TestWebhook_Verify(t *testing.T) {
	adapter := New("oa123", "", "token123")
	req := httptest.NewRequest("GET", "/webhook/zalo", nil)
	w := httptest.NewRecorder()
	adapter.handleVerify(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
