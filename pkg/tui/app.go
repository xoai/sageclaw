package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// Tab names.
const (
	tabActivity = 0
	tabSessions = 1
	tabMemory   = 2
	tabCron     = 3
)

var tabNames = []string{"Activity", "Sessions", "Memory", "Cron"}

// EventMsg wraps an agent event for the bubbletea update loop.
type EventMsg agent.Event

// Model is the main TUI model.
type Model struct {
	activeTab int
	activity  activityModel
	sessions  sessionsModel
	memory    memoryModel
	cron      cronModel
	width     int
	height    int
	quitting  bool
}

// New creates a new TUI model.
func New(store *sqlite.Store, memEngine memory.MemoryEngine) Model {
	return Model{
		sessions: sessionsModel{store: store},
		memory:   memoryModel{engine: memEngine},
		cron:     cronModel{store: store},
	}
}

// EventHandler returns an agent.EventHandler that sends events to the TUI.
func EventHandler(p *tea.Program) agent.EventHandler {
	return func(e agent.Event) {
		if p != nil {
			p.Send(EventMsg(e))
		}
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(tabNames)
			m.refreshTab()
			return m, nil
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
			m.refreshTab()
			return m, nil
		case "1":
			m.activeTab = tabActivity
		case "2":
			m.activeTab = tabSessions
			m.sessions.refresh()
		case "3":
			m.activeTab = tabMemory
			m.memory.refresh()
		case "4":
			m.activeTab = tabCron
			m.cron.refresh()
		case "r":
			m.refreshTab()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case EventMsg:
		m.activity.addEvent(agent.Event(msg))
	}

	return m, nil
}

func (m *Model) refreshTab() {
	switch m.activeTab {
	case tabSessions:
		m.sessions.refresh()
	case tabMemory:
		m.memory.refresh()
	case tabCron:
		m.cron.refresh()
	}
}

func (m Model) View() string {
	if m.quitting {
		return ""
	}

	width := m.width
	if width == 0 {
		width = 80
	}
	height := m.height
	if height == 0 {
		height = 24
	}

	var b strings.Builder

	// Header.
	b.WriteString(titleStyle.Render(" SageClaw "))
	b.WriteString("  ")

	// Tabs.
	for i, name := range tabNames {
		if i == m.activeTab {
			b.WriteString(activeTabStyle.Render(fmt.Sprintf(" %d %s ", i+1, name)))
		} else {
			b.WriteString(inactiveTabStyle.Render(fmt.Sprintf(" %d %s ", i+1, name)))
		}
	}
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", width))
	b.WriteString("\n")

	// Content.
	contentHeight := height - 5 // Header + status bar + padding.
	switch m.activeTab {
	case tabActivity:
		b.WriteString(m.activity.view(width, contentHeight))
	case tabSessions:
		b.WriteString(m.sessions.view(width, contentHeight))
	case tabMemory:
		b.WriteString(m.memory.view(width, contentHeight))
	case tabCron:
		b.WriteString(m.cron.view(width, contentHeight))
	}

	// Pad to fill screen.
	lines := strings.Count(b.String(), "\n")
	for i := lines; i < height-2; i++ {
		b.WriteString("\n")
	}

	// Status bar.
	status := statusBarStyle.Render(fmt.Sprintf(" Tab: switch view | 1-4: jump | r: refresh | q: quit "))
	b.WriteString("\n")
	b.WriteString(lipgloss.PlaceHorizontal(width, lipgloss.Left, status))

	return b.String()
}
