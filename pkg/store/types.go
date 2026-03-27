package store

import (
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Session represents a conversation session.
type Session struct {
	ID              string
	Key             string // Composite: {agentID}:{channel}:{kind}:{chatID}
	Channel         string
	ChatID          string
	AgentID         string
	Kind            string // direct, subagent, cron
	Label           string
	Status          string // active, archived, compacted
	Model           string
	Provider        string
	SpawnedBy       string // Parent session ID (for subagents)
	InputTokens     int64
	OutputTokens    int64
	CompactionCount int
	MessageCount    int
	Title           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Metadata        map[string]string
}

// SessionKey builds a composite session key.
func SessionKey(agentID, channel, kind, chatID string) string {
	return agentID + ":" + channel + ":" + kind + ":" + chatID
}

// SessionKeyWithThread builds a session key that includes thread context.
func SessionKeyWithThread(agentID, channel, kind, chatID, threadID string) string {
	key := SessionKey(agentID, channel, kind, chatID)
	if threadID != "" {
		return key + ":" + threadID
	}
	return key
}

// Memory represents a stored memory entry.
type Memory struct {
	ID          string
	Title       string
	Content     string
	Tags        []string
	ContentHash string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AccessedAt  time.Time
	AccessCount int
	Confidence  float64 // 0.0-1.0: trust level (0.9=correction, 0.8=default, 0.7=fact, 0.5=inferred)
}

// CronJob represents a scheduled job.
type CronJob struct {
	ID       string
	AgentID  string
	Schedule string
	Prompt   string
	Enabled  bool
}

// DelegationLink defines a permitted delegation path.
type DelegationLink struct {
	ID            string
	SourceID      string
	TargetID      string
	Direction     string // "sync" or "async"
	MaxConcurrent int
	TimeoutSec    int // Per-link timeout in seconds. 0 = use default (300s).
}

// DelegationRecord captures a delegation execution.
type DelegationRecord struct {
	ID          string
	LinkID      string
	SourceID    string
	TargetID    string
	Prompt      string
	Result      string
	Status      string // "pending", "running", "completed", "failed"
	StartedAt   time.Time
	CompletedAt *time.Time
}

// TeamTask represents a task on the team board.
type TeamTask struct {
	ID          string
	TeamID      string
	Title       string
	Description string
	Status      string // "open", "claimed", "completed", "blocked"
	AssignedTo  string
	CreatedBy   string
	Result      string
	BlockedBy   string // Comma-separated task IDs this task depends on.
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TeamMessage represents a mailbox message.
type TeamMessage struct {
	ID        string
	TeamID    string
	FromAgent string
	ToAgent   string // Empty = broadcast.
	Content   string
	Read      bool
	CreatedAt time.Time
}

// Connection represents a channel connection (e.g., a Telegram bot).
type Connection struct {
	ID            string
	Platform      string    // "telegram", "discord", "zalo", "whatsapp"
	AgentID       string    // Bound agent (empty = unbound)
	Label         string    // Auto from metadata: "@botname"
	Metadata      string    // JSON: {username, first_name, ...}
	CredentialKey string    // Legacy: key in credentials table
	Credentials   []byte    // Encrypted JSON blob: {"token": "...", ...}
	DmEnabled     bool      // Allow DM messages
	GroupEnabled  bool      // Allow group messages
	Status        string    // "active", "stopped", "error"
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// ConnectionFilter for listing connections.
type ConnectionFilter struct {
	Platform string
	Status   string
	AgentID  string
}

// Message re-export for convenience.
type Message = canonical.Message
