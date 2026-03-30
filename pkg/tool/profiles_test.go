package tool

import (
	"context"
	"encoding/json"
	"strings"
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

	// Minimal profile includes core.
	groups = ProfileGroups(ProfileMinimal)
	if len(groups) != 1 || !groups[GroupCore] {
		t.Errorf("minimal profile should include only core group, got %d groups", len(groups))
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

func TestListForAgent_ProfileFiltering(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("team_send", "Send", nil, GroupTeam, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("datetime", "Time", nil, GroupCore, RiskSafe, "builtin", noop)

	// Full profile sees all tools.
	tools := reg.ListForAgent(ProfileFull, nil, nil)
	if len(tools) != 4 {
		t.Errorf("full profile should return 4 tools, got %d", len(tools))
	}

	// Coding profile sees fs, runtime, core — but not team.
	tools = reg.ListForAgent(ProfileCoding, nil, nil)
	names := toolNames(tools)
	if !names["read_file"] || !names["execute_command"] || !names["datetime"] {
		t.Errorf("coding profile should include fs, runtime, core tools, got %v", names)
	}
	if names["team_send"] {
		t.Error("coding profile should not include team tools")
	}

	// Messaging profile sees team, core — but not fs, runtime.
	tools = reg.ListForAgent(ProfileMessaging, nil, nil)
	names = toolNames(tools)
	if !names["team_send"] || !names["datetime"] {
		t.Errorf("messaging should include team, core, got %v", names)
	}
	if names["read_file"] || names["execute_command"] {
		t.Error("messaging should not include fs or runtime")
	}

	// Minimal profile sees only core.
	tools = reg.ListForAgent(ProfileMinimal, nil, nil)
	names = toolNames(tools)
	if !names["datetime"] {
		t.Error("minimal should include core tools")
	}
	if names["read_file"] || names["execute_command"] || names["team_send"] {
		t.Error("minimal should only include core tools")
	}
}

func toolNames(tools []canonical.ToolDef) map[string]bool {
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t.Name] = true
	}
	return m
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

func TestListForAgent_MinimalProfileOnlyCore(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("datetime", "Time", nil, GroupCore, RiskSafe, "builtin", noop)

	// Minimal profile only sees core tools.
	tools := reg.ListForAgent(ProfileMinimal, nil, nil)
	if len(tools) != 1 {
		t.Errorf("minimal profile should show only core tools, got %d", len(tools))
	}
	if tools[0].Name != "datetime" {
		t.Errorf("expected datetime, got %s", tools[0].Name)
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

	// Minimal profile allows only core.
	if !IsInProfile(ProfileMinimal, GroupCore) {
		t.Error("minimal profile should allow core")
	}
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

func TestSchemaCompression(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to fetch"},"mode":{"type":"string","description":"Output format","default":"markdown"}},"required":["url"]}`)

	compressed := compressSchema(schema)

	var obj map[string]any
	json.Unmarshal(compressed, &obj)

	props := obj["properties"].(map[string]any)
	urlProp := props["url"].(map[string]any)
	modeProp := props["mode"].(map[string]any)

	// Descriptions and defaults should be stripped.
	if _, ok := urlProp["description"]; ok {
		t.Error("description should be stripped from url property")
	}
	if _, ok := modeProp["description"]; ok {
		t.Error("description should be stripped from mode property")
	}
	if _, ok := modeProp["default"]; ok {
		t.Error("default should be stripped from mode property")
	}

	// Type and required should be preserved.
	if urlProp["type"] != "string" {
		t.Error("type should be preserved")
	}
	req := obj["required"].([]any)
	if len(req) != 1 || req[0] != "url" {
		t.Error("required should be preserved")
	}

	// Compressed should be smaller.
	if len(compressed) >= len(schema) {
		t.Errorf("compressed (%d bytes) should be smaller than original (%d bytes)", len(compressed), len(schema))
	}
}

func TestSoftTrimResult(t *testing.T) {
	// Short content — no trimming.
	short := "hello world"
	if got := softTrimResult(short); got != short {
		t.Errorf("short content should not be trimmed")
	}

	// Exactly at threshold — no trimming.
	exact := strings.Repeat("x", softTrimThreshold)
	if got := softTrimResult(exact); got != exact {
		t.Errorf("content at threshold should not be trimmed")
	}

	// Over threshold — should be trimmed.
	big := strings.Repeat("A", 2000) + strings.Repeat("B", 6000) + strings.Repeat("C", 2000)
	result := softTrimResult(big)

	if len(result) >= len(big) {
		t.Errorf("trimmed result (%d) should be smaller than original (%d)", len(result), len(big))
	}
	// Should start with head content.
	if !strings.HasPrefix(result, strings.Repeat("A", 1500)) {
		t.Error("should keep first 1500 chars")
	}
	// Should end with tail content.
	if !strings.HasSuffix(result, strings.Repeat("C", 1500)) {
		t.Error("should keep last 1500 chars")
	}
	// Should contain trim note.
	if !strings.Contains(result, "chars trimmed") {
		t.Error("should contain trim note")
	}
}

func TestExecute_SoftTrimsLargeResult(t *testing.T) {
	reg := NewRegistry()
	bigResult := strings.Repeat("x", 10000)
	reg.RegisterWithGroup("big_tool", "Returns big output", nil, GroupCore, RiskSafe, "builtin",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: bigResult}, nil
		})

	result, err := reg.Execute(context.Background(), "big_tool", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Content) >= len(bigResult) {
		t.Errorf("Execute should soft-trim large results: got %d chars, original %d", len(result.Content), len(bigResult))
	}
	if !strings.Contains(result.Content, "chars trimmed") {
		t.Error("trimmed result should contain trim note")
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
