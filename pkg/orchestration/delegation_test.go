package orchestration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/tool"
)

type mockProvider struct {
	response string
}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return &canonical.Response{
		ID: "mock_resp", StopReason: "end_turn",
		Messages: []canonical.Message{{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: m.response}}}},
	}, nil
}
func (m *mockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func setupDelegator(t *testing.T) (*Delegator, *sqlite.Store) {
	t.Helper()
	s, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	// Insert agents.
	s.DB().Exec(`INSERT INTO agents (id, name, model) VALUES ('coordinator', 'Coordinator', 'mock')`)
	s.DB().Exec(`INSERT INTO agents (id, name, model) VALUES ('researcher', 'Researcher', 'mock')`)

	// Insert delegation link.
	s.DB().Exec(`INSERT INTO delegation_links (id, source_id, target_id, direction, max_concurrent)
		VALUES ('link1', 'coordinator', 'researcher', 'sync', 2)`)

	configs := map[string]agent.Config{
		"coordinator": {AgentID: "coordinator", SystemPrompt: "You coordinate.", Model: "mock"},
		"researcher":  {AgentID: "researcher", SystemPrompt: "You research.", Model: "mock"},
	}

	prov := &mockProvider{response: "Research result: found 3 papers on the topic."}
	toolReg := tool.NewRegistry()

	d := NewDelegator(s, configs, prov, nil, toolReg)
	return d, s
}

func TestDelegation_SyncSuccess(t *testing.T) {
	d, _ := setupDelegator(t)
	ctx := context.Background()

	recordID, result, err := d.Delegate(ctx, "coordinator", "researcher", "Find papers on AI safety", "sync")
	if err != nil {
		t.Fatalf("delegation failed: %v", err)
	}
	if recordID == "" {
		t.Fatal("expected record ID")
	}
	if result == "" {
		t.Fatal("expected result from sync delegation")
	}
	t.Logf("Result: %s", result)
}

func TestDelegation_NoLink(t *testing.T) {
	d, _ := setupDelegator(t)
	ctx := context.Background()

	_, _, err := d.Delegate(ctx, "researcher", "coordinator", "reverse delegation", "sync")
	if err == nil {
		t.Fatal("expected error for missing delegation link")
	}
}

func TestDelegation_AsyncReturnsID(t *testing.T) {
	d, _ := setupDelegator(t)
	ctx := context.Background()

	recordID, result, err := d.Delegate(ctx, "coordinator", "researcher", "Async research task", "async")
	if err != nil {
		t.Fatalf("delegation failed: %v", err)
	}
	if recordID == "" {
		t.Fatal("expected record ID")
	}
	if result != "" {
		t.Fatal("expected empty result for async delegation")
	}
}

func TestDelegation_DepthLimit(t *testing.T) {
	d, _ := setupDelegator(t)

	// Set depth to max.
	ctx := context.WithValue(context.Background(), delegationDepthKey{}, maxDelegationDepth)
	_, _, err := d.Delegate(ctx, "coordinator", "researcher", "too deep", "sync")
	if err == nil {
		t.Fatal("expected depth limit error")
	}
}

func TestDelegation_HotReloadAdd(t *testing.T) {
	d, s := setupDelegator(t)
	ctx := context.Background()

	// Delegation from researcher→coordinator should fail (no link).
	_, _, err := d.Delegate(ctx, "researcher", "coordinator", "reverse", "sync")
	if err == nil {
		t.Fatal("expected error for missing link")
	}

	// Create link in DB AFTER delegator construction (simulates dashboard create).
	s.DB().Exec(`INSERT INTO delegation_links (id, source_id, target_id, direction, max_concurrent)
		VALUES ('link2', 'researcher', 'coordinator', 'sync', 1)`)
	s.DB().Exec(`INSERT OR IGNORE INTO delegation_state (link_id, active_count) VALUES ('link2', 0)`)

	// Now delegation should succeed without restart.
	_, result, err := d.Delegate(ctx, "researcher", "coordinator", "hot-reload test", "sync")
	if err != nil {
		t.Fatalf("expected hot-reload link to work: %v", err)
	}
	if result == "" {
		t.Fatal("expected result from hot-reloaded link")
	}
}

func TestDelegation_HotReloadRemove(t *testing.T) {
	d, s := setupDelegator(t)
	ctx := context.Background()

	// Delegation should work (link exists from setup).
	_, _, err := d.Delegate(ctx, "coordinator", "researcher", "before delete", "sync")
	if err != nil {
		t.Fatalf("expected delegation to work: %v", err)
	}

	// Delete link from DB (simulates dashboard delete).
	// Must delete state first (FK reference), then link.
	s.DB().Exec(`DELETE FROM delegation_state WHERE link_id = 'link1'`)
	res, delErr := s.DB().Exec(`DELETE FROM delegation_links WHERE id = 'link1'`)
	if delErr != nil {
		t.Fatalf("failed to delete link: %v", delErr)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("expected 1 row deleted, got %d", n)
	}

	// Now delegation should fail immediately.
	_, _, err = d.Delegate(ctx, "coordinator", "researcher", "after delete", "sync")
	if err == nil {
		t.Fatal("expected error after link deleted from DB")
	}
}

// Ensure json import is used (for tool registry).
var _ = json.RawMessage{}
