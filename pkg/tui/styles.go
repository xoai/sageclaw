package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Tab bar.
	activeTabStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("57")).
			Padding(0, 2)

	inactiveTabStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Padding(0, 2)

	// Content area.
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("170"))

	subtitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	// Events.
	eventToolCall = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))    // Blue
	eventToolResult = lipgloss.NewStyle().Foreground(lipgloss.Color("37"))  // Cyan
	eventChunk    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))   // Light gray
	eventError    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))   // Red
	eventStart    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))    // Green
	eventDone     = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))    // Green

	// Status bar.
	statusBarStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("229")).
			Background(lipgloss.Color("236")).
			Padding(0, 1)

	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)
