package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Command defines a slash command.
type Command struct {
	Name        string
	Description string
	Handler     func(m *AppModel, args string) CommandResult
}

// CommandResult tells the app what to do after a command runs.
type CommandResult struct {
	TeaCmd  tea.Cmd // Bubbletea command to execute.
	Message string  // Status message to show in chat.
	Quit    bool    // True to exit the TUI.
}

// commandRegistry holds all registered slash commands.
var commandRegistry []Command

func init() {
	commandRegistry = []Command{
		{Name: "help", Description: "Show slash command reference", Handler: cmdHelp},
		{Name: "agent", Description: "Switch agent", Handler: cmdAgent},
		{Name: "session", Description: "Switch or resume session", Handler: cmdSession},
		{Name: "model", Description: "Change model preference", Handler: cmdModel},
		{Name: "new", Description: "Start a new session", Handler: cmdNew},
		{Name: "status", Description: "Show system health", Handler: cmdStatus},
		{Name: "settings", Description: "Toggle display preferences", Handler: cmdSettings},
		{Name: "think", Description: "Toggle thinking display", Handler: cmdThink},
		{Name: "voice", Description: "Toggle voice mode (coming soon)", Handler: cmdStub},
		{Name: "attach", Description: "Attach a file: /attach <path>", Handler: cmdAttach},
		{Name: "exit", Description: "Quit SageClaw TUI", Handler: cmdExit},
		{Name: "quit", Description: "Quit SageClaw TUI", Handler: cmdExit},
	}
}

// ParseCommand checks if text is a slash command. Returns the command,
// args, and true if matched; empty and false if not a command.
func ParseCommand(text string) (name, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	// Remove the slash prefix.
	text = text[1:]
	if text == "" {
		return "", "", false // Bare "/" is not a command.
	}
	parts := strings.SplitN(text, " ", 2)
	name = strings.ToLower(parts[0])
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return name, args, true
}

// FindCommand looks up a command by name. Returns nil if not found.
func FindCommand(name string) *Command {
	for i := range commandRegistry {
		if commandRegistry[i].Name == name {
			return &commandRegistry[i]
		}
	}
	return nil
}

// MatchCommands returns commands whose names start with the given prefix.
// Used for autocomplete.
func MatchCommands(prefix string) []Command {
	prefix = strings.ToLower(prefix)
	var matches []Command
	for _, cmd := range commandRegistry {
		if strings.HasPrefix(cmd.Name, prefix) {
			matches = append(matches, cmd)
		}
	}
	return matches
}

// AllCommands returns the full command list (for help screen).
func AllCommands() []Command {
	return commandRegistry
}

// --- Command handlers ---

func cmdHelp(m *AppModel, args string) CommandResult {
	m.helpScreen = NewHelpScreen(m.theme, m.width, m.height-1)
	m.overlay = OverlayHelp
	m.chat.input.Blur()
	return CommandResult{}
}

func cmdAgent(m *AppModel, args string) CommandResult {
	return CommandResult{TeaCmd: m.ShowAgentPicker()}
}

func cmdSession(m *AppModel, args string) CommandResult {
	return CommandResult{TeaCmd: m.ShowSessionPicker()}
}

func cmdModel(m *AppModel, args string) CommandResult {
	return CommandResult{TeaCmd: m.ShowModelPicker()}
}

func cmdNew(m *AppModel, args string) CommandResult {
	m.chatID = fmt.Sprintf("tui-%d", time.Now().UnixMilli())
	m.sessionSeq++
	m.chat = NewChatView(m.width, m.height-1, m.themeMode)
	m.chat.SetConnected(true) // Preserve connected state.
	return CommandResult{Message: "New session started."}
}

func cmdStatus(m *AppModel, args string) CommandResult {
	m.statusScreen = NewStatusScreen(m.theme, m.width, m.height-1)
	m.overlay = OverlayStatus
	m.chat.input.Blur()
	return CommandResult{TeaCmd: LoadHealth(m.client)}
}

func cmdSettings(m *AppModel, args string) CommandResult {
	m.settingsScreen = NewSettingsScreen(m.theme, m.chat.showThinking, m.width, m.height-1)
	m.overlay = OverlaySettings
	m.chat.input.Blur()
	return CommandResult{}
}

func cmdThink(m *AppModel, args string) CommandResult {
	m.chat.showThinking = !m.chat.showThinking
	if m.chat.showThinking {
		return CommandResult{Message: "Thinking display: on"}
	}
	return CommandResult{Message: "Thinking display: off"}
}

func cmdAttach(m *AppModel, args string) CommandResult {
	if args == "" {
		return CommandResult{Message: "Usage: /attach <file-path>"}
	}
	if err := m.attachments.Add(args); err != nil {
		return CommandResult{Message: fmt.Sprintf("Cannot attach: %s", err)}
	}
	return CommandResult{Message: fmt.Sprintf("Attached: %s", args)}
}

func cmdStub(m *AppModel, args string) CommandResult {
	return CommandResult{Message: "Not yet implemented — available in next update."}
}

func cmdExit(m *AppModel, args string) CommandResult {
	return CommandResult{Quit: true}
}
