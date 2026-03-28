package zalobot

import "testing"

func TestParseConsentReply(t *testing.T) {
	tests := []struct {
		text    string
		nonce   string
		granted bool
		tier    string
		matched bool
	}{
		{"ALLOW abc123", "abc123", true, "once", true},
		{"ALWAYS abc123", "abc123", true, "always", true},
		{"DENY abc123", "abc123", false, "deny", true},
		{"allow abc123", "abc123", true, "once", true},
		{"hello world", "", false, "", false},
		{"ALLOW", "", false, "", false},
	}

	for _, tt := range tests {
		nonce, granted, tier, matched := ParseConsentReply(tt.text)
		if matched != tt.matched {
			t.Errorf("ParseConsentReply(%q): matched = %v, want %v", tt.text, matched, tt.matched)
			continue
		}
		if !matched {
			continue
		}
		if nonce != tt.nonce || granted != tt.granted || tier != tt.tier {
			t.Errorf("ParseConsentReply(%q) = (%q, %v, %q), want (%q, %v, %q)",
				tt.text, nonce, granted, tier, tt.nonce, tt.granted, tt.tier)
		}
	}
}

func TestHandleConsentText_OwnerOnly(t *testing.T) {
	called := false

	a := New("zb_test", "token",
		WithOwnerUserID("owner1"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			called = true
		}),
	)

	consumed := a.HandleConsentText("wrong_user", "ALLOW abc123")
	if !consumed {
		t.Error("should consume consent text")
	}
	if called {
		t.Error("callback should NOT be called for non-owner")
	}
}

func TestHandleConsentText_Success(t *testing.T) {
	var gotNonce string
	var gotGranted bool

	a := New("zb_test", "token",
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotGranted = granted
		}),
	)

	consumed := a.HandleConsentText("user1", "DENY nonce789")
	if !consumed {
		t.Error("should consume consent text")
	}
	if gotNonce != "nonce789" {
		t.Errorf("nonce = %q, want nonce789", gotNonce)
	}
	if gotGranted {
		t.Error("expected granted=false for deny")
	}
}
