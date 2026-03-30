package agent

import "github.com/xoai/sageclaw/pkg/canonical"

// EventType identifies the type of agent event.
type EventType string

const (
	EventRunStarted    EventType = "run.started"
	EventRunCompleted  EventType = "run.completed"
	EventRunFailed     EventType = "run.failed"
	EventChunk         EventType = "chunk"
	EventToolCall      EventType = "tool.call"
	EventToolResult    EventType = "tool.result"
	EventConsentNeeded EventType = "consent.needed"
	EventConsentResult EventType = "consent.result"
	EventVoiceStarted  EventType = "voice.started"
	EventVoiceAudio    EventType = "voice.audio"    // Audio chunk received from model.
	EventVoiceText     EventType = "voice.text"     // Transcript received.

	// Team orchestration events.
	EventTeamTaskCreated   EventType = "team.task.created"
	EventTeamTaskClaimed   EventType = "team.task.claimed"
	EventTeamTaskProgress  EventType = "team.task.progress"
	EventTeamTaskCompleted EventType = "team.task.completed"
	EventTeamTaskFailed    EventType = "team.task.failed"
	EventTeamTaskCancelled EventType = "team.task.cancelled"
	EventTeamTaskBlocked   EventType = "team.task.blocked"
	EventTeamTaskUnblocked EventType = "team.task.unblocked"
	EventTeamTaskReview    EventType = "team.task.review"
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
	Consent    *ConsentRequest      // For consent.needed events.
	TeamData   *TeamEventData       // For team.task.* events.
	Provider   string               // Provider name for this iteration (e.g. "anthropic", "gemini").
	Model      string               // Model ID for this iteration (e.g. "claude-sonnet-4-20250514").
}

// ConsentRequest carries information for a consent prompt.
type ConsentRequest struct {
	ToolName    string `json:"tool_name"`
	Group       string `json:"group"`
	Source      string `json:"source"`      // "builtin", "mcp:weather", "skill:code"
	RiskLevel   string `json:"risk_level"`  // Derived: "sensitive" for always-consent, "moderate" otherwise. For adapter compat.
	Explanation string `json:"explanation"`
	Nonce       string `json:"nonce"` // Unique nonce for this consent request.
}

// TeamEventData carries team task state for SSE events.
type TeamEventData struct {
	TeamID string `json:"team_id"`
	TaskID string `json:"task_id"`
	Seq    int64  `json:"seq"`
	Task   any    `json:"task,omitempty"` // Task snapshot
}

// EventHandler is a callback for agent events.
type EventHandler func(Event)
