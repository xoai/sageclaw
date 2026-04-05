package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHelpScreen_View(t *testing.T) {
	screen := NewHelpScreen(NewTheme(ThemeDark), 80, 24)
	view := screen.View()

	if !strings.Contains(view, "/help") {
		t.Error("help screen should contain /help")
	}
	if !strings.Contains(view, "/agent") {
		t.Error("help screen should contain /agent")
	}
	if !strings.Contains(view, "Esc") {
		t.Error("help screen should show Esc hint")
	}
}

func TestHelpScreen_EscCloses(t *testing.T) {
	screen := NewHelpScreen(NewTheme(ThemeDark), 80, 24)

	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}

	msg := cmd()
	cancelled, ok := msg.(PickerCancelledMsg)
	if !ok {
		t.Fatalf("expected PickerCancelledMsg, got %T", msg)
	}
	if cancelled.Overlay != OverlayHelp {
		t.Error("wrong overlay type")
	}
}

func TestStatusScreen_View(t *testing.T) {
	screen := NewStatusScreen(NewTheme(ThemeDark), 80, 24)
	screen.SetHealth(HealthInfo{Status: "ok", Providers: 3, Agents: 2}, nil)
	view := screen.View()

	if !strings.Contains(view, "ok") {
		t.Error("status screen should show status")
	}
	if !strings.Contains(view, "3") {
		t.Error("status screen should show provider count")
	}
}

func TestStatusScreen_Error(t *testing.T) {
	screen := NewStatusScreen(NewTheme(ThemeDark), 80, 24)
	screen.SetHealth(HealthInfo{}, fmt.Errorf("connection refused"))
	view := screen.View()

	if !strings.Contains(view, "connection refused") {
		t.Error("status screen should show error")
	}
}

func TestSettingsScreen_Toggle(t *testing.T) {
	screen := NewSettingsScreen(NewTheme(ThemeDark), true, 80, 24)

	// Press 't' to toggle thinking.
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if screen.showThinking {
		t.Error("thinking should be toggled off")
	}

	// Toggle back.
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	if !screen.showThinking {
		t.Error("thinking should be toggled on")
	}
}

func TestSettingsScreen_EscSaves(t *testing.T) {
	screen := NewSettingsScreen(NewTheme(ThemeDark), true, 80, 24)

	// Toggle off, then esc.
	screen, _ = screen.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	_, cmd := screen.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}

	msg := cmd()
	changed, ok := msg.(settingsChangedMsg)
	if !ok {
		t.Fatalf("expected settingsChangedMsg, got %T", msg)
	}
	if changed.ShowThinking {
		t.Error("should carry the toggled-off value")
	}
}
