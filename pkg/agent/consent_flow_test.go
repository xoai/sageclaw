package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

func TestInjectTo_TargetsCorrectLoop(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
		"agent2": {AgentID: "agent2"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	// Ensure loops are created.
	l1 := pool.Get("agent1")
	l2 := pool.Get("agent2")
	if l1 == nil || l2 == nil {
		t.Fatal("expected both loops")
	}

	msg := canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: "targeted"}},
	}
	pool.InjectTo("agent1", msg)

	// agent1 should receive the message.
	select {
	case got := <-l1.injectChan:
		if got.Content[0].Text != "targeted" {
			t.Errorf("expected 'targeted', got %q", got.Content[0].Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("agent1 should have received the message")
	}

	// agent2 should NOT receive the message.
	select {
	case <-l2.injectChan:
		t.Error("agent2 should NOT have received the message")
	case <-time.After(50 * time.Millisecond):
		// Expected.
	}
}

func TestInjectTo_UnknownAgent(t *testing.T) {
	pool := NewLoopPool(map[string]Config{}, nil, nil, nil, nil, nil)
	// Should not panic.
	pool.InjectTo("nonexistent", canonical.Message{})
}

func TestHeadlessConfig(t *testing.T) {
	cfg := Config{
		AgentID:      "test",
		Headless:     true,
		PreAuthorize: []string{"runtime", "mcp:weather"},
	}
	l := NewLoop(cfg, nil, nil, nil, nil, nil)

	if !l.config.Headless {
		t.Error("expected headless to be true")
	}
	if len(l.config.PreAuthorize) != 2 {
		t.Errorf("expected 2 pre-authorize entries, got %d", len(l.config.PreAuthorize))
	}
}

// makeTestRegistry creates a registry with common test tools.
func makeTestRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }
	reg.RegisterWithGroup("read_file", "Read", nil, tool.GroupFS, tool.RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, tool.GroupRuntime, tool.RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("memory_search", "Search", nil, tool.GroupMemory, tool.RiskSafe, "builtin", noop)
	reg.RegisterWithGroup("mcp_weather", "Weather", nil, tool.GroupMCP, tool.RiskSensitive, "mcp:weather", noop)
	reg.RegisterWithGroup("delegate_task", "Delegate", nil, tool.GroupOrchestration, tool.RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("team_send", "Send", nil, tool.GroupTeam, tool.RiskModerate, "builtin", noop)
	return reg
}

func TestCheckConsent_InProfileNoConsent(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "coding"}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// fs tools are in coding profile and not always-consent → no consent needed.
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "read_file"}, 0)
	if result != nil {
		t.Errorf("in-profile tool should not need consent, got: %s", result.Content)
	}

	// memory tools are in coding profile → no consent needed.
	result = l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "2", Name: "memory_search"}, 0)
	if result != nil {
		t.Errorf("in-profile safe tool should not need consent, got: %s", result.Content)
	}
}

func TestCheckConsent_AlwaysConsentBlocks(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	// Use coding profile (includes runtime) — but runtime is always-consent.
	l := NewLoop(Config{AgentID: "test", ToolProfile: "coding"}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// execute_command is runtime (always-consent) → should block waiting for consent.
	// We test with a cancelled context to avoid blocking forever.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.
	result := l.checkConsent(ctx, "s1", canonical.ToolCall{ID: "1", Name: "execute_command"}, 0)
	if result == nil {
		t.Fatal("always-consent tool should require consent even when in-profile")
	}
	if result.Content != "Context cancelled while waiting for consent." {
		t.Errorf("unexpected error: %s", result.Content)
	}
}

func TestCheckConsent_OutOfProfileBlocks(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	// messaging profile doesn't include team_send's group? Actually it does: team is in messaging.
	// Use readonly profile — doesn't include team.
	l := NewLoop(Config{AgentID: "test", ToolProfile: "readonly"}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := l.checkConsent(ctx, "s1", canonical.ToolCall{ID: "1", Name: "team_send"}, 0)
	if result == nil {
		t.Fatal("out-of-profile tool should require consent")
	}
}

func TestCheckConsent_HeadlessInProfileAllowed(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "coding", Headless: true}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// fs is in-profile, not always-consent → allowed for headless.
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "read_file"}, 0)
	if result != nil {
		t.Errorf("headless agent should allow in-profile tools, got: %s", result.Content)
	}
}

func TestCheckConsent_HeadlessAlwaysConsentBlocked(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "coding", Headless: true}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// runtime is always-consent → blocked for headless without pre-authorize.
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "execute_command"}, 0)
	if result == nil {
		t.Fatal("headless agent should block always-consent tools without pre-authorize")
	}
	if !result.IsError {
		t.Error("should be an error result")
	}
}

func TestCheckConsent_HeadlessPreAuthorized(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{
		AgentID:      "test",
		ToolProfile:  "coding",
		Headless:     true,
		PreAuthorize: []string{"runtime", "mcp:weather"},
	}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// runtime is pre-authorized → allowed.
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "execute_command"}, 0)
	if result != nil {
		t.Errorf("pre-authorized runtime should be allowed, got: %s", result.Content)
	}

	// mcp:weather is pre-authorized → allowed.
	result = l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "2", Name: "mcp_weather"}, 0)
	if result != nil {
		t.Errorf("pre-authorized mcp:weather should be allowed, got: %s", result.Content)
	}

	// orchestration is NOT pre-authorized → blocked for non-team-lead agents.
	result = l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "3", Name: "delegate_task"}, 0)
	if result == nil {
		t.Fatal("non-pre-authorized orchestration should be blocked for non-team-lead")
	}
}

func TestCheckConsent_TeamLeadBypassesOrchestration(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{
		AgentID:     "lead-1",
		ToolProfile: "coding",
		Headless:    true,
		TeamInfo: &TeamInfoConfig{
			TeamID: "team-1",
			Role:   "lead",
		},
	}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// Team lead bypasses orchestration consent — delegation is friction-free.
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "delegate_task"}, 0)
	if result != nil {
		t.Errorf("team lead should bypass orchestration consent, got: %s", result.Content)
	}

	// But runtime still requires pre-authorization even for leads.
	result = l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "2", Name: "execute_command"}, 0)
	if result == nil {
		t.Fatal("team lead should NOT bypass runtime consent")
	}
}

func TestCheckConsent_HeadlessOutOfProfileBlocked(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "readonly", Headless: true}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// team_send is not in readonly profile → blocked (no consent prompt possible).
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "team_send"}, 0)
	if result == nil {
		t.Fatal("headless agent should block out-of-profile tools")
	}
}

func TestCheckConsent_DenyListBlocks(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "coding", ToolDeny: []string{"read_file"}}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "read_file"}, 0)
	if result == nil {
		t.Fatal("denied tool should be blocked")
	}
	if !result.IsError {
		t.Error("should be an error result")
	}
}

func TestCheckConsent_MCPPerServerKey(t *testing.T) {
	reg := makeTestRegistry()
	cs := tool.NewPersistentConsentStore(nil)
	l := NewLoop(Config{AgentID: "test", ToolProfile: "full"}, nil, reg, nil, nil, nil, WithConsentStore(cs))

	// Grant consent for mcp:weather (per-server key).
	cs.GrantOnce("s1", "mcp:weather")

	// mcp_weather should pass (consent key is "mcp:weather", not "mcp").
	result := l.checkConsent(context.Background(), "s1", canonical.ToolCall{ID: "1", Name: "mcp_weather"}, 0)
	if result != nil {
		t.Errorf("MCP tool with per-server grant should pass, got: %s", result.Content)
	}
}

func TestResolveOwner_Cached(t *testing.T) {
	callCount := 0
	resolver := func(sessionID string) (string, string) {
		callCount++
		return "user123", "telegram"
	}

	l := NewLoop(Config{AgentID: "test"}, nil, nil, nil, nil, nil, WithOwnerResolver(resolver))

	ownerID, platform := l.resolveOwner("s1")
	if ownerID != "user123" || platform != "telegram" {
		t.Errorf("resolveOwner = (%q, %q), want (user123, telegram)", ownerID, platform)
	}

	// Second call should use cache.
	l.resolveOwner("s1")
	if callCount != 1 {
		t.Errorf("resolver called %d times, expected 1 (cached)", callCount)
	}

	// Different session triggers new call.
	l.resolveOwner("s2")
	if callCount != 2 {
		t.Errorf("resolver called %d times, expected 2 (new session)", callCount)
	}
}

func TestResolveOwner_NilResolver(t *testing.T) {
	l := NewLoop(Config{AgentID: "test"}, nil, nil, nil, nil, nil)

	ownerID, platform := l.resolveOwner("s1")
	if ownerID != "" || platform != "" {
		t.Errorf("nil resolver should return empty, got (%q, %q)", ownerID, platform)
	}
}

func TestNonceManager_IntegrationWithPool(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	nm := NewNonceManager(ctx)
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)
	l := pool.Get("agent1")
	if l == nil {
		t.Fatal("expected loop")
	}

	// Generate nonce.
	pc, err := nm.Generate("agent1", "session1", "runtime")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	// Build consent token.
	token := "__consent__" + pc.Nonce + "_grant_once"

	// Inject targeted to agent1.
	pool.InjectTo("agent1", canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: token}},
	})

	// Verify it arrives.
	select {
	case msg := <-l.injectChan:
		if msg.Content[0].Text != token {
			t.Errorf("expected token %q, got %q", token, msg.Content[0].Text)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("should have received consent token")
	}

	// Validate nonce (single-use).
	result, err := nm.Validate(pc.Nonce)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if result.AgentID != "agent1" {
		t.Errorf("expected agent1, got %s", result.AgentID)
	}
}
