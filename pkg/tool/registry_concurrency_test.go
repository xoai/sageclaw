package tool

import (
	"testing"
)

func TestIsConcurrencySafe(t *testing.T) {
	reg := NewRegistry()
	noop := func(ctx interface{ Deadline() (interface{}, bool) }, input []byte) (interface{}, error) { return nil, nil }
	_ = noop

	// Register a concurrent-safe tool via RegisterFull.
	reg.RegisterFull("read_file", "Read file", nil, GroupFS, RiskModerate, "builtin", true, nil)
	// Register an exclusive tool via RegisterWithGroup (defaults false).
	reg.RegisterWithGroup("write_file", "Write file", nil, GroupFS, RiskModerate, "builtin", nil)
	// Register an exclusive tool via RegisterFull with false.
	reg.RegisterFull("execute_command", "Exec", nil, GroupRuntime, RiskSensitive, "builtin", false, nil)

	tests := []struct {
		name     string
		expected bool
	}{
		{"read_file", true},
		{"write_file", false},
		{"execute_command", false},
		{"unknown_tool", false},     // Unknown defaults to false.
		{"mcp:some_tool", false},    // Non-existent MCP tool.
	}

	for _, tt := range tests {
		got := reg.IsConcurrencySafe(tt.name)
		if got != tt.expected {
			t.Errorf("IsConcurrencySafe(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestRegisterFullOverridesRegisterWithGroup(t *testing.T) {
	reg := NewRegistry()

	// Register first as not concurrent-safe.
	reg.RegisterWithGroup("tool_a", "A", nil, GroupCore, RiskSafe, "builtin", nil)
	if reg.IsConcurrencySafe("tool_a") {
		t.Error("tool_a should not be concurrent-safe after RegisterWithGroup")
	}

	// Re-register as concurrent-safe.
	reg.RegisterFull("tool_a", "A", nil, GroupCore, RiskSafe, "builtin", true, nil)
	if !reg.IsConcurrencySafe("tool_a") {
		t.Error("tool_a should be concurrent-safe after RegisterFull with true")
	}
}

func TestConcurrencyClassificationTable(t *testing.T) {
	// Verify that the spec's classification table is correct for all built-in tools.
	reg := NewRegistry()

	// Concurrent-safe tools (read-only).
	safeTools := []struct {
		name  string
		group string
		risk  string
	}{
		{"read_file", GroupFS, RiskModerate},
		{"list_directory", GroupFS, RiskModerate},
		{"web_fetch", GroupWeb, RiskModerate},
		{"web_search", GroupWeb, RiskModerate},
		{"memory_search", GroupMemory, RiskSafe},
		{"memory_get", GroupMemory, RiskSafe},
		{"datetime", GroupCore, RiskSafe},
		{"tool_search", GroupCore, RiskSafe},
		{"audit_search", GroupAudit, RiskSafe},
		{"audit_stats", GroupAudit, RiskSafe},
	}
	for _, tool := range safeTools {
		reg.RegisterFull(tool.name, "desc", nil, tool.group, tool.risk, "builtin", true, nil)
	}

	// Exclusive tools (mutations/side effects).
	exclusiveTools := []struct {
		name  string
		group string
		risk  string
	}{
		{"write_file", GroupFS, RiskModerate},
		{"edit", GroupFS, RiskModerate},
		{"execute_command", GroupRuntime, RiskSensitive},
		{"delegate", GroupOrchestration, RiskSensitive},
		{"browser", GroupBrowser, RiskModerate},
	}
	for _, tool := range exclusiveTools {
		reg.RegisterWithGroup(tool.name, "desc", nil, tool.group, tool.risk, "builtin", nil)
	}

	// Verify classifications.
	for _, tool := range safeTools {
		if !reg.IsConcurrencySafe(tool.name) {
			t.Errorf("%s should be concurrent-safe", tool.name)
		}
	}
	for _, tool := range exclusiveTools {
		if reg.IsConcurrencySafe(tool.name) {
			t.Errorf("%s should NOT be concurrent-safe", tool.name)
		}
	}
}
