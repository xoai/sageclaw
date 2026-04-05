package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Help Screen ---

// HelpScreen shows the slash command reference.
type HelpScreen struct {
	theme  Theme
	width  int
	height int
}

// NewHelpScreen creates a help screen.
func NewHelpScreen(theme Theme, width, height int) HelpScreen {
	return HelpScreen{theme: theme, width: width, height: height}
}

// Update handles key events (Esc to close).
func (h HelpScreen) Update(msg tea.Msg) (HelpScreen, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" || keyMsg.String() == "q" {
			return h, func() tea.Msg { return PickerCancelledMsg{Overlay: OverlayHelp} }
		}
	}
	return h, nil
}

// View renders the help screen.
func (h HelpScreen) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(h.theme.Accent).Render("Slash Commands")
	b.WriteString(title)
	b.WriteString("\n\n")

	cmds := AllCommands()
	nameWidth := 0
	for _, cmd := range cmds {
		if len(cmd.Name) > nameWidth {
			nameWidth = len(cmd.Name)
		}
	}

	nameStyle := lipgloss.NewStyle().Foreground(h.theme.Accent).Bold(true)
	descStyle := h.theme.Dim

	for _, cmd := range cmds {
		name := nameStyle.Render(fmt.Sprintf("  /%-*s", nameWidth, cmd.Name))
		desc := descStyle.Render("  " + cmd.Description)
		b.WriteString(name + desc + "\n")
	}

	b.WriteString("\n")
	b.WriteString(h.theme.Dim.Render("  Press Esc or q to close"))

	return lipgloss.Place(h.width, h.height, lipgloss.Center, lipgloss.Center, b.String())
}

// --- Status Screen ---

// StatusScreen shows system health info.
type StatusScreen struct {
	theme  Theme
	health HealthInfo
	err    error
	width  int
	height int
}

// statusLoadedMsg carries health data.
type statusLoadedMsg struct {
	Health HealthInfo
	Err    error
}

// NewStatusScreen creates a status screen.
func NewStatusScreen(theme Theme, width, height int) StatusScreen {
	return StatusScreen{theme: theme, width: width, height: height}
}

// LoadHealth returns a cmd to fetch health info.
func LoadHealth(client *TUIClient) tea.Cmd {
	return func() tea.Msg {
		health, err := client.GetHealth()
		return statusLoadedMsg{Health: health, Err: err}
	}
}

// SetHealth updates the displayed health data.
func (s *StatusScreen) SetHealth(health HealthInfo, err error) {
	s.health = health
	s.err = err
}

// Update handles key events.
func (s StatusScreen) Update(msg tea.Msg) (StatusScreen, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "esc" || keyMsg.String() == "q" {
			return s, func() tea.Msg { return PickerCancelledMsg{Overlay: OverlayStatus} }
		}
	}
	return s, nil
}

// View renders the status screen.
func (s StatusScreen) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(s.theme.Accent).Render("System Status")
	b.WriteString(title)
	b.WriteString("\n\n")

	if s.err != nil {
		b.WriteString(s.theme.ErrStyle.Render(fmt.Sprintf("  Error: %s", s.err)))
	} else {
		statusColor := s.theme.Success
		if s.health.Status != "ok" {
			statusColor = s.theme.Error
		}
		b.WriteString(fmt.Sprintf("  Status:    %s\n",
			lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(s.health.Status)))
		b.WriteString(fmt.Sprintf("  Providers: %d\n",
			s.health.Providers))
		b.WriteString(fmt.Sprintf("  Agents:    %d\n",
			s.health.Agents))
	}

	b.WriteString("\n")
	b.WriteString(s.theme.Dim.Render("  Press Esc or q to close"))

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, b.String())
}

// --- Settings Screen ---

// SettingsScreen shows toggleable display preferences.
type SettingsScreen struct {
	theme        Theme
	showThinking bool
	width        int
	height       int
}

// settingsChangedMsg carries updated settings back to the app.
type settingsChangedMsg struct {
	ShowThinking bool
}

// NewSettingsScreen creates a settings screen.
func NewSettingsScreen(theme Theme, showThinking bool, width, height int) SettingsScreen {
	return SettingsScreen{
		theme:        theme,
		showThinking: showThinking,
		width:        width,
		height:       height,
	}
}

// Update handles key events.
func (s SettingsScreen) Update(msg tea.Msg) (SettingsScreen, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "esc", "q":
			return s, func() tea.Msg {
				return settingsChangedMsg{ShowThinking: s.showThinking}
			}
		case "t":
			s.showThinking = !s.showThinking
		}
	}
	return s, nil
}

// View renders the settings screen.
func (s SettingsScreen) View() string {
	var b strings.Builder

	title := lipgloss.NewStyle().Bold(true).Foreground(s.theme.Accent).Render("Settings")
	b.WriteString(title)
	b.WriteString("\n\n")

	thinkStatus := s.theme.Dim.Render("off")
	if s.showThinking {
		thinkStatus = lipgloss.NewStyle().Foreground(s.theme.Success).Render("on")
	}
	b.WriteString(fmt.Sprintf("  [T] Show thinking:  %s\n", thinkStatus))

	b.WriteString("\n")
	b.WriteString(s.theme.Dim.Render("  Press Esc or q to save and close"))

	return lipgloss.Place(s.width, s.height, lipgloss.Center, lipgloss.Center, b.String())
}
