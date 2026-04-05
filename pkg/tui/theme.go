package tui

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme holds the color palette and lipgloss styles for the TUI.
type Theme struct {
	// Colors.
	Text       lipgloss.Color
	TextDim    lipgloss.Color
	TextBold   lipgloss.Color
	Accent     lipgloss.Color
	AccentDim  lipgloss.Color
	Border     lipgloss.Color
	UserBorder lipgloss.Color
	BotBorder  lipgloss.Color
	ToolBorder lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	Error      lipgloss.Color
	BgHeader   lipgloss.Color
	BgOverlay  lipgloss.Color

	// Pre-built styles.
	Header     lipgloss.Style
	StatusBar  lipgloss.Style
	UserLabel  lipgloss.Style
	BotLabel   lipgloss.Style
	ToolLabel  lipgloss.Style
	ThinkLabel lipgloss.Style
	Dim        lipgloss.Style
	Bold       lipgloss.Style
	ErrStyle   lipgloss.Style
}

// ThemeMode indicates dark or light terminal background.
type ThemeMode int

const (
	ThemeDark ThemeMode = iota
	ThemeLight
)

// DetectTheme returns the best theme for the current terminal.
// Checks SAGECLAW_THEME override, then COLORFGBG heuristic.
func DetectTheme() ThemeMode {
	if v := os.Getenv("SAGECLAW_THEME"); v != "" {
		switch strings.ToLower(v) {
		case "light":
			return ThemeLight
		case "dark":
			return ThemeDark
		}
	}

	// COLORFGBG is "fg;bg" — dark terminals have low bg values.
	if v := os.Getenv("COLORFGBG"); v != "" {
		parts := strings.Split(v, ";")
		if len(parts) >= 2 {
			bg, err := strconv.Atoi(parts[len(parts)-1])
			if err == nil && bg > 6 {
				return ThemeLight
			}
		}
	}

	return ThemeDark
}

// NewTheme creates a Theme for the given mode.
func NewTheme(mode ThemeMode) Theme {
	if mode == ThemeLight {
		return newLightTheme()
	}
	return newDarkTheme()
}

func newDarkTheme() Theme {
	t := Theme{
		Text:       lipgloss.Color("252"), // Warm white.
		TextDim:    lipgloss.Color("241"),
		TextBold:   lipgloss.Color("229"), // Warm gold.
		Accent:     lipgloss.Color("214"), // Golden.
		AccentDim:  lipgloss.Color("136"),
		Border:     lipgloss.Color("238"),
		UserBorder: lipgloss.Color("75"),  // Blue.
		BotBorder:  lipgloss.Color("214"), // Golden.
		ToolBorder: lipgloss.Color("241"),
		Success:    lipgloss.Color("78"),  // Green.
		Warning:    lipgloss.Color("214"), // Yellow.
		Error:      lipgloss.Color("196"), // Red.
		BgHeader:   lipgloss.Color("236"),
		BgOverlay:  lipgloss.Color("235"),
	}
	t.buildStyles()
	return t
}

func newLightTheme() Theme {
	t := Theme{
		Text:       lipgloss.Color("235"),
		TextDim:    lipgloss.Color("245"),
		TextBold:   lipgloss.Color("0"),
		Accent:     lipgloss.Color("130"), // Dark gold.
		AccentDim:  lipgloss.Color("137"),
		Border:     lipgloss.Color("250"),
		UserBorder: lipgloss.Color("33"),  // Blue.
		BotBorder:  lipgloss.Color("130"), // Gold.
		ToolBorder: lipgloss.Color("245"),
		Success:    lipgloss.Color("28"),  // Green.
		Warning:    lipgloss.Color("130"), // Yellow.
		Error:      lipgloss.Color("160"), // Red.
		BgHeader:   lipgloss.Color("254"),
		BgOverlay:  lipgloss.Color("255"),
	}
	t.buildStyles()
	return t
}

func (t *Theme) buildStyles() {
	t.Header = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.TextBold).
		Background(t.BgHeader).
		Padding(0, 1)

	t.StatusBar = lipgloss.NewStyle().
		Foreground(t.TextDim)

	t.UserLabel = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.UserBorder)

	t.BotLabel = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.BotBorder)

	t.ToolLabel = lipgloss.NewStyle().
		Foreground(t.ToolBorder)

	t.ThinkLabel = lipgloss.NewStyle().
		Italic(true).
		Foreground(t.TextDim)

	t.Dim = lipgloss.NewStyle().
		Foreground(t.TextDim)

	t.Bold = lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Text)

	t.ErrStyle = lipgloss.NewStyle().
		Foreground(t.Error)
}
