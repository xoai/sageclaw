package deferred

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func makeTool(name, desc string) canonical.ToolDef {
	return canonical.ToolDef{
		Name:        name,
		Description: desc,
		InputSchema: json.RawMessage(`{"type":"object"}`),
	}
}

func TestFilterDeferred_SplitsCorrectly(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("read_file", "Read a file from disk"),
		makeTool("web_fetch", "Fetch a URL and return content"),
		makeTool("glob", "Find files matching a pattern"),
		makeTool("memory_search", "Search memory for relevant context"),
	}

	loaded, stubs := FilterDeferred(tools, nil)

	// read_file and glob are always-loaded.
	loadedNames := map[string]bool{}
	for _, l := range loaded {
		loadedNames[l.Name] = true
	}
	if !loadedNames["read_file"] {
		t.Error("read_file should be always-loaded")
	}
	if !loadedNames["glob"] {
		t.Error("glob should be always-loaded")
	}

	// web_fetch and memory_search should be deferred.
	stubNames := map[string]bool{}
	for _, s := range stubs {
		stubNames[s.Name] = true
	}
	if !stubNames["web_fetch"] {
		t.Error("web_fetch should be deferred")
	}
	if !stubNames["memory_search"] {
		t.Error("memory_search should be deferred")
	}
}

func TestFilterDeferred_StubsHaveNoSchema(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("web_fetch", "Fetch a URL"),
	}

	_, stubs := FilterDeferred(tools, nil)

	if len(stubs) != 1 {
		t.Fatalf("expected 1 stub, got %d", len(stubs))
	}
	// ToolDefStub has no InputSchema field — just name + desc.
	if stubs[0].Name != "web_fetch" {
		t.Error("wrong stub name")
	}
}

func TestFilterDeferred_CustomAlwaysLoad(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("custom_tool", "A custom tool"),
		makeTool("read_file", "Read a file"),
	}

	custom := map[string]bool{"custom_tool": true}
	loaded, stubs := FilterDeferred(tools, custom)

	if len(loaded) != 1 || loaded[0].Name != "custom_tool" {
		t.Error("custom alwaysLoad not respected")
	}
	if len(stubs) != 1 || stubs[0].Name != "read_file" {
		t.Error("non-custom tool should be deferred")
	}
}

func TestFilterDeferred_AlwaysLoadedToolsNeverDeferred(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("tool_search", "Search for tools"),
		makeTool("datetime", "Get current date/time"),
		makeTool("delegate", "Delegate to another agent"),
	}

	loaded, stubs := FilterDeferred(tools, nil)

	if len(stubs) != 0 {
		t.Errorf("expected 0 stubs for always-loaded tools, got %d", len(stubs))
	}
	if len(loaded) != 3 {
		t.Errorf("expected 3 loaded, got %d", len(loaded))
	}
}

func TestSearchTools_ReturnsMatches(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("web_fetch", "Fetch a URL and return page content"),
		makeTool("web_search", "Search the web for information"),
		makeTool("read_file", "Read a file from the filesystem"),
		makeTool("memory_search", "Search memory store"),
	}

	results := SearchTools(tools, "web", 5)
	if len(results) != 2 {
		t.Fatalf("expected 2 web results, got %d", len(results))
	}
	// Both web tools should match.
	names := map[string]bool{}
	for _, r := range results {
		names[r.Name] = true
	}
	if !names["web_fetch"] || !names["web_search"] {
		t.Error("expected both web tools in results")
	}
}

func TestSearchTools_ExactNameScoresHigher(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("grep", "Search file contents with regex"),
		makeTool("grep_memory", "Search memory with grep-like syntax"),
	}

	results := SearchTools(tools, "grep", 5)
	if len(results) < 1 {
		t.Fatal("expected at least 1 result")
	}
	// Exact match should be first.
	if results[0].Name != "grep" {
		t.Errorf("exact match should rank first, got %s", results[0].Name)
	}
}

func TestSearchTools_MaxResults(t *testing.T) {
	var tools []canonical.ToolDef
	for i := 0; i < 20; i++ {
		tools = append(tools, makeTool("tool_"+string(rune('a'+i)), "A tool for testing"))
	}

	results := SearchTools(tools, "tool", 3)
	if len(results) != 3 {
		t.Errorf("expected max 3 results, got %d", len(results))
	}
}

func TestSearchTools_NoMatch(t *testing.T) {
	tools := []canonical.ToolDef{
		makeTool("read_file", "Read a file"),
	}

	results := SearchTools(tools, "zzzzz", 5)
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching query, got %d", len(results))
	}
}

func TestSearchTools_FullSchemaReturned(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
	tools := []canonical.ToolDef{
		{Name: "read_file", Description: "Read a file", InputSchema: schema},
	}

	results := SearchTools(tools, "read", 5)
	if len(results) != 1 {
		t.Fatal("expected 1 result")
	}
	if string(results[0].InputSchema) != string(schema) {
		t.Error("full schema not returned")
	}
}

func TestStubsPromptSection(t *testing.T) {
	stubs := []ToolDefStub{
		{Name: "web_fetch", Description: "Fetch a URL"},
		{Name: "memory_search", Description: "Search memory"},
	}

	section := StubsPromptSection(stubs)
	if !strings.Contains(section, "web_fetch") {
		t.Error("section should contain web_fetch")
	}
	if !strings.Contains(section, "tool_search") {
		// The section header should mention tool_search.
		if !strings.Contains(section, "Additional tools") {
			t.Error("section should have header")
		}
	}
}

func TestStubsPromptSection_Empty(t *testing.T) {
	if StubsPromptSection(nil) != "" {
		t.Error("empty stubs should return empty string")
	}
}

func TestFilterDeferred_LongDescriptionTruncated(t *testing.T) {
	longDesc := strings.Repeat("A very long description. ", 10) // ~240 chars
	tools := []canonical.ToolDef{
		makeTool("custom_tool", longDesc),
	}

	_, stubs := FilterDeferred(tools, nil)
	if len(stubs) != 1 {
		t.Fatal("expected 1 stub")
	}
	if len(stubs[0].Description) > 130 {
		t.Errorf("stub description should be truncated, got %d chars", len(stubs[0].Description))
	}
}
