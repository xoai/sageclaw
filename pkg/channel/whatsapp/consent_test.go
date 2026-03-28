package whatsapp

import (
	"testing"
)

func TestHandleConsentReply_Allow(t *testing.T) {
	var gotNonce, gotTier string
	var gotGranted bool

	a := New("wa_test", "phone1", "token", "verify",
		WithOwnerUserID("1234567890"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotGranted = granted
			gotTier = tier
		}),
	)

	consumed := a.HandleConsentReply(WAMessage{
		From: "1234567890",
		Type: "interactive",
		Interactive: &WAInteractive{
			Type:        "button_reply",
			ButtonReply: &WAButtonReply{ID: "consent:nonce123:once", Title: "Allow once"},
		},
	})

	if !consumed {
		t.Error("should consume consent reply")
	}
	if gotNonce != "nonce123" {
		t.Errorf("nonce = %q, want nonce123", gotNonce)
	}
	if !gotGranted {
		t.Error("expected granted=true")
	}
	if gotTier != "once" {
		t.Errorf("tier = %q, want once", gotTier)
	}
}

func TestHandleConsentReply_Deny(t *testing.T) {
	var gotGranted bool

	a := New("wa_test", "phone1", "token", "verify",
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotGranted = granted
		}),
	)

	consumed := a.HandleConsentReply(WAMessage{
		From: "1234567890",
		Type: "interactive",
		Interactive: &WAInteractive{
			Type:        "button_reply",
			ButtonReply: &WAButtonReply{ID: "consent:nonce123:deny", Title: "Deny"},
		},
	})

	if !consumed {
		t.Error("should consume consent reply")
	}
	if gotGranted {
		t.Error("expected granted=false for deny")
	}
}

func TestHandleConsentReply_OwnerOnly(t *testing.T) {
	called := false

	a := New("wa_test", "phone1", "token", "verify",
		WithOwnerUserID("1234567890"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			called = true
		}),
	)

	consumed := a.HandleConsentReply(WAMessage{
		From: "9999999999", // Wrong user.
		Type: "interactive",
		Interactive: &WAInteractive{
			Type:        "button_reply",
			ButtonReply: &WAButtonReply{ID: "consent:nonce123:once", Title: "Allow"},
		},
	})

	if !consumed {
		t.Error("should still consume (but not action)")
	}
	if called {
		t.Error("callback should NOT be called for non-owner")
	}
}

func TestHandleConsentReply_NonConsent(t *testing.T) {
	a := New("wa_test", "phone1", "token", "verify")

	consumed := a.HandleConsentReply(WAMessage{
		From: "1234567890",
		Type: "text",
	})

	if consumed {
		t.Error("non-interactive message should not be consumed")
	}
}
