package test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/bus"
	localbus "github.com/xoai/sageclaw/pkg/bus/local"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/memory/fts5"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/pipeline"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/tool"
)

type mockProvider struct {
	mu        sync.Mutex
	responses []canonical.Response
	callCount int
}

func (m *mockProvider) Name() string { return "mock" }

func (m *mockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callCount >= len(m.responses) {
		return &canonical.Response{
			ID:         "fallback",
			StopReason: "end_turn",
			Messages: []canonical.Message{
				{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "No more scripted responses."}}},
			},
		}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return &resp, nil
}

func (m *mockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

// TestIntegration_FullPipeline tests the complete message flow:
// Inbound → debounce → classify → schedule → agent loop (tool call + response) → outbound.
func TestIntegration_FullPipeline(t *testing.T) {
	ctx := context.Background()

	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()
	store.DB().ExecContext(ctx, `INSERT INTO agents (id, name, model) VALUES ('default', 'test', 'mock')`)

	memEngine := fts5.New(store)
	sandbox, _ := security.NewSandbox(t.TempDir())

	toolReg := tool.NewRegistry()
	tool.RegisterFS(toolReg, sandbox)
	tool.RegisterMemory(toolReg, memEngine)
	tool.RegisterCron(toolReg, store)

	preCtx := middleware.Chain(middleware.PreContextMemory(memEngine))
	postTool := middleware.Chain(middleware.PostToolScrub(), middleware.PostToolAudit(store))

	prov := &mockProvider{
		responses: []canonical.Response{
			{
				ID: "msg_1", StopReason: "tool_use",
				Messages: []canonical.Message{{Role: "assistant", Content: []canonical.Content{
					{Type: "text", Text: "Searching memory..."},
					{Type: "tool_call", ToolCall: &canonical.ToolCall{ID: "c1", Name: "memory_search", Input: json.RawMessage(`{"query":"test"}`)}},
				}}},
				Usage: canonical.Usage{InputTokens: 100, OutputTokens: 30},
			},
			{
				ID: "msg_2", StopReason: "end_turn",
				Messages: []canonical.Message{{Role: "assistant", Content: []canonical.Content{
					{Type: "text", Text: "SageClaw is working end-to-end!"},
				}}},
				Usage: canonical.Usage{InputTokens: 150, OutputTokens: 20},
			},
		},
	}

	agentLoop := agent.NewLoop(agent.Config{
		AgentID: "default", SystemPrompt: "Test agent.", Model: "mock",
	}, prov, toolReg, preCtx, postTool, nil)

	msgBus := localbus.New()
	var p *pipeline.Pipeline
	scheduler := pipeline.NewLaneScheduler(pipeline.DefaultLaneLimits(), func(ctx context.Context, req pipeline.RunRequest) {
		p.RunAgent(ctx, req)
	})
	p = pipeline.New(msgBus, scheduler, store, agentLoop, pipeline.PipelineConfig{AgentID: "default"})

	var outbound []bus.Envelope
	var outMu sync.Mutex
	outDone := make(chan struct{}, 1)
	msgBus.SubscribeOutbound(ctx, func(env bus.Envelope) {
		outMu.Lock()
		outbound = append(outbound, env)
		outMu.Unlock()
		select {
		case outDone <- struct{}{}:
		default:
		}
	})

	p.Start(ctx)

	// Simulate inbound message.
	msgBus.PublishInbound(ctx, bus.Envelope{
		Channel: "telegram", ChatID: "12345",
		Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello SageClaw"}}}},
	})

	// Wait for response.
	select {
	case <-outDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for outbound response")
	}
	time.Sleep(200 * time.Millisecond)

	// Verify outbound response.
	outMu.Lock()
	if len(outbound) == 0 {
		t.Fatal("expected outbound messages")
	}
	found := false
	for _, env := range outbound {
		for _, msg := range env.Messages {
			for _, c := range msg.Content {
				if c.Type == "text" && c.Text != "" {
					t.Logf("Response: %s", c.Text)
					found = true
				}
			}
		}
	}
	outMu.Unlock()
	if !found {
		t.Fatal("no assistant response found")
	}

	// Verify session created and messages persisted.
	sess, err := store.FindSession(ctx, "telegram", "12345")
	if err != nil {
		t.Fatalf("finding session: %v", err)
	}
	msgs, _ := store.GetMessages(ctx, sess.ID, 0)
	t.Logf("Persisted %d messages in session %s", len(msgs), sess.ID)
	if len(msgs) == 0 {
		t.Fatal("expected persisted messages")
	}

	// Verify audit log.
	var auditCount int
	store.DB().QueryRow("SELECT COUNT(*) FROM audit_log").Scan(&auditCount)
	t.Logf("Audit log entries: %d", auditCount)

	// Verify provider call count.
	prov.mu.Lock()
	if prov.callCount != 2 {
		t.Fatalf("expected 2 provider calls, got %d", prov.callCount)
	}
	prov.mu.Unlock()
}

// TestIntegration_CommandBypass verifies commands skip the agent loop.
func TestIntegration_CommandBypass(t *testing.T) {
	ctx := context.Background()

	store, _ := sqlite.New(":memory:")
	defer store.Close()
	store.DB().ExecContext(ctx, `INSERT INTO agents (id, name, model) VALUES ('default', 'test', 'mock')`)

	prov := &mockProvider{}
	agentLoop := agent.NewLoop(agent.Config{AgentID: "default", Model: "mock"}, prov, tool.NewRegistry(), nil, nil, nil)

	msgBus := localbus.New()
	var p *pipeline.Pipeline
	scheduler := pipeline.NewLaneScheduler(pipeline.DefaultLaneLimits(), func(ctx context.Context, req pipeline.RunRequest) {
		p.RunAgent(ctx, req)
	})
	p = pipeline.New(msgBus, scheduler, store, agentLoop, pipeline.PipelineConfig{AgentID: "default"})

	var outbound []bus.Envelope
	var outMu sync.Mutex
	outDone := make(chan struct{}, 1)
	msgBus.SubscribeOutbound(ctx, func(env bus.Envelope) {
		outMu.Lock()
		outbound = append(outbound, env)
		outMu.Unlock()
		select {
		case outDone <- struct{}{}:
		default:
		}
	})

	p.Start(ctx)

	msgBus.PublishInbound(ctx, bus.Envelope{
		Channel: "telegram", ChatID: "99999",
		Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "/help"}}}},
	})

	select {
	case <-outDone:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for command response")
	}

	outMu.Lock()
	if len(outbound) == 0 {
		t.Fatal("expected command response")
	}
	outMu.Unlock()

	// Provider should not have been called.
	prov.mu.Lock()
	if prov.callCount != 0 {
		t.Fatalf("provider should not be called for commands, got %d calls", prov.callCount)
	}
	prov.mu.Unlock()
}

// TestIntegration_MemoryPersistence verifies memories survive across searches.
func TestIntegration_MemoryPersistence(t *testing.T) {
	ctx := context.Background()
	store, _ := sqlite.New(":memory:")
	defer store.Close()
	engine := fts5.New(store)

	id, _ := engine.Write(ctx, "SageClaw uses FTS5 for full-text search with BM25 ranking", "FTS5 arch", []string{"architecture"})

	results, err := engine.Search(ctx, "full-text search ranking", memory.SearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("searching: %v", err)
	}
	if len(results) == 0 || results[0].ID != id {
		t.Fatal("expected matching search result")
	}

	engine.Delete(ctx, id)
	results2, _ := engine.Search(ctx, "full-text search ranking", memory.SearchOptions{Limit: 5})
	for _, r := range results2 {
		if r.ID == id {
			t.Fatal("deleted memory should not appear")
		}
	}
}
