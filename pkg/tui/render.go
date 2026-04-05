package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/glamour"
	glamourstyles "github.com/charmbracelet/glamour/styles"
	"github.com/charmbracelet/lipgloss"
)

// Renderer formats chat messages for terminal display.
type Renderer struct {
	theme    Theme
	width    int
	glamour  *glamour.TermRenderer
	themeMode ThemeMode
}

// NewRenderer creates a Renderer for the given width and theme.
func NewRenderer(width int, mode ThemeMode) *Renderer {
	r := &Renderer{
		theme:     NewTheme(mode),
		width:     width,
		themeMode: mode,
	}
	r.rebuildGlamour()
	return r
}

// Theme returns the renderer's theme.
func (r *Renderer) Theme() *Theme { return &r.theme }

// SetWidth updates the renderer width (on terminal resize).
func (r *Renderer) SetWidth(w int) {
	if w == r.width {
		return
	}
	r.width = w
	r.rebuildGlamour()
}

func (r *Renderer) rebuildGlamour() {
	styleName := glamourstyles.DarkStyle
	if r.themeMode == ThemeLight {
		styleName = glamourstyles.LightStyle
	}
	// Content width minus box borders (2 chars each side).
	contentWidth := r.width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}
	gr, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(styleName),
		glamour.WithWordWrap(contentWidth),
		glamour.WithEmoji(),
	)
	if err != nil {
		// Fallback — no markdown rendering.
		r.glamour = nil
		return
	}
	r.glamour = gr
}

// RenderUserMessage formats a user message with a bordered box.
func (r *Renderer) RenderUserMessage(text string, width int) string {
	label := r.theme.UserLabel.Render("You")
	boxWidth := width - 2
	if boxWidth < 20 {
		boxWidth = 20
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(r.theme.UserBorder).
		Width(boxWidth).
		Padding(0, 1)

	return fmt.Sprintf("%s\n%s", label, box.Render(text))
}

// RenderAssistantMessage formats a completed assistant message with markdown.
func (r *Renderer) RenderAssistantMessage(text string, agentName string, width int) string {
	label := r.theme.BotLabel.Render(agentName)

	rendered := text
	if r.glamour != nil {
		if out, err := r.glamour.Render(text); err == nil {
			rendered = strings.TrimSpace(out)
		}
	}

	boxWidth := width - 2
	if boxWidth < 20 {
		boxWidth = 20
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(r.theme.BotBorder).
		Width(boxWidth).
		Padding(0, 1)

	return fmt.Sprintf("%s\n%s", label, box.Render(rendered))
}

// RenderStreamingMessage formats an in-progress streaming message (no markdown).
func (r *Renderer) RenderStreamingMessage(text string, agentName string, width int) string {
	label := r.theme.BotLabel.Render(agentName + " ...")

	boxWidth := width - 2
	if boxWidth < 20 {
		boxWidth = 20
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(r.theme.BotBorder).
		BorderStyle(lipgloss.Border{
			Top:         "─",
			Bottom:      "╌",
			Left:        "│",
			Right:       "│",
			TopLeft:     "╭",
			TopRight:    "╮",
			BottomLeft:  "╰",
			BottomRight: "╯",
		}).
		Width(boxWidth).
		Padding(0, 1)

	return fmt.Sprintf("%s\n%s", label, box.Render(text))
}

// ToolStatus represents the execution state of a tool call.
type ToolStatus int

const (
	ToolRunning ToolStatus = iota
	ToolSuccess
	ToolError
)

// RenderToolCall formats a compact tool execution line (Hermes-style).
func (r *Renderer) RenderToolCall(name string, input string, status ToolStatus, result string, width int) string {
	// Status icon with color.
	var statusPart string
	switch status {
	case ToolRunning:
		statusPart = lipgloss.NewStyle().Foreground(r.theme.Warning).Render("⟳")
	case ToolSuccess:
		statusPart = lipgloss.NewStyle().Foreground(r.theme.Success).Render("✓")
	case ToolError:
		statusPart = lipgloss.NewStyle().Foreground(r.theme.Error).Render("✗")
	}

	// Tool name.
	namePart := lipgloss.NewStyle().Foreground(r.theme.Accent).Bold(true).Render(name)

	// Compact input preview (single line).
	inputPreview := compactInput(input, width-lipgloss.Width(namePart)-10)
	inputPart := r.theme.Dim.Render(inputPreview)

	line := fmt.Sprintf("  ⚙ %s %s  %s", namePart, statusPart, inputPart)

	// For errors, show result on next line.
	if status == ToolError && result != "" {
		errLine := "    " + lipgloss.NewStyle().Foreground(r.theme.Error).Render(truncateOneLine(result, width-6))
		line += "\n" + errLine
	}

	return line
}

// compactInput formats tool input as a short single-line preview.
func compactInput(input string, maxWidth int) string {
	if input == "" || maxWidth < 10 {
		return ""
	}
	// Remove outer braces and whitespace for compactness.
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	s = strings.TrimSpace(s)
	// Collapse whitespace.
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > maxWidth {
		s = s[:maxWidth-3] + "..."
	}
	return s
}

// truncateOneLine returns a single-line truncated string.
func truncateOneLine(s string, maxLen int) string {
	s = strings.SplitN(s, "\n", 2)[0]
	if len(s) > maxLen {
		s = s[:maxLen-3] + "..."
	}
	return s
}

// RenderThinking formats a thinking/reasoning block.
func (r *Renderer) RenderThinking(text string, width int) string {
	boxWidth := width - 2
	if boxWidth < 20 {
		boxWidth = 20
	}

	label := r.theme.ThinkLabel.Render("💭 Thinking")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(r.theme.TextDim).
		Width(boxWidth).
		Foreground(r.theme.TextDim).
		Padding(0, 1)

	return fmt.Sprintf("%s\n%s", label, box.Render(text))
}

// RenderStatusBar formats the status bar between viewport and input.
func (r *Renderer) RenderStatusBar(model, provider string, tokens int, elapsed string, connected bool, width int) string {
	connStatus := r.theme.Dim.Render("connected")
	if !connected {
		connStatus = lipgloss.NewStyle().Foreground(r.theme.Warning).Render("reconnecting")
	}

	parts := []string{}
	if model != "" {
		parts = append(parts, model)
	}
	if provider != "" {
		parts = append(parts, provider)
	}
	if tokens > 0 {
		parts = append(parts, formatTokens(tokens))
	}
	if elapsed != "" {
		parts = append(parts, elapsed)
	}

	info := r.theme.StatusBar.Render("⚡ " + strings.Join(parts, " · "))

	// Right-align connection status.
	infoLen := lipgloss.Width(info)
	connLen := lipgloss.Width(connStatus)
	gap := width - infoLen - connLen
	if gap < 1 {
		gap = 1
	}

	return info + strings.Repeat(" ", gap) + connStatus
}

// formatToolInput returns a compact representation of tool input JSON.
func formatToolInput(input string, maxWidth int) string {
	if input == "" {
		return ""
	}

	// Try to parse as JSON for pretty display.
	var obj map[string]any
	if err := json.Unmarshal([]byte(input), &obj); err == nil {
		parts := make([]string, 0, len(obj))
		for k, v := range obj {
			s := fmt.Sprintf("%s: %v", k, v)
			if len(s) > maxWidth {
				s = s[:maxWidth-3] + "..."
			}
			parts = append(parts, s)
		}
		return strings.Join(parts, "\n")
	}

	// Not JSON — truncate raw string.
	if len(input) > maxWidth*3 {
		return input[:maxWidth*3-3] + "..."
	}
	return input
}

// truncateLines limits output to maxLines, showing a collapse indicator.
func truncateLines(text string, maxLines int, maxWidth int) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return text
	}
	result := strings.Join(lines[:maxLines], "\n")
	remaining := len(lines) - maxLines
	return fmt.Sprintf("%s\n... (%d lines collapsed)", result, remaining)
}

// formatTokens formats token count in human-readable form.
func formatTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fK tokens", float64(n)/1000)
	}
	return fmt.Sprintf("%d tokens", n)
}
