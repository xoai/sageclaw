package zalo

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
		{"allow abc123", "abc123", true, "once", true},
		{"ALWAYS abc123", "abc123", true, "always", true},
		{"always abc123", "abc123", true, "always", true},
		{"DENY abc123", "abc123", false, "deny", true},
		{"deny abc123", "abc123", false, "deny", true},
		{"  ALLOW abc123  ", "abc123", true, "once", true},
		{"ALLOW ", "", false, "", false},   // No nonce.
		{"ALLOW", "", false, "", false},    // No space + nonce.
		{"hello world", "", false, "", false},
		{"", "", false, "", false},
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
		if nonce != tt.nonce {
			t.Errorf("ParseConsentReply(%q): nonce = %q, want %q", tt.text, nonce, tt.nonce)
		}
		if granted != tt.granted {
			t.Errorf("ParseConsentReply(%q): granted = %v, want %v", tt.text, granted, tt.granted)
		}
		if tier != tt.tier {
			t.Errorf("ParseConsentReply(%q): tier = %q, want %q", tt.text, tier, tt.tier)
		}
	}
}

func TestHandleConsentText_OwnerOnly(t *testing.T) {
	called := false

	a := New("zl_test", "oa1", "secret", "token",
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
	var gotNonce, gotTier string

	a := New("zl_test", "oa1", "secret", "token",
		WithOwnerUserID("owner1"),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotTier = tier
		}),
	)

	consumed := a.HandleConsentText("owner1", "ALWAYS nonce456")
	if !consumed {
		t.Error("should consume consent text")
	}
	if gotNonce != "nonce456" {
		t.Errorf("nonce = %q, want nonce456", gotNonce)
	}
	if gotTier != "always" {
		t.Errorf("tier = %q, want always", gotTier)
	}
}
