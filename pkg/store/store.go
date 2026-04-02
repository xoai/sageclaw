package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// SessionStore manages conversation sessions and messages.
type SessionStore interface {
	CreateSession(ctx context.Context, channel, chatID, agentID string) (*Session, error)
	CreateSessionWithKind(ctx context.Context, channel, chatID, agentID, kind string) (*Session, error)
	CreateSessionWithThread(ctx context.Context, channel, chatID, agentID, threadID string) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	FindSession(ctx context.Context, channel, chatID string) (*Session, error)
	FindSessionWithKind(ctx context.Context, channel, chatID, kind string) (*Session, error)
	FindSessionWithThread(ctx context.Context, channel, chatID, threadID string) (*Session, error)
	FindSessionByKey(ctx context.Context, key string) (*Session, error)
	UpdateSessionTokens(ctx context.Context, sessionID string, inputTokens, outputTokens int64, model, provider string) error
	UpdateSessionTitle(ctx context.Context, sessionID, title string) error
	UpdateSessionMetadata(ctx context.Context, sessionID string, merge map[string]string) error
	AppendMessages(ctx context.Context, sessionID string, msgs []canonical.Message) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]canonical.Message, error)
	ListSessions(ctx context.Context, limit int) ([]Session, error)
}

// MemoryStore manages the memory persistence layer.
type MemoryStore interface {
	WriteMemory(ctx context.Context, content, title string, tags []string) (string, bool, error)
	GetMemory(ctx context.Context, id string) (*Memory, error)
	DeleteMemory(ctx context.Context, id string) error
	ListMemories(ctx context.Context, filterTags []string, limit, offset int) ([]Memory, error)
	SearchMemories(ctx context.Context, query string, limit int) ([]Memory, []float64, error)
}

// CronStore manages scheduled jobs.
type CronStore interface {
	CreateCronJob(ctx context.Context, agentID, schedule, prompt string) (string, error)
	ListCronJobs(ctx context.Context) ([]CronJob, error)
	GetCronJob(ctx context.Context, id string) (*CronJob, error)
	GetCronLastRun(ctx context.Context, id string) (time.Time, error)
	UpdateCronLastRun(ctx context.Context, id string, t time.Time) error
	UpdateCronJob(ctx context.Context, id string, schedule, prompt *string) error
	DeleteCronJob(ctx context.Context, id string) error
}

// CredentialStore manages encrypted credentials.
type CredentialStore interface {
	StoreCredential(ctx context.Context, name string, value []byte, encKey []byte) error
	GetCredential(ctx context.Context, name string, encKey []byte) ([]byte, error)
}

// DelegationStore manages delegation state (v0.3+).
type DelegationStore interface {
	GetDelegationLinks(ctx context.Context, sourceAgentID string) ([]DelegationLink, error)
	IncrementDelegation(ctx context.Context, linkID string) error
	DecrementDelegation(ctx context.Context, linkID string) error
	GetDelegationCount(ctx context.Context, linkID string) (int, error)
	RecordDelegation(ctx context.Context, entry DelegationRecord) error
	GetDelegationRecord(ctx context.Context, delegationID string) (*DelegationRecord, error)
	GetDelegationHistory(ctx context.Context, agentID string, limit int) ([]DelegationRecord, error)
}

// ConnectionStore manages channel connections.
type ConnectionStore interface {
	CreateConnection(ctx context.Context, conn Connection) error
	GetConnection(ctx context.Context, id string) (*Connection, error)
	ListConnections(ctx context.Context, filter ConnectionFilter) ([]Connection, error)
	UpdateConnection(ctx context.Context, id string, fields map[string]any) error
	DeleteConnection(ctx context.Context, id string) error
}

// TeamStore manages teams, task boards, and mailboxes.
type TeamStore interface {
	// Team CRUD
	GetTeam(ctx context.Context, teamID string) (*Team, error)
	GetTeamByAgent(ctx context.Context, agentID string) (*Team, string, error) // Returns (team, role, err)
	UpdateTeam(ctx context.Context, teamID string, fields map[string]any) error
	ListTeamMembers(ctx context.Context, teamID string) ([]TeamMember, error)

	// Task lifecycle
	CreateTask(ctx context.Context, task TeamTask) (string, error)
	GetTask(ctx context.Context, taskID string) (*TeamTask, error)
	UpdateTask(ctx context.Context, taskID string, fields map[string]any) error
	UpdateTaskProgress(ctx context.Context, taskID string, percent int, text string) error
	ClaimTask(ctx context.Context, taskID, agentID string) error
	CompleteTask(ctx context.Context, taskID string, result string) error
	CancelTask(ctx context.Context, taskID string) error
	UpdateTaskStatus(ctx context.Context, taskID, status string) error
	ListTasks(ctx context.Context, teamID string, status string) ([]TeamTask, error)
	GetTasksByParent(ctx context.Context, parentID string) ([]TeamTask, error)
	GetBlockedTasks(ctx context.Context, teamID string) ([]TeamTask, error)
	UnblockTasks(ctx context.Context, completedTaskID string) ([]TeamTask, error)
	RetryTask(ctx context.Context, taskID string) error
	SearchTasks(ctx context.Context, teamID, query string) ([]TeamTask, error)
	NextTaskNumber(ctx context.Context, teamID string) (int, error)
	RecoverStaleTasks(ctx context.Context, timeoutSeconds int) ([]TeamTask, error)
	IncrementDispatchAttempt(ctx context.Context, taskID string) (int, error)
	CancelDependentTasks(ctx context.Context, taskID string) ([]TeamTask, error)
	FindDuplicateTask(ctx context.Context, teamID, title, assignee string) (*TeamTask, error)
	IncrementSubtaskCount(ctx context.Context, taskID string) error
	DecrementSubtaskCount(ctx context.Context, taskID string) error
	DeleteTask(ctx context.Context, taskID string) error
	DeleteTerminalTasks(ctx context.Context, teamID string) (int, error)

	// Task comments
	CreateComment(ctx context.Context, comment TeamTaskComment) (string, error)
	ListComments(ctx context.Context, taskID string) ([]TeamTaskComment, error)

	// Team messages (legacy mailbox)
	SendTeamMessage(ctx context.Context, msg TeamMessage) error
	GetTeamMessages(ctx context.Context, agentID string, unreadOnly bool) ([]TeamMessage, error)
	MarkMessageRead(ctx context.Context, messageID string) error
}

// MCPRegistryStore manages the MCP marketplace registry.
type MCPRegistryStore interface {
	UpsertMCPEntry(ctx context.Context, entry MCPRegistryEntry) error
	GetMCPEntry(ctx context.Context, id string) (*MCPRegistryEntry, error)
	DeleteMCPEntry(ctx context.Context, id string) error
	DeleteMCPCredential(ctx context.Context, name string) error
	ListMCPEntries(ctx context.Context, filter MCPFilter) ([]MCPRegistryEntry, error)
	SearchMCPEntries(ctx context.Context, query string, limit int) ([]MCPRegistryEntry, error)
	SetMCPStatus(ctx context.Context, id, status, statusError string) error
	SetMCPAgents(ctx context.Context, id string, agentIDs []string) error
	CountMCPByCategory(ctx context.Context) (map[string]int, error)
	CountMCPInstalled(ctx context.Context) (int, error)
	GetMCPSeedVersion(ctx context.Context) (int, error)
	SetMCPSeedVersion(ctx context.Context, version int) error
}

// ModelStore manages the discovered models cache.
type ModelStore interface {
	UpsertDiscoveredModels(ctx context.Context, models []DiscoveredModel) error
	ListDiscoveredModels(ctx context.Context, provider string) ([]DiscoveredModel, error)
	ListAllDiscoveredModels(ctx context.Context) ([]DiscoveredModel, error)
	DeleteDiscoveredModelsByProvider(ctx context.Context, provider string) error
	GetDiscoveredModelAge(ctx context.Context, provider string) (time.Duration, error)
	RefreshDiscoveredModels(ctx context.Context, provider string, models []DiscoveredModel) error
}

// ModelPricingOverride represents a user-set pricing override.
type ModelPricingOverride struct {
	ModelID           string
	Provider          string
	InputCost         float64
	OutputCost        float64
	CacheCost         float64
	ThinkingCost      float64
	CacheCreationCost float64
	UpdatedAt         time.Time
}

// PricingStore manages model pricing lookups and user overrides.
type PricingStore interface {
	// GetModelPricing returns pricing for a model: override > discovered > nil.
	GetModelPricing(ctx context.Context, modelID string) (*DiscoveredModel, error)
	UpsertModelPricingOverride(ctx context.Context, o ModelPricingOverride) error
	DeleteModelPricingOverride(ctx context.Context, modelID string) error
	ListModelPricingOverrides(ctx context.Context) ([]ModelPricingOverride, error)
	// BulkUpdateModelPricing updates pricing columns on discovered_models rows.
	// Only updates rows where pricing_source is not "user". Creates rows for
	// models not yet in discovered_models (OpenRouter-only models).
	BulkUpdateModelPricing(ctx context.Context, updates []ModelPricingBulk) error
}

// SettingsStore manages key-value settings.
type SettingsStore interface {
	GetSetting(ctx context.Context, key string) (string, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Store composes all store interfaces.
type Store interface {
	SessionStore
	MemoryStore
	CronStore
	CredentialStore
	DelegationStore
	TeamStore
	ConnectionStore
	MCPRegistryStore
	ModelStore
	PricingStore
	SettingsStore
	DB() *sql.DB
	Close() error
}
