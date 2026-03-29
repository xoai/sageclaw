package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestProfileGroups(t *testing.T) {
	// Full profile returns nil (all tools).
	groups := ProfileGroups(ProfileFull)
	if len(groups) != 0 {
		t.Errorf("full profile should return empty map, got %d groups", len(groups))
	}

	// Coding profile includes expected groups.
	groups = ProfileGroups(ProfileCoding)
	for _, g := range []string{GroupFS, GroupRuntime, GroupWeb, GroupMemory} {
		if !groups[g] {
			t.Errorf("coding profile should include %s", g)
		}
	}
	if groups[GroupTeam] {
		t.Error("coding profile should not include team")
	}

	// Messaging profile.
	groups = ProfileGroups(ProfileMessaging)
	if !groups[GroupWeb] || !groups[GroupMemory] || !groups[GroupTeam] {
		t.Error("messaging profile should include web, memory, team")
	}
	if groups[GroupFS] || groups[GroupRuntime] {
		t.Error("messaging profile should not include fs or runtime")
	}

	// Minimal profile.
	groups = ProfileGroups(ProfileMinimal)
	if len(groups) != 0 {
		t.Errorf("minimal profile should return empty map, got %d groups", len(groups))
	}

	// Unknown profile returns nil.
	groups = ProfileGroups("nonexistent")
	if groups != nil {
		t.Error("unknown profile should return nil")
	}
}

func TestValidProfile(t *testing.T) {
	for _, p := range AllProfiles() {
		if !ValidProfile(p) {
			t.Errorf("%s should be valid", p)
		}
	}
	if ValidProfile("nonexistent") {
		t.Error("nonexistent should be invalid")
	}
}

func TestListForAgent_FullProfile(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read file", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("memory_search", "Search", nil, GroupMemory, RiskSafe, "builtin", noop)

	// Full profile = all tools.
	tools := reg.ListForAgent(ProfileFull, nil, nil)
	if len(tools) != 3 {
		t.Errorf("full profile should return 3 tools, got %d", len(tools))
	}

	// Empty profile defaults to full.
	tools = reg.ListForAgent("", nil, nil)
	if len(tools) != 3 {
		t.Errorf("empty profile should default to full, got %d", len(tools))
	}
}

func TestListForAgent_AllProfilesShowAllTools(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("team_send", "Send", nil, GroupTeam, RiskModerate, "builtin", noop)

	// All profiles show all tools — profile controls consent, not visibility.
	for _, profile := range []string{ProfileFull, ProfileCoding, ProfileMessaging, ProfileReadonly, ProfileMinimal} {
		tools := reg.ListForAgent(profile, nil, nil)
		if len(tools) != 3 {
			t.Errorf("%s profile should return 3 tools (all visible), got %d", profile, len(tools))
		}
	}
}

func TestListForAgent_DenyGroup(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Deny runtime group.
	tools := reg.ListForAgent(ProfileFull, []string{"group:runtime"}, nil)
	if len(tools) != 1 {
		t.Errorf("should have 1 tool after denying runtime, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("remaining tool should be read_file, got %s", tools[0].Name)
	}
}

func TestListForAgent_DenyGroupAndTool(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("write_file", "Write", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Deny entire fs group.
	tools := reg.ListForAgent(ProfileFull, []string{"group:fs"}, nil)
	if len(tools) != 1 {
		t.Errorf("expected 1 tool (exec only), got %d", len(tools))
	}
	if tools[0].Name != "execute_command" {
		t.Errorf("remaining tool should be execute_command, got %s", tools[0].Name)
	}
}

func TestListForAgent_DenySingleTool(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("write_file", "Write", nil, GroupFS, RiskModerate, "builtin", noop)

	// Deny single tool by name.
	tools := reg.ListForAgent(ProfileFull, []string{"write_file"}, nil)
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("remaining tool should be read_file, got %s", tools[0].Name)
	}
}

func TestListForAgent_MinimalProfileShowsAllTools(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Minimal profile shows all tools — consent handles access control.
	tools := reg.ListForAgent(ProfileMinimal, nil, nil)
	if len(tools) != 2 {
		t.Errorf("minimal profile should show all tools (consent controls access), got %d", len(tools))
	}
}

func TestAlwaysConsentGroups(t *testing.T) {
	// Runtime, MCP, and orchestration must always require consent.
	for _, g := range []string{GroupRuntime, GroupMCP, GroupOrchestration} {
		if !AlwaysConsentGroups[g] {
			t.Errorf("%s should be in AlwaysConsentGroups", g)
		}
	}
	// Other groups should not be in the always-consent set.
	for _, g := range []string{GroupFS, GroupWeb, GroupMemory, GroupKnowledge, GroupTeam, GroupCron, GroupAudit} {
		if AlwaysConsentGroups[g] {
			t.Errorf("%s should NOT be in AlwaysConsentGroups", g)
		}
	}
}

func TestIsInProfile(t *testing.T) {
	// Full profile allows everything.
	if !IsInProfile(ProfileFull, GroupFS) {
		t.Error("full profile should allow fs")
	}
	if !IsInProfile(ProfileFull, GroupTeam) {
		t.Error("full profile should allow team")
	}

	// Empty profile treated as full.
	if !IsInProfile("", GroupRuntime) {
		t.Error("empty profile should allow runtime")
	}

	// Unknown profile treated as full.
	if !IsInProfile("nonexistent", GroupFS) {
		t.Error("unknown profile should allow fs (treated as full)")
	}

	// Coding profile allows fs but not team.
	if !IsInProfile(ProfileCoding, GroupFS) {
		t.Error("coding profile should allow fs")
	}
	if IsInProfile(ProfileCoding, GroupTeam) {
		t.Error("coding profile should not allow team")
	}

	// Messaging profile allows team but not fs.
	if !IsInProfile(ProfileMessaging, GroupTeam) {
		t.Error("messaging profile should allow team")
	}
	if IsInProfile(ProfileMessaging, GroupFS) {
		t.Error("messaging profile should not allow fs")
	}

	// Minimal profile allows nothing.
	if IsInProfile(ProfileMinimal, GroupFS) {
		t.Error("minimal profile should not allow fs")
	}
	if IsInProfile(ProfileMinimal, GroupMemory) {
		t.Error("minimal profile should not allow memory")
	}
}

func TestUnregisterBySource(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("builtin_tool", "Builtin", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("brave_web_search", "Search", nil, GroupMCP, RiskSensitive, "mcp:brave-search", noop)
	reg.RegisterWithGroup("brave_news_search", "News", nil, GroupMCP, RiskSensitive, "mcp:brave-search", noop)
	reg.RegisterWithGroup("github_issues", "Issues", nil, GroupMCP, RiskSensitive, "mcp:github", noop)

	reg.UnregisterBySource("mcp:brave-search")

	tools := reg.List()
	if len(tools) != 2 {
		t.Errorf("expected 2 tools after unregister, got %d", len(tools))
	}

	// builtin_tool and github_issues should remain.
	if _, _, ok := reg.Get("builtin_tool"); !ok {
		t.Error("builtin_tool should still exist")
	}
	if _, _, ok := reg.Get("github_issues"); !ok {
		t.Error("github_issues should still exist")
	}
	if _, _, ok := reg.Get("brave_web_search"); ok {
		t.Error("brave_web_search should be removed")
	}
}

func TestListForAgent_AllowedMCPServers(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("brave_search", "Search", nil, GroupMCP, RiskSensitive, "mcp:brave-search", noop)
	reg.RegisterWithGroup("github_issues", "Issues", nil, GroupMCP, RiskSensitive, "mcp:github", noop)

	// nil = no filtering, all MCP tools pass.
	tools := reg.ListForAgent(ProfileFull, nil, nil)
	if len(tools) != 3 {
		t.Errorf("nil allowedMCPServers should return all 3 tools, got %d", len(tools))
	}

	// Only allow brave-search MCP.
	tools = reg.ListForAgent(ProfileFull, nil, []string{"brave-search"})
	if len(tools) != 2 {
		t.Errorf("expected 2 tools (builtin + brave), got %d", len(tools))
	}
	names := map[string]bool{}
	for _, tool := range tools {
		names[tool.Name] = true
	}
	if !names["read_file"] {
		t.Error("builtin read_file should be present")
	}
	if !names["brave_search"] {
		t.Error("brave_search should be present")
	}
	if names["github_issues"] {
		t.Error("github_issues should be filtered out")
	}

	// Empty slice = no MCP tools allowed.
	tools = reg.ListForAgent(ProfileFull, nil, []string{})
	if len(tools) != 1 {
		t.Errorf("empty allowedMCPServers should return only builtin, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("only read_file should remain, got %s", tools[0].Name)
	}
}

func TestGetMeta(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("test_tool", "Test", nil, GroupFS, RiskModerate, "builtin", noop)

	group, risk, source, ok := reg.GetMeta("test_tool")
	if !ok {
		t.Fatal("test_tool should exist")
	}
	if group != GroupFS {
		t.Errorf("group should be fs, got %s", group)
	}
	if risk != RiskModerate {
		t.Errorf("risk should be moderate, got %s", risk)
	}
	if source != "builtin" {
		t.Errorf("source should be builtin, got %s", source)
	}

	// Non-existent tool.
	_, _, _, ok = reg.GetMeta("nonexistent")
	if ok {
		t.Error("nonexistent should not be found")
	}
}
