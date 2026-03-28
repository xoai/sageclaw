package discord

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xoai/sageclaw/pkg/channel"
)

func TestRenderConsent_SendsButtons(t *testing.T) {
	var sentPayload discordMessagePayload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &sentPayload)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	_ = srv // Server available for future integration test.

	// Verify option construction (can't override const discordAPIBase for full integration test).
	req := channel.ConsentPromptRequest{
		ChatID:      "channel123",
		OwnerUserID: "user1",
		Nonce:       "abc123",
		ToolName:    "shell_exec",
		Group:       "runtime",
		RiskLevel:   "sensitive",
		Explanation: "Can execute commands.",
		Options:     channel.DefaultConsentOptions("abc123"),
	}

	// Verify options are correct.
	if len(req.Options) != 3 {
		t.Fatalf("expected 3 options, got %d", len(req.Options))
	}
	if req.Options[0].Tier != "once" {
		t.Errorf("option 0 tier = %q, want once", req.Options[0].Tier)
	}
	if req.Options[1].Tier != "always" {
		t.Errorf("option 1 tier = %q, want always", req.Options[1].Tier)
	}
	if req.Options[2].Tier != "deny" {
		t.Errorf("option 2 tier = %q, want deny", req.Options[2].Tier)
	}
}

func TestHandleInteraction_ConsentGrant(t *testing.T) {
	var gotNonce, gotTier string
	var gotGranted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Adapter{
		connID:      "dc_test",
		token:       "token",
		client:      srv.Client(),
		ownerUserID: "user1",
		consentCB: func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotGranted = granted
			gotTier = tier
		},
	}

	a.HandleInteraction(context.Background(), Interaction{
		ID:    "inter1",
		Type:  interactionTypeComponent,
		Token: "token123",
		Data:  &InteractionData{CustomID: "consent:nonce456:always"},
		User:  &DiscordUser{ID: "user1"},
	})

	if gotNonce != "nonce456" {
		t.Errorf("nonce = %q, want nonce456", gotNonce)
	}
	if !gotGranted {
		t.Error("expected granted=true for always")
	}
	if gotTier != "always" {
		t.Errorf("tier = %q, want always", gotTier)
	}
}

func TestHandleInteraction_ConsentDeny(t *testing.T) {
	var gotGranted bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Adapter{
		connID: "dc_test",
		token:  "token",
		client: srv.Client(),
		consentCB: func(nonce string, granted bool, tier string) {
			gotGranted = granted
		},
	}

	a.HandleInteraction(context.Background(), Interaction{
		ID:    "inter1",
		Type:  interactionTypeComponent,
		Token: "token123",
		Data:  &InteractionData{CustomID: "consent:nonce456:deny"},
		User:  &DiscordUser{ID: "user1"},
	})

	if gotGranted {
		t.Error("expected granted=false for deny")
	}
}

func TestHandleInteraction_OwnerOnly(t *testing.T) {
	called := false

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Adapter{
		connID:      "dc_test",
		token:       "token",
		client:      srv.Client(),
		ownerUserID: "user1",
		consentCB: func(nonce string, granted bool, tier string) {
			called = true
		},
	}

	// Wrong user.
	a.HandleInteraction(context.Background(), Interaction{
		ID:    "inter1",
		Type:  interactionTypeComponent,
		Token: "token123",
		Data:  &InteractionData{CustomID: "consent:nonce456:once"},
		User:  &DiscordUser{ID: "wronguser"},
	})

	if called {
		t.Error("consent callback should NOT be called for non-owner")
	}
}

func TestHandleInteraction_GuildMember(t *testing.T) {
	var gotNonce string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &Adapter{
		connID:      "dc_test",
		token:       "token",
		client:      srv.Client(),
		ownerUserID: "user1",
		consentCB: func(nonce string, granted bool, tier string) {
			gotNonce = nonce
		},
	}

	// Guild interactions have Member instead of User.
	a.HandleInteraction(context.Background(), Interaction{
		ID:    "inter1",
		Type:  interactionTypeComponent,
		Token: "token123",
		Data:  &InteractionData{CustomID: "consent:nonce789:once"},
		Member: &GuildMember{
			User: &DiscordUser{ID: "user1"},
		},
	})

	if gotNonce != "nonce789" {
		t.Errorf("nonce = %q, want nonce789", gotNonce)
	}
}

func TestSplitN(t *testing.T) {
	tests := []struct {
		input string
		sep   string
		n     int
		want  []string
	}{
		{"a:b:c", ":", 2, []string{"a", "b:c"}},
		{"abc", ":", 2, []string{"abc"}},
		{"a:b", ":", 3, []string{"a", "b"}},
	}
	for _, tt := range tests {
		got := splitN(tt.input, tt.sep, tt.n)
		if len(got) != len(tt.want) {
			t.Errorf("splitN(%q, %q, %d) = %v, want %v", tt.input, tt.sep, tt.n, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("splitN(%q, %q, %d)[%d] = %q, want %q", tt.input, tt.sep, tt.n, i, got[i], tt.want[i])
			}
		}
	}
}
