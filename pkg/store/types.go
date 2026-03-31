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

// Team represents a team definition.
type Team struct {
	ID          string
	Name        string
	LeadID      string
	Description string
	Status      string // "active", "archived"
	Config      string // JSON: legacy field from migration 004
	Settings    string // JSON: {max_concurrent, chat_verbosity, ...}
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// TeamMember represents a member of a team.
type TeamMember struct {
	AgentID     string
	Role        string // "lead" or "member"
	DisplayName string // From identity.yaml
	Description string // From identity.yaml role field
}

// TeamTask represents a task on the team board.
type TeamTask struct {
	ID              string     `json:"id"`
	TeamID          string     `json:"team_id"`
	Title           string     `json:"title"`
	Description     string     `json:"description"`
	Status          string     `json:"status"` // "pending", "in_progress", "completed", "blocked", "in_review", "cancelled", "failed"
	AssignedTo      string     `json:"assigned_to"`
	CreatedBy       string     `json:"created_by"`
	Result          string     `json:"result"`
	BlockedBy       string     `json:"blocked_by"` // Comma-separated task IDs this task depends on.
	ParentID        string     `json:"parent_id"`  // Subtask parent
	Priority        int        `json:"priority"`   // Higher = more urgent
	OwnerAgentID    string     `json:"owner_agent_id"` // Lead who created this task
	BatchID         string     `json:"batch_id"` // Groups tasks from same delegation turn
	TaskNumber      int        `json:"task_number"` // Auto-increment per team
	Identifier      string     `json:"identifier"` // Team prefix + task_number (e.g., "TSK-12")
	ProgressPercent int        `json:"progress_percent"` // 0-100
	RequireApproval bool       `json:"require_approval"` // Needs lead approval before completing
	SessionID       string     `json:"session_id"` // Session used for execution
	RetryCount       int        `json:"retry_count"`
	MaxRetries       int        `json:"max_retries"`
	ErrorMessage     string     `json:"error_message"`
	SubtaskCount     int        `json:"subtask_count"`
	DispatchAttempts int        `json:"dispatch_attempts"`
	ClaimedAt        *time.Time `json:"claimed_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// TeamTaskComment represents a comment on a task.
type TeamTaskComment struct {
	ID          string
	TaskID      string
	AgentID     string
	UserID      string
	Content     string
	CommentType string // "note", "status", "system"
	CreatedAt   time.Time
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
	OwnerUserID   string    // Platform user ID of the connection owner
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

// MCPRegistryEntry represents an MCP server in the marketplace registry.
type MCPRegistryEntry struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Description  string     `json:"description,omitempty"`
	Category     string     `json:"category,omitempty"`
	Connection   string     `json:"connection"`            // JSON string
	FallbackConn string     `json:"fallback_connection,omitempty"` // JSON string
	ConfigSchema string     `json:"config_schema,omitempty"`       // JSON string
	GitHubURL    string     `json:"github_url,omitempty"`
	Stars        int        `json:"stars,omitempty"`
	Tags         []string   `json:"tags,omitempty"`
	Source       string     `json:"source"`    // "curated" | "custom"
	Installed    bool       `json:"installed"`
	Enabled      bool       `json:"enabled"`
	Status       string     `json:"status"`        // available | installing | connected | disabled | failed
	StatusError  string     `json:"status_error,omitempty"`
	AgentIDs     []string   `json:"agent_ids,omitempty"`
	InstalledAt  *time.Time `json:"installed_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// DiscoveredModel represents a model fetched from a provider's API.
type DiscoveredModel struct {
	ID              string          // "anthropic/claude-sonnet-4-20250514"
	Provider        string          // "anthropic", "openai", "gemini", "ollama"
	ModelID         string          // Raw API model ID
	DisplayName     string          // "Claude Sonnet 4"
	ContextWindow   int             // Max input tokens
	MaxOutputTokens int             // Max output tokens
	Capabilities    map[string]bool // {"vision":true,"thinking":true}
	DiscoveredAt    time.Time
	UpdatedAt       time.Time
}

// MCPFilter for listing MCP registry entries.
type MCPFilter struct {
	Category  string
	Installed *bool
	Enabled   *bool
	Status    []string // Filter by status values (e.g., ["connected", "disabled"])
	Source    string
	Query     string
	Limit     int
	Offset    int
}
