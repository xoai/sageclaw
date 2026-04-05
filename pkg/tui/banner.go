package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SageClaw deep blue gradient colors (from sageclaw-logo.svg).
var bannerColors = []lipgloss.Color{
	"#092163", // Darkest navy
	"#113880",
	"#164699",
	"#1955ab",
	"#2471be",
	"#2b7dc2",
	"#358ac6",
	"#3ca0cd",
	"#59b0d5",
	"#72c1da", // Lightest cyan-blue
}

// ASCII art for "SAGECLAW" — block letter style.
var bannerLines = []string{
	`  ██████   █████   ██████  ███████  ██████ ██       █████  ██     ██`,
	`  ██      ██   ██ ██       ██      ██      ██      ██   ██ ██     ██`,
	`  ███████ ███████ ██   ███ █████   ██      ██      ███████ ██  █  ██`,
	`       ██ ██   ██ ██    ██ ██      ██      ██      ██   ██ ██ ███ ██`,
	`  ██████  ██   ██  ██████  ███████  ██████ ███████ ██   ██  ███ ███ `,
}

// RenderBanner produces the colored ASCII art banner.
func RenderBanner(width int) string {
	var lines []string

	for i, line := range bannerLines {
		// Pick color from gradient based on line index.
		colorIdx := (i * (len(bannerColors) - 1)) / max(len(bannerLines)-1, 1)
		style := lipgloss.NewStyle().Foreground(bannerColors[colorIdx])

		// Center the line.
		rendered := style.Render(line)
		renderedWidth := lipgloss.Width(rendered)
		if renderedWidth < width {
			pad := (width - renderedWidth) / 2
			rendered = strings.Repeat(" ", pad) + rendered
		}
		lines = append(lines, rendered)
	}

	return strings.Join(lines, "\n")
}

// RenderWelcome produces the welcome message below the banner.
func RenderWelcome(agentName string, theme Theme, width int) string {
	var b strings.Builder

	subtitle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#3ca0cd")).
		Bold(true).
		Render("AI Agent Terminal")

	subtitleWidth := lipgloss.Width(subtitle)
	if subtitleWidth < width {
		pad := (width - subtitleWidth) / 2
		subtitle = strings.Repeat(" ", pad) + subtitle
	}
	b.WriteString(subtitle)
	b.WriteString("\n\n")

	tips := []string{
		"Type a message to start chatting",
		"Use /help to see available commands",
		"Use /agent to switch agents, /session to switch sessions",
	}

	for i, tip := range tips {
		num := lipgloss.NewStyle().Foreground(lipgloss.Color("#2471be")).Bold(true).
			Render(string(rune('1' + i)))
		text := theme.Dim.Render(". " + tip)
		line := "  " + num + text
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}
