package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ChatState tracks the chat sub-state machine.
// IDLE → SENDING → STREAMING → COMPLETING → IDLE
//                    ↕              ↕
//              CONSENT_NEEDED  CONSENT_NEEDED
type ChatState int

const (
	ChatIdle ChatState = iota
	ChatSending
	ChatStreaming
	ChatCompleting
	ChatConsentNeeded
)

// String returns a human-readable state name.
func (s ChatState) String() string {
	switch s {
	case ChatIdle:
		return "idle"
	case ChatSending:
		return "sending"
	case ChatStreaming:
		return "streaming"
	case ChatCompleting:
		return "completing"
	case ChatConsentNeeded:
		return "consent"
	default:
		return "unknown"
	}
}

// ChatMessage represents a single message in the chat history.
type ChatMessage struct {
	Role       string     // "user", "assistant", "system", "tool"
	Content    string     // Text content (accumulated for streaming).
	ToolName   string     // For tool messages.
	ToolCallID string     // Tool call ID for matching results.
	ToolInput  string     // Tool input JSON.
	ToolStatus ToolStatus // Running/success/error for tool messages.
	Complete   bool       // Whether the message is finalized.
}

// ChatView is the main chat interface model.
type ChatView struct {
	viewport viewport.Model
	input    textarea.Model
	renderer *Renderer

	messages      []ChatMessage
	state         ChatState
	prevState     ChatState // State before consent interruption.
	inputHistory  []string
	historyIndex  int

	// Current turn metadata.
	currentModel    string
	currentProvider string
	currentTokens   int
	turnStart       time.Time
	connected       bool

	// Display preferences.
	showThinking bool

	width  int
	height int
}

// NewChatView creates a ChatView with the given dimensions and theme.
func NewChatView(width, height int, mode ThemeMode) ChatView {
	ta := textarea.New()
	ta.Placeholder = "Type a message or / for commands..."
	ta.Focus()
	ta.CharLimit = 4096
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	inputHeight := 5 // textarea + borders
	statusHeight := 1
	vpHeight := height - inputHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}

	vp := viewport.New(width, vpHeight)

	return ChatView{
		viewport:     vp,
		input:        ta,
		renderer:     NewRenderer(width, mode),
		state:        ChatIdle,
		connected:    true,
		showThinking: true,
		historyIndex: -1,
		width:        width,
		height:       height,
	}
}

// Resize updates the chat view dimensions.
func (c *ChatView) Resize(width, height int) {
	c.width = width
	c.height = height

	inputHeight := 5
	statusHeight := 1
	vpHeight := height - inputHeight - statusHeight
	if vpHeight < 1 {
		vpHeight = 1
	}

	c.viewport.Width = width
	c.viewport.Height = vpHeight
	c.input.SetWidth(width - 2)
	c.renderer.SetWidth(width)
	c.rebuildViewport()
}

// AddUserMessage appends a user message and saves to input history.
func (c *ChatView) AddUserMessage(text string) {
	c.messages = append(c.messages, ChatMessage{
		Role:     "user",
		Content:  text,
		Complete: true,
	})
	c.inputHistory = append(c.inputHistory, text)
	c.historyIndex = -1
	c.rebuildViewport()
}

// StartAssistantMessage begins a new assistant message (streaming).
func (c *ChatView) StartAssistantMessage(model, provider string) {
	c.messages = append(c.messages, ChatMessage{
		Role:     "assistant",
		Content:  "",
		Complete: false,
	})
	c.currentModel = model
	c.currentProvider = provider
	c.currentTokens = 0
	c.turnStart = time.Now()
	c.state = ChatStreaming
	c.rebuildViewport()
}

// AppendChunk adds streaming text to the current assistant message.
// If the last message is not an incomplete assistant message (e.g., after
// tool calls), creates a new one to receive the chunks.
func (c *ChatView) AppendChunk(text string) {
	if len(c.messages) == 0 {
		c.messages = append(c.messages, ChatMessage{
			Role: "assistant", Complete: false,
		})
	}
	last := &c.messages[len(c.messages)-1]
	if last.Role != "assistant" || last.Complete {
		// New assistant message after tool calls or completed message.
		c.messages = append(c.messages, ChatMessage{
			Role: "assistant", Complete: false,
		})
		last = &c.messages[len(c.messages)-1]
	}
	last.Content += text
	c.rebuildViewport()
}

// CompleteAssistantMessage finalizes the current assistant message.
func (c *ChatView) CompleteAssistantMessage() {
	if len(c.messages) == 0 {
		return
	}
	last := &c.messages[len(c.messages)-1]
	if last.Role == "assistant" && !last.Complete {
		last.Complete = true
	}
	c.state = ChatIdle
	c.rebuildViewport()
}

// AddToolCall adds a tool execution entry.
func (c *ChatView) AddToolCall(name, input string) {
	c.messages = append(c.messages, ChatMessage{
		Role:       "tool",
		ToolName:   name,
		ToolInput:  input,
		ToolStatus: ToolRunning,
		Complete:   false,
	})
	c.rebuildViewport()
}

// SetLastToolCallID sets the tool_call_id on the most recent tool message.
func (c *ChatView) SetLastToolCallID(id string) {
	for i := len(c.messages) - 1; i >= 0; i-- {
		if c.messages[i].Role == "tool" && c.messages[i].ToolCallID == "" {
			c.messages[i].ToolCallID = id
			break
		}
	}
}

// UpdateToolResult updates a tool call by tool_call_id with its result.
func (c *ChatView) UpdateToolResult(toolCallID, result string, isError bool) {
	for i := len(c.messages) - 1; i >= 0; i-- {
		m := &c.messages[i]
		if m.Role == "tool" && m.ToolCallID == toolCallID && !m.Complete {
			m.Content = result
			m.Complete = true
			if isError {
				m.ToolStatus = ToolError
			} else {
				m.ToolStatus = ToolSuccess
			}
			break
		}
	}
	c.rebuildViewport()
}

// AddAssistantMessage appends a completed assistant message (for history replay).
func (c *ChatView) AddAssistantMessage(content string) {
	c.messages = append(c.messages, ChatMessage{
		Role:     "assistant",
		Content:  content,
		Complete: true,
	})
	c.rebuildViewport()
}

// AddThinking adds a thinking/reasoning block.
func (c *ChatView) AddThinking(text string) {
	c.messages = append(c.messages, ChatMessage{
		Role:     "thinking",
		Content:  text,
		Complete: true,
	})
	c.rebuildViewport()
}

// SetConnected updates the connection status indicator.
func (c *ChatView) SetConnected(connected bool) {
	c.connected = connected
}

// TransitionTo moves the chat state machine.
func (c *ChatView) TransitionTo(state ChatState) {
	if state == ChatConsentNeeded {
		c.prevState = c.state
	}
	c.state = state
}

// ResumeFromConsent returns to the pre-consent state.
func (c *ChatView) ResumeFromConsent() {
	c.state = c.prevState
}

// State returns the current chat state.
func (c *ChatView) State() ChatState { return c.state }

// InputValue returns the current input text.
func (c *ChatView) InputValue() string { return c.input.Value() }

// ResetInput clears the input field.
func (c *ChatView) ResetInput() { c.input.Reset() }

// Update processes bubbletea messages for the chat view's sub-components.
func (c *ChatView) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "ctrl+p":
			// Scroll history when input is empty.
			if strings.TrimSpace(c.input.Value()) == "" && len(c.inputHistory) > 0 {
				if c.historyIndex == -1 {
					c.historyIndex = len(c.inputHistory) - 1
				} else if c.historyIndex > 0 {
					c.historyIndex--
				}
				c.input.SetValue(c.inputHistory[c.historyIndex])
				return nil
			}
		case "down":
			if c.historyIndex >= 0 {
				c.historyIndex++
				if c.historyIndex >= len(c.inputHistory) {
					c.historyIndex = -1
					c.input.Reset()
				} else {
					c.input.SetValue(c.inputHistory[c.historyIndex])
				}
				return nil
			}
		case "pgup":
			c.viewport.HalfViewUp()
			return nil
		case "pgdown":
			c.viewport.HalfViewDown()
			return nil
		}
	}

	// Update textarea.
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// View renders the chat view with input fixed at the bottom.
func (c *ChatView) View() string {
	var b strings.Builder

	// Viewport (scrollable messages) — fills available space.
	if len(c.messages) == 0 {
		// Empty state: show banner centered in viewport.
		banner := RenderBanner(c.width)
		welcome := RenderWelcome("Agent", *c.renderer.Theme(), c.width)
		emptyContent := banner + "\n\n" + welcome

		// Pad to fill viewport height.
		contentLines := strings.Count(emptyContent, "\n") + 1
		vpHeight := c.viewport.Height
		if contentLines < vpHeight {
			topPad := (vpHeight - contentLines) / 3 // Slight top bias.
			emptyContent = strings.Repeat("\n", topPad) + emptyContent
		}
		c.viewport.SetContent(emptyContent)
	}
	b.WriteString(c.viewport.View())
	b.WriteString("\n")

	// Status bar.
	elapsed := ""
	if !c.turnStart.IsZero() && c.state != ChatIdle {
		elapsed = fmt.Sprintf("%.1fs", time.Since(c.turnStart).Seconds())
	}
	statusBar := c.renderer.RenderStatusBar(
		c.currentModel, c.currentProvider,
		c.currentTokens, elapsed, c.connected, c.width)
	b.WriteString(statusBar)
	b.WriteString("\n")

	// Input — always at the bottom.
	inputStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(lipgloss.Color("#164699")).
		Width(c.width - 2)
	b.WriteString(inputStyle.Render(c.input.View()))

	return b.String()
}

// rebuildViewport re-renders all messages into the viewport.
func (c *ChatView) rebuildViewport() {
	var parts []string

	for _, msg := range c.messages {
		switch msg.Role {
		case "user":
			parts = append(parts, c.renderer.RenderUserMessage(msg.Content, c.width))

		case "assistant":
			if msg.Complete {
				parts = append(parts, c.renderer.RenderAssistantMessage(msg.Content, "Agent", c.width))
			} else {
				parts = append(parts, c.renderer.RenderStreamingMessage(msg.Content, "Agent", c.width))
			}

		case "tool":
			parts = append(parts, c.renderer.RenderToolCall(
				msg.ToolName, msg.ToolInput, msg.ToolStatus, msg.Content, c.width))

		case "thinking":
			if c.showThinking {
				parts = append(parts, c.renderer.RenderThinking(msg.Content, c.width))
			}
		}
	}

	content := strings.Join(parts, "\n\n")
	c.viewport.SetContent(content)
	c.viewport.GotoBottom()
}
