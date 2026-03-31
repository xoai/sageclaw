package toolstatus

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolveDisplay_KnownTools(t *testing.T) {
	dm := DefaultDisplayMap()
	tests := []struct {
		name     string
		tool     string
		input    string
		wantVerb string
		wantCat  string
	}{
		{"web_search", "web_search", `{"query":"market trends"}`, "Searching", "web"},
		{"web_fetch", "web_fetch", `{"url":"https://example.com"}`, "Fetching", "web"},
		{"read_file", "read_file", `{"path":"/tmp/foo.go"}`, "Reading", "coding"},
		{"write_file", "write_file", `{"path":"/tmp/bar.go"}`, "Writing", "coding"},
		{"execute_command", "execute_command", `{"command":"go test"}`, "Running command", "coding"},
		{"memory_search", "memory_search", `{"query":"test"}`, "Searching memory", "tool"},
		{"handoff", "handoff", `{"target_agent":"researcher"}`, "Handing off", "tool"},
		{"spawn", "spawn", `{}`, "Starting sub-agent", "tool"},
		{"browser", "browser", `{"url":"https://example.com"}`, "Browsing", "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := dm.ResolveDisplay(tt.tool, json.RawMessage(tt.input))
			if d.Verb != tt.wantVerb {
				t.Errorf("verb = %q, want %q", d.Verb, tt.wantVerb)
			}
			if d.Category != tt.wantCat {
				t.Errorf("category = %q, want %q", d.Category, tt.wantCat)
			}
		})
	}
}

func TestResolveDisplay_UnknownTool(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("some_custom_tool", nil)
	if d.Emoji != "🔧" {
		t.Errorf("emoji = %q, want 🔧", d.Emoji)
	}
	if !strings.Contains(d.Verb, "some_custom_tool") {
		t.Errorf("verb = %q, should contain tool name", d.Verb)
	}
}

func TestResolveDisplay_MCPTool(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("mcp_brave_search", nil)
	if d.Emoji != "🔌" {
		t.Errorf("emoji = %q, want 🔌", d.Emoji)
	}
	if d.Verb != "Using extension" {
		t.Errorf("verb = %q, want Using extension", d.Verb)
	}
}

func TestResolveDisplay_SubAction(t *testing.T) {
	dm := DefaultDisplayMap()
	tests := []struct {
		name     string
		input    string
		wantVerb string
	}{
		{"create", `{"action":"create","assignee":"researcher","title":"research task"}`, "Delegating"},
		{"list", `{"action":"list"}`, "Checking tasks"},
		{"search", `{"action":"search","query":"bugs"}`, "Searching tasks"},
		{"unknown_action", `{"action":"unknown_action_xyz"}`, "Running team_tasks"}, // falls back
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := dm.ResolveDisplay("team_tasks", json.RawMessage(tt.input))
			if d.Verb != tt.wantVerb {
				t.Errorf("verb = %q, want %q", d.Verb, tt.wantVerb)
			}
		})
	}
}

func TestResolveDisplay_DetailExtraction(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("web_search", json.RawMessage(`{"query":"market trends 2026"}`))
	if d.Detail != "market trends 2026" {
		t.Errorf("detail = %q, want %q", d.Detail, "market trends 2026")
	}
}

func TestResolveDisplay_DetailTruncation(t *testing.T) {
	dm := DefaultDisplayMap()
	longQuery := strings.Repeat("x", 100)
	d := dm.ResolveDisplay("web_search", json.RawMessage(`{"query":"`+longQuery+`"}`))
	if len(d.Detail) > maxDetailLen {
		t.Errorf("detail length = %d, want <= %d", len(d.Detail), maxDetailLen)
	}
	if !strings.HasSuffix(d.Detail, "...") {
		t.Errorf("truncated detail should end with '...'")
	}
}

func TestResolveDisplay_MalformedJSON(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("web_search", json.RawMessage(`{invalid json}`))
	if d.Detail != "" {
		t.Errorf("detail = %q, want empty on malformed JSON", d.Detail)
	}
	// Should still resolve the tool correctly
	if d.Verb != "Searching" {
		t.Errorf("verb = %q, want Searching", d.Verb)
	}
}

func TestResolveDisplay_NilInput(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("web_search", nil)
	if d.Detail != "" {
		t.Errorf("detail = %q, want empty on nil input", d.Detail)
	}
	if d.Verb != "Searching" {
		t.Errorf("verb = %q, want Searching", d.Verb)
	}
}

func TestResolveDisplay_DelegationSpecialCase(t *testing.T) {
	dm := DefaultDisplayMap()
	d := dm.ResolveDisplay("team_tasks", json.RawMessage(
		`{"action":"create","assignee":"researcher","title":"research market trends"}`,
	))
	if d.Verb != "Delegating" {
		t.Errorf("verb = %q, want Delegating", d.Verb)
	}
	if !strings.Contains(d.Detail, "@researcher") {
		t.Errorf("detail = %q, should contain @researcher", d.Detail)
	}
	if !strings.Contains(d.Detail, "research market trends") {
		t.Errorf("detail = %q, should contain task title", d.Detail)
	}
}

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		name  string
		d     ToolDisplay
		count int
		want  string
	}{
		{
			"simple",
			ToolDisplay{Emoji: "🔍", Verb: "Searching", Detail: "market trends"},
			1,
			"🔍 Searching: market trends...",
		},
		{
			"no_detail",
			ToolDisplay{Emoji: "🚀", Verb: "Starting sub-agent"},
			1,
			"🚀 Starting sub-agent...",
		},
		{
			"repeated",
			ToolDisplay{Emoji: "🔍", Verb: "Searching", Detail: "query"},
			3,
			"🔍 Searching: query (×3)...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.d.FormatStatus(tt.count)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
