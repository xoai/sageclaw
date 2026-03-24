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
	tools := reg.ListForAgent(ProfileFull, nil, nil, nil)
	if len(tools) != 3 {
		t.Errorf("full profile should return 3 tools, got %d", len(tools))
	}

	// Empty profile defaults to full.
	tools = reg.ListForAgent("", nil, nil, nil)
	if len(tools) != 3 {
		t.Errorf("empty profile should default to full, got %d", len(tools))
	}
}

func TestListForAgent_CodingProfile(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)
	reg.RegisterWithGroup("team_send", "Send", nil, GroupTeam, RiskModerate, "builtin", noop)

	// Coding profile excludes team.
	tools := reg.ListForAgent(ProfileCoding, nil, nil, nil)
	if len(tools) != 2 {
		t.Errorf("coding profile should return 2 tools (no team), got %d", len(tools))
	}
	for _, td := range tools {
		if td.Name == "team_send" {
			t.Error("coding profile should not include team_send")
		}
	}
}

func TestListForAgent_DenyGroup(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Deny runtime group.
	tools := reg.ListForAgent(ProfileFull, nil, []string{"group:runtime"}, nil)
	if len(tools) != 1 {
		t.Errorf("should have 1 tool after denying runtime, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("remaining tool should be read_file, got %s", tools[0].Name)
	}
}

func TestListForAgent_DenyAndAlsoAllow(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("write_file", "Write", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Deny fs group, then re-allow read_file.
	tools := reg.ListForAgent(ProfileFull, nil, []string{"group:fs"}, []string{"read_file"})
	if len(tools) != 2 {
		t.Errorf("expected 2 tools (exec + re-allowed read_file), got %d", len(tools))
	}
	names := map[string]bool{}
	for _, td := range tools {
		names[td.Name] = true
	}
	if !names["read_file"] {
		t.Error("read_file should be re-allowed")
	}
	if !names["execute_command"] {
		t.Error("execute_command should still be present")
	}
	if names["write_file"] {
		t.Error("write_file should be denied")
	}
}

func TestListForAgent_DenySingleTool(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("write_file", "Write", nil, GroupFS, RiskModerate, "builtin", noop)

	// Deny single tool by name.
	tools := reg.ListForAgent(ProfileFull, nil, []string{"write_file"}, nil)
	if len(tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "read_file" {
		t.Errorf("remaining tool should be read_file, got %s", tools[0].Name)
	}
}

func TestListForAgent_EnabledIntersection(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("write_file", "Write", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Legacy enabled field — only include specified tools.
	tools := reg.ListForAgent(ProfileFull, []string{"read_file"}, nil, nil)
	if len(tools) != 1 {
		t.Errorf("enabled intersection should return 1 tool, got %d", len(tools))
	}
}

func TestListForAgent_MinimalProfile(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) { return nil, nil }

	reg.RegisterWithGroup("read_file", "Read", nil, GroupFS, RiskModerate, "builtin", noop)
	reg.RegisterWithGroup("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", noop)

	// Minimal profile = no groups, so no tools.
	tools := reg.ListForAgent(ProfileMinimal, nil, nil, nil)
	if len(tools) != 0 {
		t.Errorf("minimal profile should return 0 tools, got %d", len(tools))
	}

	// But alsoAllow can add specific tools back.
	tools = reg.ListForAgent(ProfileMinimal, nil, nil, []string{"read_file"})
	if len(tools) != 1 {
		t.Errorf("minimal + alsoAllow should return 1 tool, got %d", len(tools))
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
