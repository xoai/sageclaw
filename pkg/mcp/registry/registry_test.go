package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/store"
	sqliteStore "github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/tool"
)

func newTestRegistry(t *testing.T) (*Registry, *sqliteStore.Store) {
	t.Helper()
	s, err := sqliteStore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	encKey := make([]byte, 32)
	toolReg := tool.NewRegistry()
	mgr := mcp.NewManager(toolReg)

	reg, err := NewRegistry(s, s, encKey, mgr)
	if err != nil {
		t.Fatalf("creating registry: %v", err)
	}
	return reg, s
}

func TestSeedFromCurated(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	if err := reg.SeedFromCurated(ctx); err != nil {
		t.Fatalf("seed: %v", err)
	}

	entries, err := s.ListMCPEntries(ctx, store.MCPFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 401 {
		t.Errorf("seeded entries = %d, want 401", len(entries))
	}

	// Version should be updated.
	v, _ := s.GetMCPSeedVersion(ctx)
	if v != 4 {
		t.Errorf("seed version = %d, want 4", v)
	}

	// Second seed should be no-op.
	if err := reg.SeedFromCurated(ctx); err != nil {
		t.Fatalf("seed2: %v", err)
	}
}

func TestSeedPreservesStatus(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	// Seed first time.
	reg.SeedFromCurated(ctx)

	// Simulate connected state.
	s.SetMCPStatus(ctx, "tavily-ai-tavily-mcp", "connected", "")
	s.SetMCPAgents(ctx, "tavily-ai-tavily-mcp", []string{"default"})

	// Force re-seed by changing stored version.
	s.SetMCPSeedVersion(ctx, 0)
	reg.SeedFromCurated(ctx)

	// Status should be preserved.
	entry, _ := s.GetMCPEntry(ctx, "tavily-ai-tavily-mcp")
	if entry.Status != "connected" {
		t.Errorf("status should be preserved, got %q", entry.Status)
	}
	if !entry.Installed {
		t.Error("installed should be preserved after re-seed")
	}
	if len(entry.AgentIDs) != 1 || entry.AgentIDs[0] != "default" {
		t.Errorf("agent_ids should be preserved, got %v", entry.AgentIDs)
	}
}

func TestValidateConfigValue(t *testing.T) {
	tests := []struct {
		field string
		value string
		valid bool
	}{
		{"API_KEY", "sk-ant-test-key-123", true},
		{"API_KEY", "normal_value_with-dashes.and.dots", true},
		{"API_KEY", "has;semicolon", false},
		{"API_KEY", "has|pipe", false},
		{"API_KEY", "has&ampersand", false},
		{"API_KEY", "has$dollar", false},
		{"API_KEY", "has`backtick", false},
		{"API_KEY", "has\nnewline", false},
		{"API_KEY", "", true},
	}

	for _, tt := range tests {
		err := validateConfigValue(tt.field, tt.value)
		if tt.valid && err != nil {
			t.Errorf("validateConfigValue(%q, %q) = %v, want nil", tt.field, tt.value, err)
		}
		if !tt.valid && err == nil {
			t.Errorf("validateConfigValue(%q, %q) = nil, want error", tt.field, tt.value)
		}
	}
}

func TestParseConfigSchema(t *testing.T) {
	schema := parseConfigSchema(`{"API_KEY":{"type":"string","required":true},"OPT":{"type":"string","required":false}}`)
	if len(schema) != 2 {
		t.Errorf("schema fields = %d, want 2", len(schema))
	}
	if !schema["API_KEY"].Required {
		t.Error("API_KEY should be required")
	}
	if schema["OPT"].Required {
		t.Error("OPT should not be required")
	}

	// Empty schema.
	empty := parseConfigSchema("{}")
	if empty != nil {
		t.Errorf("empty schema should be nil, got %v", empty)
	}
}

func TestCuratedToEntry(t *testing.T) {
	idx, _ := LoadCuratedIndex()
	s, ok := idx.Get("tavily-ai-tavily-mcp")
	if !ok {
		t.Fatal("tavily-ai-tavily-mcp should exist")
	}

	entry := curatedToEntry(*s)
	if entry.ID != "tavily-ai-tavily-mcp" {
		t.Errorf("id = %s", entry.ID)
	}
	if entry.Source != "curated" {
		t.Errorf("source = %s, want curated", entry.Source)
	}
	if entry.Connection == "" {
		t.Error("connection should not be empty")
	}
}

func TestCuratedToEntry_NoConfigSchema(t *testing.T) {
	idx, _ := LoadCuratedIndex()
	s, ok := idx.Get("microsoft-playwright-mcp")
	if !ok {
		t.Fatal("microsoft-playwright-mcp should exist")
	}
	if len(s.ConfigSchema) != 0 {
		t.Fatalf("playwright should have no config schema, got %d fields", len(s.ConfigSchema))
	}

	entry := curatedToEntry(*s)
	if entry.ConfigSchema != "{}" {
		t.Errorf("no-config entry should have ConfigSchema='{}', got %q", entry.ConfigSchema)
	}
}

func TestSeedNoConfigNotNull(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()
	reg.SeedFromCurated(ctx)

	entry, err := s.GetMCPEntry(ctx, "microsoft-playwright-mcp")
	if err != nil {
		t.Fatalf("get playwright: %v", err)
	}
	if entry.ConfigSchema == "null" || entry.ConfigSchema == "" {
		t.Errorf("playwright config_schema = %q, want '{}'", entry.ConfigSchema)
	}
}

func TestBuildServerConfig_Stdio(t *testing.T) {
	reg, _ := newTestRegistry(t)

	entry := &store.MCPRegistryEntry{
		ID:         "tavily-ai-tavily-mcp",
		Connection: `{"type":"stdio","command":"npx","args":["-y","test"]}`,
	}

	cfg := reg.buildServerConfig("tavily-ai-tavily-mcp", entry, map[string]string{
		"API_KEY": "test-key",
	})

	if cfg.Transport != "stdio" {
		t.Errorf("transport = %s, want stdio", cfg.Transport)
	}
	if cfg.Command != "npx" {
		t.Errorf("command = %s, want npx", cfg.Command)
	}
	if cfg.Env["API_KEY"] != "test-key" {
		t.Errorf("env API_KEY = %s, want test-key", cfg.Env["API_KEY"])
	}
	if cfg.Trust != "untrusted" {
		t.Errorf("trust = %s, want untrusted", cfg.Trust)
	}
	if cfg.ToolPrefix != "tavily-ai-tavily-mcp_" {
		t.Errorf("prefix = %s, want tavily-ai-tavily-mcp_", cfg.ToolPrefix)
	}
}

func TestBuildServerConfig_HTTP(t *testing.T) {
	reg, _ := newTestRegistry(t)

	entry := &store.MCPRegistryEntry{
		ID:         "upstash-context7",
		Connection: `{"type":"http","url":"https://mcp.grep.app/"}`,
	}

	cfg := reg.buildServerConfig("upstash-context7", entry, nil)
	if cfg.Transport != "http" {
		t.Errorf("transport = %s, want http", cfg.Transport)
	}
	if cfg.URL != "https://mcp.grep.app/" {
		t.Errorf("url = %s", cfg.URL)
	}
}

func TestBuildServerConfig_HTTPWithAuth(t *testing.T) {
	reg, _ := newTestRegistry(t)

	entry := &store.MCPRegistryEntry{
		ID:         "grep",
		Connection: `{"type":"http","url":"https://mcp.grep.app/sse"}`,
	}

	cfg := reg.buildServerConfig("grep", entry, map[string]string{
		"GREP_API_TOKEN": "grp_xxx",
	})

	if cfg.Headers["Authorization"] != "Bearer grp_xxx" {
		t.Errorf("Authorization header = %q, want Bearer grp_xxx", cfg.Headers["Authorization"])
	}
}

func TestGetInstalledForAgent(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	reg.SeedFromCurated(ctx)

	// Set two MCPs as connected.
	s.SetMCPStatus(ctx, "tavily-ai-tavily-mcp", "connected", "")
	s.SetMCPStatus(ctx, "mendableai-firecrawl-mcp-server", "connected", "")

	// Assign firecrawl only to "researcher".
	s.SetMCPAgents(ctx, "mendableai-firecrawl-mcp-server", []string{"researcher"})

	// Default agent: should get tavily (no assignment = all agents) but not firecrawl.
	ids, _ := reg.GetInstalledForAgent(ctx, "default")
	if !containsStr(ids, "tavily-ai-tavily-mcp") {
		t.Error("default agent should see tavily (no agent restriction)")
	}
	if containsStr(ids, "mendableai-firecrawl-mcp-server") {
		t.Error("default agent should not see firecrawl (assigned to researcher only)")
	}

	// Researcher: should get both.
	ids, _ = reg.GetInstalledForAgent(ctx, "researcher")
	if !containsStr(ids, "tavily-ai-tavily-mcp") {
		t.Error("researcher should see tavily")
	}
	if !containsStr(ids, "mendableai-firecrawl-mcp-server") {
		t.Error("researcher should see firecrawl")
	}
}

func TestDisableAndEnable(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	reg.SeedFromCurated(ctx)
	s.SetMCPStatus(ctx, "microsoft-playwright-mcp", "connected", "")

	// Disable.
	reg.Disable(ctx, "microsoft-playwright-mcp")
	entry, _ := s.GetMCPEntry(ctx, "microsoft-playwright-mcp")
	if entry.Status != "disabled" {
		t.Errorf("status = %q, want disabled", entry.Status)
	}
	if !entry.Installed {
		t.Error("should still be installed")
	}
}

func TestRemove(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	reg.SeedFromCurated(ctx)
	s.SetMCPStatus(ctx, "microsoft-playwright-mcp", "connected", "")
	s.SetMCPAgents(ctx, "microsoft-playwright-mcp", []string{"default"})

	reg.Remove(ctx, "microsoft-playwright-mcp")
	entry, _ := s.GetMCPEntry(ctx, "microsoft-playwright-mcp")
	if entry.Status != "available" {
		t.Errorf("status = %q, want available", entry.Status)
	}
	if entry.Installed {
		t.Error("should not be installed")
	}
	if len(entry.AgentIDs) != 0 {
		t.Errorf("agent_ids should be cleared, got %v", entry.AgentIDs)
	}
}

func TestInstallSameIDGuard(t *testing.T) {
	reg, s := newTestRegistry(t)
	ctx := context.Background()

	reg.SeedFromCurated(ctx)

	// Simulate installing state.
	s.SetMCPStatus(ctx, "microsoft-playwright-mcp", "installing", "")

	// Attempt install should fail.
	err := reg.Install(ctx, "microsoft-playwright-mcp", nil)
	if err == nil {
		t.Error("should reject install when already installing")
	}
	if !strings.Contains(err.Error(), "already in progress") {
		t.Errorf("error should mention 'already in progress', got: %s", err.Error())
	}
}
