package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	localbus "github.com/xoai/sageclaw/pkg/bus/local"
)

func TestWhatsApp_Verification(t *testing.T) {
	adapter := New("phone123", "token", "my-verify-token")

	req := httptest.NewRequest("GET", "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=my-verify-token&hub.challenge=challenge123", nil)
	w := httptest.NewRecorder()
	adapter.handleVerify(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "challenge123" {
		t.Fatalf("expected challenge, got %s", w.Body.String())
	}
}

func TestWhatsApp_VerificationFail(t *testing.T) {
	adapter := New("phone123", "token", "my-verify-token")

	req := httptest.NewRequest("GET", "/webhook/whatsapp?hub.mode=subscribe&hub.verify_token=wrong", nil)
	w := httptest.NewRecorder()
	adapter.handleVerify(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}
}

func TestWhatsApp_ReceivesMessage(t *testing.T) {
	msgBus := localbus.New()
	adapter := New("phone123", "token", "verify")
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

	payload := WebhookPayload{
		Object: "whatsapp_business_account",
		Entry: []WebhookEntry{{
			Changes: []WebhookChange{{
				Field: "messages",
				Value: ChangeValue{
					Messages: []WAMessage{{
						ID:   "msg1",
						From: "1234567890",
						Type: "text",
						Text: struct {
							Body string `json:"body"`
						}{Body: "Hello from WhatsApp"},
					}},
				},
			}},
		}},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest("POST", "/webhook/whatsapp", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	adapter.handleWebhook(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if len(received) == 0 {
		t.Fatal("expected inbound message")
	}
	if received[0].Channel != "whatsapp" {
		t.Fatalf("expected whatsapp channel, got %s", received[0].Channel)
	}
	if received[0].ChatID != "1234567890" {
		t.Fatalf("expected from number, got %s", received[0].ChatID)
	}
}
