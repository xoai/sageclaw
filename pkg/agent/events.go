package agent

import "github.com/xoai/sageclaw/pkg/canonical"

// EventType identifies the type of agent event.
type EventType string

const (
	EventRunStarted  EventType = "run.started"
	EventRunCompleted EventType = "run.completed"
	EventRunFailed   EventType = "run.failed"
	EventChunk       EventType = "chunk"
	EventToolCall    EventType = "tool.call"
	EventToolResult  EventType = "tool.result"
)

// Event represents an observable event from the agent loop.
type Event struct {
	Type       EventType
	SessionID  string
	AgentID    string
	Text       string              // For chunk events.
	ToolCall   *canonical.ToolCall  // For tool.call events.
	ToolResult *canonical.ToolResult // For tool.result events.
	Error      error                // For run.failed events.
	Iteration  int                  // Current loop iteration.
}

// EventHandler is a callback for agent events.
type EventHandler func(Event)
