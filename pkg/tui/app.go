package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AppState tracks the TUI lifecycle.
type AppState int

const (
	StateConnecting AppState = iota
	StateChatting
	StateError
)

// AppModel is the root bubbletea model for the SageClaw TUI.
type AppModel struct {
	client  *TUIClient
	state   AppState
	agentID string
	chatID  string

	chat      ChatView
	theme     Theme
	themeMode ThemeMode
	width     int
	height    int

	// Overlay state.
	overlay        OverlayType
	consent        ConsentModal
	agentPicker    AgentPicker
	sessionPicker  SessionPicker
	modelPicker    ModelPicker
	helpScreen     HelpScreen
	statusScreen   StatusScreen
	settingsScreen SettingsScreen
	preferredModel string // Local-only model preference for next session.
	attachments    AttachmentBar
	keyHandler     KeyHandler

	sseStream  *sseStream
	sseAttempt int
	sessionSeq int // Incremented on /new to ignore stale SSE events.
	errMsg     string
}

// NewApp creates the root TUI model.
func NewApp(client *TUIClient, agentID string) AppModel {
	mode := DetectTheme()
	theme := NewTheme(mode)

	m := AppModel{
		client:      client,
		agentID:     agentID,
		chatID:      fmt.Sprintf("tui-%d", time.Now().UnixMilli()),
		state:       StateConnecting,
		chat:        NewChatView(80, 24, mode),
		theme:       theme,
		themeMode:   mode,
		width:       80,
		height:      24,
		overlay:     OverlayNone,
		attachments: NewAttachmentBar(theme),
		keyHandler:  NewKeyHandler(),
	}
	m.sseStream = newSSEStream(client, "", "")
	return m
}

// agentsLoadedMsg carries the agent list for startup flow.
type agentsLoadedMsg struct {
	Agents []AgentInfo
	Err    error
}

// sessionsLoadedMsg carries the session list for startup flow.
type sessionsLoadedMsg struct {
	Sessions []SessionInfo
	Err      error
}

// messagesLoadedMsg carries loaded history for a resumed session.
type messagesLoadedMsg struct {
	Messages []MessageInfo
	Err      error
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		SSESubscribe(m.sseStream),
		m.loadAgents(),
	)
}

// loadAgents fetches agents from the server.
func (m AppModel) loadAgents() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		agents, err := client.ListAgents()
		return agentsLoadedMsg{Agents: agents, Err: err}
	}
}

// loadSessions fetches sessions for the current agent.
func (m AppModel) loadSessions() tea.Cmd {
	client := m.client
	agentID := m.agentID
	return func() tea.Msg {
		sessions, err := client.ListSessions(agentID)
		return sessionsLoadedMsg{Sessions: sessions, Err: err}
	}
}

// loadMessages fetches history for a session.
func (m AppModel) loadMessages(sessionID string) tea.Cmd {
	client := m.client
	return func() tea.Msg {
		msgs, err := client.LoadMessages(sessionID, 50)
		return messagesLoadedMsg{Messages: msgs, Err: err}
	}
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Handle selection messages from pickers/consent.
	switch msg := msg.(type) {
	case ConsentResponseMsg:
		return m.handleConsentResponse(msg)
	case AgentSelectedMsg:
		return m.handleAgentSelected(msg)
	case SessionSelectedMsg:
		return m.handleSessionSelected(msg)
	case ModelSelectedMsg:
		return m.handleModelSelected(msg)
	case PickerCancelledMsg:
		return m.handlePickerCancelled(msg)
	}

	// Route key events: global shortcuts, then overlay, then chat.
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if cmd := m.keyHandler.HandleGlobal(keyMsg, &m); cmd != nil {
			return m, cmd
		}
		if m.overlay != OverlayNone {
			return m.updateOverlay(msg)
		}
		return m.updateChat(msg)
	}

	// Non-key messages.
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h := msg.Height - 1 // minus header
		m.chat.Resize(msg.Width, h)
		// Only resize active overlays — zero-valued pickers panic on SetSize.
		switch m.overlay {
		case OverlayConsent:
			m.consent.Resize(msg.Width, h)
		case OverlayAgentPicker:
			m.agentPicker.Resize(msg.Width, h)
		case OverlaySessionPicker:
			m.sessionPicker.Resize(msg.Width, h)
		case OverlayModelPicker:
			m.modelPicker.Resize(msg.Width, h)
		}

	case agentsLoadedMsg:
		if msg.Err != nil {
			m.state = StateChatting // Proceed without picker.
			break
		}
		if len(msg.Agents) > 1 {
			m.agentPicker = NewAgentPicker(msg.Agents, m.width, m.height-1)
			m.overlay = OverlayAgentPicker
			m.state = StateChatting
		} else if len(msg.Agents) == 1 {
			m.agentID = msg.Agents[0].ID
			m.state = StateChatting
			cmds = append(cmds, m.loadSessions())
		} else {
			m.state = StateChatting
		}

	case sessionsLoadedMsg:
		if msg.Err != nil {
			break
		}
		if len(msg.Sessions) > 0 {
			m.sessionPicker = NewSessionPicker(msg.Sessions, m.width, m.height-1)
			m.overlay = OverlaySessionPicker
		}
		// No sessions — stay in chat (new session by default).

	case modelsLoadedMsg:
		if len(msg.Models) > 0 {
			m.modelPicker = NewModelPicker(msg.Models, msg.Width, msg.Height)
			m.overlay = OverlayModelPicker
		}

	case messagesLoadedMsg:
		if msg.Err == nil {
			for _, mi := range msg.Messages {
				switch mi.Role {
				case "user":
					m.chat.AddUserMessage(mi.Content)
				case "assistant":
					m.chat.AddAssistantMessage(mi.Content)
				}
			}
		}

	case SSEEventMsg:
		wasDisconnected := !m.chat.connected
		m.state = StateChatting
		m.chat.SetConnected(true)
		cmd := m.handleSSEEvent(msg.Event)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		// Re-subscribe to read the next event from the stream.
		cmds = append(cmds, SSESubscribe(m.sseStream))
		if wasDisconnected {
			cmds = append(cmds, pollPendingConsent(m.client))
		}

	case SSESyncMsg:
		m.chat.SetConnected(true)
		cmds = append(cmds, SSESubscribe(m.sseStream))

	case SSEErrorMsg:
		m.chat.SetConnected(false)
		m.state = StateError
		m.errMsg = msg.Err.Error()

	case pendingConsentMsg:
		if m.overlay != OverlayConsent && msg.Request.Nonce != "" {
			m.showConsentModal(msg.Request)
		}

	case statusLoadedMsg:
		m.statusScreen.SetHealth(msg.Health, msg.Err)

	case settingsChangedMsg:
		m.overlay = OverlayNone
		m.chat.showThinking = msg.ShowThinking
		m.chat.input.Focus()

	case uploadResultMsg:
		if msg.Err != nil {
			m.chat.AddThinking(fmt.Sprintf("⚠ Upload failed: %s", msg.Err))
		} else {
			m.attachments.MarkUploaded(msg.Index, msg.URL)
		}

	default:
		cmd := m.chat.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// --- Selection handlers ---

func (m AppModel) handleAgentSelected(msg AgentSelectedMsg) (tea.Model, tea.Cmd) {
	m.agentID = msg.Agent.ID
	m.overlay = OverlayNone
	m.state = StateChatting
	m.chat.input.Focus()
	return m, m.loadSessions()
}

func (m AppModel) handleSessionSelected(msg SessionSelectedMsg) (tea.Model, tea.Cmd) {
	m.overlay = OverlayNone
	m.state = StateChatting
	m.chat.input.Focus()
	if msg.IsNew {
		m.chatID = fmt.Sprintf("tui-%d", time.Now().UnixMilli())
		return m, nil
	}
	// Resume existing session.
	m.chatID = msg.Session.ID
	return m, m.loadMessages(msg.Session.ID)
}

func (m AppModel) handleModelSelected(msg ModelSelectedMsg) (tea.Model, tea.Cmd) {
	m.overlay = OverlayNone
	m.preferredModel = msg.Model.ID
	m.chat.input.Focus()
	return m, nil
}

func (m AppModel) handlePickerCancelled(msg PickerCancelledMsg) (tea.Model, tea.Cmd) {
	m.overlay = OverlayNone
	m.state = StateChatting
	m.chat.input.Focus()
	return m, nil
}

// ShowAgentPicker opens the agent picker (for /agent command).
func (m *AppModel) ShowAgentPicker() tea.Cmd {
	client := m.client
	return func() tea.Msg {
		agents, err := client.ListAgents()
		return agentsLoadedMsg{Agents: agents, Err: err}
	}
}

// ShowSessionPicker opens the session picker (for /session command).
func (m *AppModel) ShowSessionPicker() tea.Cmd {
	return m.loadSessions()
}

// ShowModelPicker opens the model picker (for /model command).
func (m *AppModel) ShowModelPicker() tea.Cmd {
	client := m.client
	width, height := m.width, m.height-1
	return func() tea.Msg {
		models, err := client.ListModels()
		if err != nil {
			return SSEErrorMsg{Err: fmt.Errorf("loading models: %w", err)}
		}
		return modelsLoadedMsg{Models: models, Width: width, Height: height}
	}
}

// modelsLoadedMsg carries the model list.
type modelsLoadedMsg struct {
	Models []ModelInfo
	Width  int
	Height int
}

// updateChat handles key events when no overlay is active.
func (m AppModel) updateChat(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	keyMsg := msg.(tea.KeyMsg)

	switch keyMsg.String() {
	case "alt+enter":
		cmd := m.chat.Update(msg)
		cmds = append(cmds, cmd)
	case "enter":
		text := strings.TrimSpace(m.chat.InputValue())
		if text == "" {
			break
		}
		m.chat.ResetInput()

		// Check for slash command.
		if name, args, ok := ParseCommand(text); ok {
			cmd := FindCommand(name)
			if cmd == nil {
				m.chat.AddThinking(fmt.Sprintf("Unknown command: /%s — type /help for list", name))
				break
			}
			result := cmd.Handler(&m, args)
			if result.Message != "" {
				m.chat.AddThinking(result.Message)
			}
			if result.Quit {
				return m, tea.Quit
			}
			if result.TeaCmd != nil {
				cmds = append(cmds, result.TeaCmd)
			}
			break
		}

		m.chat.AddUserMessage(text)
		m.chat.TransitionTo(ChatSending)

		// Upload any pending attachments first.
		if m.attachments.Count() > 0 {
			uploadCmds := m.attachments.UploadAll(m.client, m.chatID)
			cmds = append(cmds, uploadCmds...)
			m.attachments.Clear()
		}

		client := m.client
		agentID := m.agentID
		chatID := m.chatID
		cmds = append(cmds, func() tea.Msg {
			if err := client.SendMessage(agentID, chatID, text); err != nil {
				return SSEErrorMsg{Err: fmt.Errorf("send: %w", err)}
			}
			return nil
		})
	default:
		cmd := m.chat.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// updateOverlay routes key events to the active overlay.
func (m AppModel) updateOverlay(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case OverlayConsent:
		updated, cmd := m.consent.Update(msg)
		m.consent = updated
		return m, cmd
	case OverlayAgentPicker:
		updated, cmd := m.agentPicker.Update(msg)
		m.agentPicker = updated
		return m, cmd
	case OverlaySessionPicker:
		updated, cmd := m.sessionPicker.Update(msg)
		m.sessionPicker = updated
		return m, cmd
	case OverlayModelPicker:
		updated, cmd := m.modelPicker.Update(msg)
		m.modelPicker = updated
		return m, cmd
	case OverlayHelp:
		updated, cmd := m.helpScreen.Update(msg)
		m.helpScreen = updated
		return m, cmd
	case OverlayStatus:
		updated, cmd := m.statusScreen.Update(msg)
		m.statusScreen = updated
		return m, cmd
	case OverlaySettings:
		updated, cmd := m.settingsScreen.Update(msg)
		m.settingsScreen = updated
		return m, cmd
	}
	return m, nil
}

// handleConsentResponse processes the user's consent decision.
func (m AppModel) handleConsentResponse(resp ConsentResponseMsg) (tea.Model, tea.Cmd) {
	m.overlay = OverlayNone
	m.chat.ResumeFromConsent()
	m.chat.input.Focus() // Restore textarea focus.

	client := m.client
	return m, func() tea.Msg {
		if err := client.RespondConsent(resp.Nonce, resp.Granted, resp.Tier); err != nil {
			return SSEErrorMsg{Err: fmt.Errorf("consent response failed: %w", err)}
		}
		return nil
	}
}

// showConsentModal activates the consent overlay.
func (m *AppModel) showConsentModal(req ConsentRequest) {
	m.overlay = OverlayConsent
	m.chat.TransitionTo(ChatConsentNeeded)
	m.chat.input.Blur() // Release textarea focus so keys reach the overlay.
	m.consent = NewConsentModal(req, m.theme, m.width, m.height-1)
}

// handleSSEEvent processes a typed SSE event and updates chat state.
func (m *AppModel) handleSSEEvent(ev SSEEvent) tea.Cmd {
	switch ev.Type {
	case "chunk":
		if m.chat.State() == ChatSending {
			m.chat.StartAssistantMessage(ev.Model, ev.Provider)
		}
		m.chat.AppendChunk(ev.Text)

	case "run.started":
		m.chat.TransitionTo(ChatStreaming)
		m.chat.StartAssistantMessage(ev.Model, ev.Provider)

	case "run.completed":
		m.chat.CompleteAssistantMessage()

	case "run.failed":
		m.chat.CompleteAssistantMessage()
		if ev.Text != "" {
			m.chat.AddThinking("⚠ Error: " + ev.Text)
		}

	case "tool.call":
		id, name, input := parseToolCall(ev.ToolCall)
		m.chat.AddToolCall(name, input)
		m.chat.SetLastToolCallID(id)

	case "tool.call.started":
		// Already shown via tool.call.

	case "tool.result":
		toolCallID, content, isError := parseToolResult(ev.ToolResult)
		m.chat.UpdateToolResult(toolCallID, content, isError)

	case "consent.needed":
		req := parseConsentRequest(ev.Consent)
		if m.overlay == OverlayNone {
			m.showConsentModal(req)
		}

	case "consent.result":
		if m.overlay == OverlayConsent {
			m.overlay = OverlayNone
		}
		m.chat.ResumeFromConsent()

	case "consent.escalated":
		if m.overlay == OverlayConsent {
			m.overlay = OverlayNone
		}
		m.chat.ResumeFromConsent()
		m.chat.AddThinking("⚠ Session blocked — tool denied too many times")
	}

	return nil
}

// --- JSON parsers ---

func parseToolCall(raw json.RawMessage) (id, name, input string) {
	var tc struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(raw, &tc); err != nil {
		return "", "unknown", ""
	}
	return tc.ID, tc.Name, string(tc.Input)
}

func parseToolResult(raw json.RawMessage) (toolCallID, content string, isError bool) {
	var tr struct {
		ToolCallID string `json:"tool_call_id"`
		Content    string `json:"content"`
		IsError    bool   `json:"is_error"`
	}
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", "", false
	}
	return tr.ToolCallID, tr.Content, tr.IsError
}

func parseConsentRequest(raw json.RawMessage) ConsentRequest {
	var cr struct {
		Nonce       string `json:"nonce"`
		ToolName    string `json:"tool_name"`
		RiskLevel   string `json:"risk_level"`
		Explanation string `json:"explanation"`
		ToolInput   string `json:"tool_input"`
	}
	if err := json.Unmarshal(raw, &cr); err != nil {
		return ConsentRequest{ToolName: "unknown"}
	}
	return ConsentRequest{
		Nonce:       cr.Nonce,
		ToolName:    cr.ToolName,
		RiskLevel:   cr.RiskLevel,
		Explanation: cr.Explanation,
		ToolInput:   cr.ToolInput,
	}
}

// --- Consent polling ---

type pendingConsentMsg struct{ Request ConsentRequest }

func pollPendingConsent(client *TUIClient) tea.Cmd {
	return func() tea.Msg {
		var pending []struct {
			Nonce       string `json:"nonce"`
			ToolName    string `json:"tool_name"`
			RiskLevel   string `json:"risk_level"`
			Explanation string `json:"explanation"`
			ToolInput   string `json:"tool_input"`
		}
		if err := client.getJSON("/api/consent/pending", &pending); err != nil {
			return nil
		}
		if len(pending) > 0 {
			p := pending[0]
			return pendingConsentMsg{
				Request: ConsentRequest{
					Nonce:       p.Nonce,
					ToolName:    p.ToolName,
					RiskLevel:   p.RiskLevel,
					Explanation: p.Explanation,
					ToolInput:   p.ToolInput,
				},
			}
		}
		return nil
	}
}

// --- View ---

func (m AppModel) View() string {
	var b strings.Builder

	// Header bar — SageClaw deep blue.
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#72c1da")).
		Background(lipgloss.Color("#092163")).
		Padding(0, 1)
	header := headerStyle.Render(fmt.Sprintf("⚡ SageClaw — %s", m.agentID))
	headerWidth := lipgloss.Width(header)
	if headerWidth < m.width {
		header += headerStyle.Render(strings.Repeat(" ", m.width-headerWidth))
	}
	b.WriteString(header)
	b.WriteString("\n")

	switch m.state {
	case StateConnecting:
		b.WriteString(m.theme.Dim.Render("  Connecting to server..."))
		b.WriteString("\n")
	case StateError:
		b.WriteString(m.theme.ErrStyle.Render(fmt.Sprintf("  Error: %s", m.errMsg)))
		b.WriteString("\n")
	default:
		switch m.overlay {
		case OverlayConsent:
			b.WriteString(m.consent.View())
		case OverlayAgentPicker:
			b.WriteString(m.agentPicker.View())
		case OverlaySessionPicker:
			b.WriteString(m.sessionPicker.View())
		case OverlayModelPicker:
			b.WriteString(m.modelPicker.View())
		case OverlayHelp:
			b.WriteString(m.helpScreen.View())
		case OverlayStatus:
			b.WriteString(m.statusScreen.View())
		case OverlaySettings:
			b.WriteString(m.settingsScreen.View())
		default:
			b.WriteString(m.chat.View())
			// Attachment bar above input.
			if m.attachments.Count() > 0 {
				b.WriteString(m.attachments.View(m.width))
			}
		}
	}

	return b.String()
}
