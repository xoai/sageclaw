package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// mockPolicyStore implements store.Store with only GetConnection populated.
// All other methods panic — they should never be called in policy tests.
type mockPolicyStore struct {
	connections map[string]*store.Connection
}

func (m *mockPolicyStore) GetConnection(_ context.Context, id string) (*store.Connection, error) {
	conn, ok := m.connections[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return conn, nil
}

// Stub implementations to satisfy store.Store interface.
func (m *mockPolicyStore) CreateSession(context.Context, string, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) CreateSessionWithKind(context.Context, string, string, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) CreateSessionWithThread(context.Context, string, string, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) GetSession(context.Context, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) FindSession(context.Context, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) FindSessionWithKind(context.Context, string, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) FindSessionWithThread(context.Context, string, string, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) FindSessionByKey(context.Context, string) (*store.Session, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) UpdateSessionTokens(context.Context, string, int64, int64, string, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) UpdateSessionTitle(context.Context, string, string) error {
	return nil
}
func (m *mockPolicyStore) AppendMessages(context.Context, string, []canonical.Message) error {
	panic("not implemented")
}
func (m *mockPolicyStore) GetMessages(context.Context, string, int) ([]canonical.Message, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) WriteMemory(context.Context, string, string, []string) (string, bool, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) GetMemory(context.Context, string) (*store.Memory, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) DeleteMemory(context.Context, string) error { panic("not implemented") }
func (m *mockPolicyStore) ListMemories(context.Context, []string, int, int) ([]store.Memory, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) SearchMemories(context.Context, string, int) ([]store.Memory, []float64, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) CreateCronJob(context.Context, string, string, string) (string, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) ListCronJobs(context.Context) ([]store.CronJob, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) GetCronLastRun(context.Context, string) (time.Time, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) UpdateCronLastRun(context.Context, string, time.Time) error {
	panic("not implemented")
}
func (m *mockPolicyStore) DeleteCronJob(context.Context, string) error { panic("not implemented") }
func (m *mockPolicyStore) StoreCredential(context.Context, string, []byte, []byte) error {
	panic("not implemented")
}
func (m *mockPolicyStore) GetCredential(context.Context, string, []byte) ([]byte, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) GetDelegationLinks(context.Context, string) ([]store.DelegationLink, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) IncrementDelegation(context.Context, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) DecrementDelegation(context.Context, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) GetDelegationCount(context.Context, string) (int, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) RecordDelegation(context.Context, store.DelegationRecord) error {
	panic("not implemented")
}
func (m *mockPolicyStore) GetDelegationHistory(context.Context, string, int) ([]store.DelegationRecord, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) CreateConnection(context.Context, store.Connection) error {
	panic("not implemented")
}
func (m *mockPolicyStore) ListConnections(context.Context, store.ConnectionFilter) ([]store.Connection, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) UpdateConnection(context.Context, string, map[string]any) error {
	panic("not implemented")
}
func (m *mockPolicyStore) DeleteConnection(context.Context, string) error { panic("not implemented") }
func (m *mockPolicyStore) CreateTask(context.Context, store.TeamTask) (string, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) ClaimTask(context.Context, string, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) CompleteTask(context.Context, string, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) UpdateTaskStatus(context.Context, string, string) error {
	panic("not implemented")
}
func (m *mockPolicyStore) ListTasks(context.Context, string, string) ([]store.TeamTask, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) SendTeamMessage(context.Context, store.TeamMessage) error {
	panic("not implemented")
}
func (m *mockPolicyStore) GetTeamMessages(context.Context, string, bool) ([]store.TeamMessage, error) {
	panic("not implemented")
}
func (m *mockPolicyStore) MarkMessageRead(context.Context, string) error { panic("not implemented") }
func (m *mockPolicyStore) DB() *sql.DB                                   { return nil }
func (m *mockPolicyStore) Close() error                                  { return nil }

// pipelineWithConnections creates a Pipeline with a mock store for policy tests.
func pipelineWithConnections(conns map[string]*store.Connection) *Pipeline {
	return &Pipeline{
		store: &mockPolicyStore{connections: conns},
	}
}

// --- checkPolicy tests ---

func TestCheckPolicy_DmAllowed(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: true, GroupEnabled: true},
	})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "dm", ChatID: "123",
	})
	if !got {
		t.Fatal("expected DM to be allowed")
	}
}

func TestCheckPolicy_DmDisabled(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: false, GroupEnabled: true},
	})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "dm", ChatID: "123",
	})
	if got {
		t.Fatal("expected DM to be dropped when DmEnabled=false")
	}
}

func TestCheckPolicy_GroupMentioned(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: true, GroupEnabled: true},
	})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "group", ChatID: "-100123", Mentioned: true,
	})
	if !got {
		t.Fatal("expected mentioned group message to be allowed")
	}
}

func TestCheckPolicy_GroupNotMentioned(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: true, GroupEnabled: true},
	})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "group", ChatID: "-100123", Mentioned: false,
	})
	if got {
		t.Fatal("expected unmentioned group message to be dropped")
	}
}

func TestCheckPolicy_GroupDisabled(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: true, GroupEnabled: false},
	})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "group", ChatID: "-100123", Mentioned: true,
	})
	if got {
		t.Fatal("expected group message to be dropped when GroupEnabled=false")
	}
}

func TestCheckPolicy_UnknownConnection(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{})

	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "unknown_conn", Kind: "dm", ChatID: "123",
	})
	if !got {
		t.Fatal("expected unknown connection to be allowed (legacy compat)")
	}
}

func TestCheckPolicy_EmptyKindAllowed(t *testing.T) {
	p := pipelineWithConnections(map[string]*store.Connection{
		"tg_abc": {ID: "tg_abc", DmEnabled: true, GroupEnabled: true},
	})

	// Empty kind (legacy envelope) should pass through.
	got := p.checkPolicy(context.Background(), bus.Envelope{
		Channel: "tg_abc", Kind: "", ChatID: "123",
	})
	if !got {
		t.Fatal("expected empty kind to be allowed (legacy compat)")
	}
}

// --- channelKey / parseChannelKey tests ---

func TestChannelKey_DmBasic(t *testing.T) {
	key := channelKey("tg_abc", "dm", "123", "", "")
	channel, kind, chatID, threadID, agentID := parseChannelKey(key)
	if channel != "tg_abc" || kind != "dm" || chatID != "123" || threadID != "" || agentID != "" {
		t.Fatalf("unexpected parse: ch=%s kind=%s chat=%s thread=%s agent=%s", channel, kind, chatID, threadID, agentID)
	}
}

func TestChannelKey_GroupWithThread(t *testing.T) {
	key := channelKey("tg_abc", "group", "-100123", "99", "")
	channel, kind, chatID, threadID, agentID := parseChannelKey(key)
	if channel != "tg_abc" || kind != "group" || chatID != "-100123" || threadID != "99" || agentID != "" {
		t.Fatalf("unexpected parse: ch=%s kind=%s chat=%s thread=%s agent=%s", channel, kind, chatID, threadID, agentID)
	}
}

func TestChannelKey_WithAgent(t *testing.T) {
	key := channelKey("tg_abc", "dm", "123", "", "agent1")
	channel, kind, chatID, threadID, agentID := parseChannelKey(key)
	if channel != "tg_abc" || kind != "dm" || chatID != "123" || threadID != "" || agentID != "agent1" {
		t.Fatalf("unexpected parse: ch=%s kind=%s chat=%s thread=%s agent=%s", channel, kind, chatID, threadID, agentID)
	}
}

func TestChannelKey_ThreadAndAgent(t *testing.T) {
	key := channelKey("tg_abc", "group", "-100123", "99", "agent1")
	channel, kind, chatID, threadID, agentID := parseChannelKey(key)
	if channel != "tg_abc" || kind != "group" || chatID != "-100123" || threadID != "99" || agentID != "agent1" {
		t.Fatalf("unexpected parse: ch=%s kind=%s chat=%s thread=%s agent=%s", channel, kind, chatID, threadID, agentID)
	}
}

func TestChannelKey_DmAndGroupDifferent(t *testing.T) {
	dmKey := channelKey("tg_abc", "dm", "123", "", "")
	groupKey := channelKey("tg_abc", "group", "123", "", "")
	if dmKey == groupKey {
		t.Fatal("DM and group keys should be different for same chatID")
	}
}

func TestChannelKey_EmptyKindDefaultsDm(t *testing.T) {
	key := channelKey("tg_abc", "", "123", "", "")
	_, kind, _, _, _ := parseChannelKey(key)
	if kind != "dm" {
		t.Fatalf("expected empty kind to default to 'dm', got %s", kind)
	}
}
