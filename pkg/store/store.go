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
	AppendMessages(ctx context.Context, sessionID string, msgs []canonical.Message) error
	GetMessages(ctx context.Context, sessionID string, limit int) ([]canonical.Message, error)
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
	GetCronLastRun(ctx context.Context, id string) (time.Time, error)
	UpdateCronLastRun(ctx context.Context, id string, t time.Time) error
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

// TeamStore manages team task boards and mailboxes (v0.3+).
type TeamStore interface {
	CreateTask(ctx context.Context, task TeamTask) (string, error)
	ClaimTask(ctx context.Context, taskID, agentID string) error
	CompleteTask(ctx context.Context, taskID string, result string) error
	UpdateTaskStatus(ctx context.Context, taskID, status string) error
	ListTasks(ctx context.Context, teamID string, status string) ([]TeamTask, error)
	SendTeamMessage(ctx context.Context, msg TeamMessage) error
	GetTeamMessages(ctx context.Context, agentID string, unreadOnly bool) ([]TeamMessage, error)
	MarkMessageRead(ctx context.Context, messageID string) error
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
	DB() *sql.DB
	Close() error
}
