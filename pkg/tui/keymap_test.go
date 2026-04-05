package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestKeyHandler_CtrlC_ClearsInput(t *testing.T) {
	kh := NewKeyHandler()
	mode := ThemeDark
	m := AppModel{
		chat:    NewChatView(80, 24, mode),
		theme:   NewTheme(mode),
		overlay: OverlayNone,
	}
	m.chat.input.SetValue("some text")

	// First Ctrl+C: clears input.
	cmd := kh.HandleGlobal(tea.KeyMsg{Type: tea.KeyCtrlC}, &m)
	if cmd != nil {
		t.Error("first Ctrl+C with non-empty input should return nil (clear, don't quit)")
	}
	if m.chat.InputValue() != "" {
		t.Error("input should be cleared after first Ctrl+C")
	}
}

func TestKeyHandler_CtrlC_QuitsWhenEmpty(t *testing.T) {
	kh := NewKeyHandler()
	mode := ThemeDark
	m := AppModel{
		chat:    NewChatView(80, 24, mode),
		theme:   NewTheme(mode),
		overlay: OverlayNone,
	}

	// Ctrl+C with empty input: quits.
	cmd := kh.HandleGlobal(tea.KeyMsg{Type: tea.KeyCtrlC}, &m)
	if cmd == nil {
		t.Error("Ctrl+C with empty input should return quit cmd")
	}
}

func TestKeyHandler_CtrlL(t *testing.T) {
	kh := NewKeyHandler()
	mode := ThemeDark
	m := AppModel{
		chat:    NewChatView(80, 24, mode),
		theme:   NewTheme(mode),
		overlay: OverlayNone,
	}
	m.chat.AddUserMessage("hello")
	if len(m.chat.messages) != 1 {
		t.Fatal("expected 1 message")
	}

	cmd := kh.HandleGlobal(tea.KeyMsg{Type: tea.KeyCtrlL}, &m)
	if cmd != nil {
		t.Error("Ctrl+L should return nil")
	}
	if len(m.chat.messages) != 0 {
		t.Error("messages should be cleared")
	}
}
