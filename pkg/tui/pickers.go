package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Picker messages ---

// AgentSelectedMsg is sent when a user picks an agent.
type AgentSelectedMsg struct{ Agent AgentInfo }

// SessionSelectedMsg is sent when a user picks a session.
type SessionSelectedMsg struct {
	Session  SessionInfo
	IsNew    bool
}

// ModelSelectedMsg is sent when a user picks a model.
type ModelSelectedMsg struct{ Model ModelInfo }

// PickerCancelledMsg is sent when the user cancels a picker with Esc.
type PickerCancelledMsg struct{ Overlay OverlayType }

// --- Agent picker ---

type agentItem struct{ info AgentInfo }

func (i agentItem) Title() string       { return fmt.Sprintf("%s %s", i.info.Avatar, i.info.Name) }
func (i agentItem) Description() string { return i.info.Role }
func (i agentItem) FilterValue() string { return i.info.Name + " " + i.info.Role }

// AgentPicker wraps list.Model for agent selection.
type AgentPicker struct {
	list   list.Model
	agents []AgentInfo
}

// NewAgentPicker creates an agent picker from a list of agents.
func NewAgentPicker(agents []AgentInfo, width, height int) AgentPicker {
	items := make([]list.Item, len(agents))
	for i, a := range agents {
		items[i] = agentItem{info: a}
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, width-4, height-4)
	l.Title = "Select Agent"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	return AgentPicker{list: l, agents: agents}
}

// Resize updates picker dimensions.
func (p *AgentPicker) Resize(width, height int) {
	p.list.SetSize(width-4, height-4)
}

// Update handles key events for the agent picker.
func (p AgentPicker) Update(msg tea.Msg) (AgentPicker, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if item, ok := p.list.SelectedItem().(agentItem); ok {
				return p, func() tea.Msg { return AgentSelectedMsg{Agent: item.info} }
			}
		case "esc":
			return p, func() tea.Msg { return PickerCancelledMsg{Overlay: OverlayAgentPicker} }
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the agent picker.
func (p AgentPicker) View() string {
	return p.list.View()
}

// --- Session picker ---

type sessionItem struct {
	info  SessionInfo
	isNew bool
}

func (i sessionItem) Title() string {
	if i.isNew {
		return "✨ New session"
	}
	title := i.info.Title
	if title == "" {
		title = "Untitled session"
	}
	return fmt.Sprintf("💬 %s", title)
}

func (i sessionItem) Description() string {
	if i.isNew {
		return "Start a fresh conversation"
	}
	parts := []string{}
	if i.info.CreatedAt != "" {
		if t, err := time.Parse(time.RFC3339, i.info.CreatedAt); err == nil {
			parts = append(parts, relativeTime(t))
		}
	}
	if i.info.TokensUsed > 0 {
		parts = append(parts, formatTokens(i.info.TokensUsed))
	}
	return strings.Join(parts, " · ")
}

func (i sessionItem) FilterValue() string {
	if i.isNew {
		return "new session"
	}
	return i.info.Title + " " + i.info.ID
}

// SessionPicker wraps list.Model for session selection.
type SessionPicker struct {
	list     list.Model
	sessions []SessionInfo
}

// NewSessionPicker creates a session picker. "New session" is always first.
func NewSessionPicker(sessions []SessionInfo, width, height int) SessionPicker {
	items := make([]list.Item, 0, len(sessions)+1)
	items = append(items, sessionItem{isNew: true})
	for _, s := range sessions {
		items = append(items, sessionItem{info: s})
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, width-4, height-4)
	l.Title = "Select Session"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	return SessionPicker{list: l, sessions: sessions}
}

// Resize updates picker dimensions.
func (p *SessionPicker) Resize(width, height int) {
	p.list.SetSize(width-4, height-4)
}

// Update handles key events for the session picker.
func (p SessionPicker) Update(msg tea.Msg) (SessionPicker, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if item, ok := p.list.SelectedItem().(sessionItem); ok {
				return p, func() tea.Msg {
					return SessionSelectedMsg{Session: item.info, IsNew: item.isNew}
				}
			}
		case "esc":
			return p, func() tea.Msg { return PickerCancelledMsg{Overlay: OverlaySessionPicker} }
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the session picker.
func (p SessionPicker) View() string {
	return p.list.View()
}

// --- Model picker ---

type modelItem struct{ info ModelInfo }

func (i modelItem) Title() string       { return i.info.Name }
func (i modelItem) Description() string { return fmt.Sprintf("%s · %s", i.info.Provider, i.info.Tier) }
func (i modelItem) FilterValue() string { return i.info.Name + " " + i.info.Provider }

// ModelPicker wraps list.Model for model selection.
type ModelPicker struct {
	list   list.Model
	models []ModelInfo
}

// NewModelPicker creates a model picker.
func NewModelPicker(models []ModelInfo, width, height int) ModelPicker {
	items := make([]list.Item, len(models))
	for i, m := range models {
		items[i] = modelItem{info: m}
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, width-4, height-4)
	l.Title = "Select Model"
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.Styles.Title = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	return ModelPicker{list: l, models: models}
}

// Resize updates picker dimensions.
func (p *ModelPicker) Resize(width, height int) {
	p.list.SetSize(width-4, height-4)
}

// Update handles key events for the model picker.
func (p ModelPicker) Update(msg tea.Msg) (ModelPicker, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if item, ok := p.list.SelectedItem().(modelItem); ok {
				return p, func() tea.Msg { return ModelSelectedMsg{Model: item.info} }
			}
		case "esc":
			return p, func() tea.Msg { return PickerCancelledMsg{Overlay: OverlayModelPicker} }
		}
	}

	var cmd tea.Cmd
	p.list, cmd = p.list.Update(msg)
	return p, cmd
}

// View renders the model picker.
func (p ModelPicker) View() string {
	return p.list.View()
}

// --- Helpers ---

// relativeTime formats a time as a human-readable relative duration.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1d ago"
		}
		return fmt.Sprintf("%dd ago", days)
	}
}
