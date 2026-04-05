package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// KeyHandler provides centralized keyboard shortcut handling for the app.
type KeyHandler struct {
	lastCtrlC time.Time // For double-tap Ctrl+C exit.
}

// NewKeyHandler creates a KeyHandler.
func NewKeyHandler() KeyHandler {
	return KeyHandler{}
}

// HandleGlobal processes global shortcuts that work regardless of overlay state.
// Returns a tea.Cmd if the key was handled, nil otherwise.
func (k *KeyHandler) HandleGlobal(msg tea.KeyMsg, m *AppModel) tea.Cmd {
	switch msg.String() {
	case "ctrl+c":
		// During overlay: don't quit, let overlay handle it.
		if m.overlay != OverlayNone {
			return nil
		}
		// Double-tap: first clears input, second quits.
		now := time.Now()
		if now.Sub(k.lastCtrlC) < 500*time.Millisecond {
			return tea.Quit
		}
		k.lastCtrlC = now

		// First tap: clear input if non-empty, otherwise quit.
		if strings.TrimSpace(m.chat.InputValue()) != "" {
			m.chat.ResetInput()
			return nil
		}
		return tea.Quit

	case "ctrl+l":
		// Clear chat display (keep history, just reset viewport).
		if m.overlay == OverlayNone {
			m.chat.ClearDisplay()
			return nil
		}
	}

	return nil
}

// ClearDisplay clears all messages from the chat display.
func (c *ChatView) ClearDisplay() {
	c.messages = nil
	c.rebuildViewport()
}
