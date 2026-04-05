package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConsentModal_AllowOnce(t *testing.T) {
	req := ConsentRequest{
		Nonce:       "abc-123",
		ToolName:    "delete_file",
		RiskLevel:   "sensitive",
		Explanation: "Deletes a file",
		ToolInput:   `{"path":"/tmp/test"}`,
	}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)

	// Press 'a' for allow once.
	updated, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	_ = updated
	if cmd == nil {
		t.Fatal("expected a command from pressing 'a'")
	}

	msg := cmd()
	resp, ok := msg.(ConsentResponseMsg)
	if !ok {
		t.Fatalf("expected ConsentResponseMsg, got %T", msg)
	}
	if resp.Nonce != "abc-123" {
		t.Errorf("expected nonce abc-123, got %q", resp.Nonce)
	}
	if !resp.Granted {
		t.Error("expected granted=true for allow once")
	}
	if resp.Tier != "once" {
		t.Errorf("expected tier once, got %q", resp.Tier)
	}
}

func TestConsentModal_AlwaysAllow(t *testing.T) {
	req := ConsentRequest{Nonce: "n-1", ToolName: "list_files"}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)

	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil {
		t.Fatal("expected command")
	}

	resp := cmd().(ConsentResponseMsg)
	if !resp.Granted || resp.Tier != "always" {
		t.Errorf("expected granted=true tier=always, got granted=%v tier=%q", resp.Granted, resp.Tier)
	}
}

func TestConsentModal_Deny(t *testing.T) {
	req := ConsentRequest{Nonce: "n-2", ToolName: "exec"}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)

	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if cmd == nil {
		t.Fatal("expected command")
	}

	resp := cmd().(ConsentResponseMsg)
	if resp.Granted || resp.Tier != "deny" {
		t.Errorf("expected granted=false tier=deny, got granted=%v tier=%q", resp.Granted, resp.Tier)
	}
}

func TestConsentModal_EscDenies(t *testing.T) {
	req := ConsentRequest{Nonce: "n-3", ToolName: "exec"}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)

	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command")
	}

	resp := cmd().(ConsentResponseMsg)
	if resp.Granted {
		t.Error("esc should deny")
	}
}

func TestConsentModal_UnhandledKey(t *testing.T) {
	req := ConsentRequest{Nonce: "n-4", ToolName: "exec"}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)

	_, cmd := modal.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if cmd != nil {
		t.Error("unhandled key should produce no command")
	}
}

func TestConsentModal_View(t *testing.T) {
	req := ConsentRequest{
		Nonce:       "n-5",
		ToolName:    "delete_file",
		RiskLevel:   "sensitive",
		Explanation: "Removes a file permanently",
		ToolInput:   `{"path":"/important"}`,
	}
	modal := NewConsentModal(req, NewTheme(ThemeDark), 80, 24)
	view := modal.View()

	if !strings.Contains(view, "delete_file") {
		t.Error("view should contain tool name")
	}
	if !strings.Contains(view, "sensitive") {
		t.Error("view should contain risk level")
	}
	if !strings.Contains(view, "Allow once") {
		t.Error("view should contain action hints")
	}
	if !strings.Contains(view, "Deny") {
		t.Error("view should contain deny option")
	}
}

func TestOverlayType_String(t *testing.T) {
	tests := []struct {
		o    OverlayType
		want string
	}{
		{OverlayNone, "none"},
		{OverlayConsent, "consent"},
		{OverlayAgentPicker, "agent_picker"},
		{OverlayHelp, "help"},
	}
	for _, tt := range tests {
		if got := tt.o.String(); got != tt.want {
			t.Errorf("OverlayType(%d).String() = %q, want %q", tt.o, got, tt.want)
		}
	}
}

func TestParseConsentRequest_WithToolInput(t *testing.T) {
	raw := []byte(`{"nonce":"abc","tool_name":"exec","risk_level":"sensitive","explanation":"runs command","tool_input":"{\"cmd\":\"ls\"}"}`)
	req := parseConsentRequest(raw)

	if req.Nonce != "abc" {
		t.Errorf("nonce: got %q", req.Nonce)
	}
	if req.ToolInput != `{"cmd":"ls"}` {
		t.Errorf("tool_input: got %q", req.ToolInput)
	}
}
