package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConsentRequest holds the data for a pending consent prompt.
type ConsentRequest struct {
	Nonce       string
	ToolName    string
	RiskLevel   string
	Explanation string
	ToolInput   string
}

// ConsentModal is the bubbletea model for tool consent prompts.
type ConsentModal struct {
	request ConsentRequest
	theme   Theme
	width   int
	height  int
}

// ConsentResponseMsg is sent when the user responds to a consent prompt.
type ConsentResponseMsg struct {
	Nonce   string
	Granted bool
	Tier    string // "once", "always", "deny"
}

// NewConsentModal creates a consent modal for the given request.
func NewConsentModal(req ConsentRequest, theme Theme, width, height int) ConsentModal {
	return ConsentModal{
		request: req,
		theme:   theme,
		width:   width,
		height:  height,
	}
}

// Resize updates modal dimensions.
func (c *ConsentModal) Resize(width, height int) {
	c.width = width
	c.height = height
}

// Update handles key events for the consent modal.
func (c ConsentModal) Update(msg tea.Msg) (ConsentModal, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		key := strings.ToLower(msg.String())
		switch key {
		case "a":
			return c, func() tea.Msg {
				return ConsentResponseMsg{
					Nonce:   c.request.Nonce,
					Granted: true,
					Tier:    "once",
				}
			}
		case "s":
			return c, func() tea.Msg {
				return ConsentResponseMsg{
					Nonce:   c.request.Nonce,
					Granted: true,
					Tier:    "always",
				}
			}
		case "d", "esc":
			return c, func() tea.Msg {
				return ConsentResponseMsg{
					Nonce:   c.request.Nonce,
					Granted: false,
					Tier:    "deny",
				}
			}
		case "enter":
			// Enter defaults to allow once (most common action).
			return c, func() tea.Msg {
				return ConsentResponseMsg{
					Nonce:   c.request.Nonce,
					Granted: true,
					Tier:    "once",
				}
			}
		}
	}
	return c, nil
}

// View renders the consent modal as a centered overlay.
func (c ConsentModal) View() string {
	modalWidth := c.width - 10
	if modalWidth > 60 {
		modalWidth = 60
	}
	if modalWidth < 30 {
		modalWidth = 30
	}

	// Risk color.
	riskStyle := c.theme.Dim
	switch c.request.RiskLevel {
	case "sensitive":
		riskStyle = lipgloss.NewStyle().Foreground(c.theme.Error).Bold(true)
	case "moderate":
		riskStyle = lipgloss.NewStyle().Foreground(c.theme.Warning)
	}

	// Build content.
	var content strings.Builder
	content.WriteString("\n")
	content.WriteString(fmt.Sprintf("  %s wants to run:\n\n", c.request.ToolName))
	content.WriteString(fmt.Sprintf("  Tool: %s\n", c.request.ToolName))
	content.WriteString(fmt.Sprintf("  Risk: %s\n", riskStyle.Render(c.request.RiskLevel)))

	if c.request.Explanation != "" {
		content.WriteString(fmt.Sprintf("  Why:  %s\n", c.request.Explanation))
	}

	if c.request.ToolInput != "" {
		inputRunes := []rune(c.request.ToolInput)
		maxLen := modalWidth - 10
		if len(inputRunes) > maxLen {
			inputRunes = append(inputRunes[:maxLen-3], '.', '.', '.')
		}
		content.WriteString(fmt.Sprintf("  Input: %s\n", c.theme.Dim.Render(string(inputRunes))))
	}

	content.WriteString("\n")

	// Action bar.
	allowOnce := lipgloss.NewStyle().Foreground(c.theme.Success).Bold(true).Render("[A] Allow once")
	alwaysAllow := lipgloss.NewStyle().Foreground(c.theme.Accent).Bold(true).Render("[S] Always allow")
	deny := lipgloss.NewStyle().Foreground(c.theme.Error).Bold(true).Render("[D] Deny")
	content.WriteString(fmt.Sprintf("  %s  %s  %s\n", allowOnce, alwaysAllow, deny))
	content.WriteString("\n")

	// Box it.
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(c.theme.Warning).
		Render("🔒 Tool Consent")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(c.theme.Warning).
		Width(modalWidth).
		Padding(0, 1)

	modal := fmt.Sprintf("%s\n%s", title, box.Render(content.String()))

	// Center in terminal.
	return lipgloss.Place(c.width, c.height,
		lipgloss.Center, lipgloss.Center,
		modal)
}
