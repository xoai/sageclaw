package toolstatus

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ToolDisplayEntry describes how to present a tool call to the user.
type ToolDisplayEntry struct {
	Emoji     string // e.g. "🔍"
	Verb      string // e.g. "Searching"
	DetailKey string // JSON arg to extract, e.g. "query"
	Category  string // "web", "coding", "tool" — for reaction phase
}

// ToolDisplay is the resolved display for a specific tool call.
type ToolDisplay struct {
	Emoji    string
	Verb     string
	Detail   string
	Category string
}

// ToolDisplayMap holds the static mapping from tool name to display info.
type ToolDisplayMap struct {
	entries map[string]ToolDisplayEntry
}

// DefaultDisplayMap returns the display map with all known tool entries.
func DefaultDisplayMap() *ToolDisplayMap {
	return &ToolDisplayMap{
		entries: map[string]ToolDisplayEntry{
			// Web tools
			"web_search": {Emoji: "🔍", Verb: "Searching", DetailKey: "query", Category: "web"},
			"web_fetch":  {Emoji: "🌐", Verb: "Fetching", DetailKey: "url", Category: "web"},
			"browser":    {Emoji: "🌐", Verb: "Browsing", DetailKey: "url", Category: "web"},

			// Team / orchestration tools
			"team_tasks:create": {Emoji: "📋", Verb: "Delegating", DetailKey: "assignee", Category: "tool"},
			"team_tasks:list":   {Emoji: "📋", Verb: "Checking tasks", DetailKey: "", Category: "tool"},
			"team_tasks:search": {Emoji: "📋", Verb: "Searching tasks", DetailKey: "query", Category: "tool"},
			"team_create_task":  {Emoji: "📋", Verb: "Creating task", DetailKey: "title", Category: "tool"},
			"team_assign_task":  {Emoji: "📋", Verb: "Assigning task", DetailKey: "assignee", Category: "tool"},
			"team_claim_task":   {Emoji: "📋", Verb: "Claiming task", DetailKey: "", Category: "tool"},
			"team_complete_task": {Emoji: "✅", Verb: "Completing task", DetailKey: "", Category: "tool"},
			"team_list_tasks":   {Emoji: "📋", Verb: "Listing tasks", DetailKey: "", Category: "tool"},
			"team_send":         {Emoji: "✉️", Verb: "Messaging team", DetailKey: "to", Category: "tool"},
			"team_inbox":        {Emoji: "📥", Verb: "Checking inbox", DetailKey: "", Category: "tool"},
			"handoff":           {Emoji: "🔄", Verb: "Handing off", DetailKey: "target_agent", Category: "tool"},
			"spawn":             {Emoji: "🚀", Verb: "Starting sub-agent", DetailKey: "", Category: "tool"},
			"delegate":          {Emoji: "📋", Verb: "Delegating", DetailKey: "agent", Category: "tool"},
			"delegation_status": {Emoji: "📋", Verb: "Checking delegation", DetailKey: "", Category: "tool"},
			"evaluate":          {Emoji: "🔄", Verb: "Evaluating", DetailKey: "", Category: "tool"},

			// Memory / knowledge tools
			"memory_search":          {Emoji: "🧠", Verb: "Searching memory", DetailKey: "query", Category: "tool"},
			"memory_get":             {Emoji: "🧠", Verb: "Reading memory", DetailKey: "", Category: "tool"},
			"memory_link":            {Emoji: "🧠", Verb: "Linking memory", DetailKey: "", Category: "tool"},
			"memory_graph":           {Emoji: "🧠", Verb: "Traversing graph", DetailKey: "", Category: "tool"},
			"knowledge_graph_search": {Emoji: "🧠", Verb: "Querying knowledge", DetailKey: "query", Category: "tool"},

			// File system tools
			"read_file":       {Emoji: "📖", Verb: "Reading", DetailKey: "path", Category: "coding"},
			"write_file":      {Emoji: "✏️", Verb: "Writing", DetailKey: "path", Category: "coding"},
			"edit":            {Emoji: "✏️", Verb: "Editing", DetailKey: "path", Category: "coding"},
			"list_directory":  {Emoji: "📂", Verb: "Listing", DetailKey: "path", Category: "coding"},
			"execute_command": {Emoji: "⚡", Verb: "Running command", DetailKey: "command", Category: "coding"},

			// Media tools
			"read_image":    {Emoji: "👁", Verb: "Processing image", DetailKey: "", Category: "tool"},
			"read_document": {Emoji: "📄", Verb: "Reading document", DetailKey: "", Category: "tool"},
			"create_image":  {Emoji: "🎨", Verb: "Creating image", DetailKey: "prompt", Category: "tool"},

			// Session tools
			"sessions_list":    {Emoji: "📋", Verb: "Listing sessions", DetailKey: "", Category: "tool"},
			"session_status":   {Emoji: "📋", Verb: "Checking session", DetailKey: "", Category: "tool"},
			"sessions_history": {Emoji: "📋", Verb: "Reading history", DetailKey: "", Category: "tool"},
			"sessions_send":    {Emoji: "📋", Verb: "Sending to session", DetailKey: "", Category: "tool"},

			// Messaging
			"message": {Emoji: "✉️", Verb: "Sending message", DetailKey: "to", Category: "tool"},

			// Scheduling
			"cron_create": {Emoji: "⏰", Verb: "Creating schedule", DetailKey: "", Category: "tool"},

			// Audit
			"audit_search": {Emoji: "🔍", Verb: "Searching audit log", DetailKey: "query", Category: "tool"},
			"audit_stats":  {Emoji: "📊", Verb: "Getting audit stats", DetailKey: "", Category: "tool"},

			// Utility
			"datetime":   {Emoji: "🕐", Verb: "Checking time", DetailKey: "", Category: "tool"},
			"load_skill": {Emoji: "🧩", Verb: "Loading skill", DetailKey: "skill", Category: "tool"},
			"plan":       {Emoji: "📝", Verb: "Planning", DetailKey: "", Category: "tool"},

			// Workflow progress (synthetic events from WorkflowRelay)
			"_wf_delegating":     {Emoji: "📤", Verb: "Delegating to team", DetailKey: "count", Category: "tool"},
			"_wf_task_started":   {Emoji: "📋", Verb: "Task started", DetailKey: "title", Category: "tool"},
			"_wf_task_completed": {Emoji: "✅", Verb: "Task done", DetailKey: "title", Category: "tool"},
			"_wf_task_failed":    {Emoji: "❌", Verb: "Task failed", DetailKey: "title", Category: "tool"},
		},
	}
}

const maxDetailLen = 60

// ResolveDisplay resolves the display for a tool call.
func (m *ToolDisplayMap) ResolveDisplay(toolName string, input json.RawMessage) ToolDisplay {
	// Parse input for sub-action disambiguation and detail extraction.
	var args map[string]any
	if len(input) > 0 {
		_ = json.Unmarshal(input, &args) // best-effort; args stays nil on failure
	}

	// Member-prefixed tool calls from WorkflowRelay: "member:{displayName}:{realTool}"
	var memberPrefix string
	if strings.HasPrefix(toolName, "member:") {
		parts := strings.SplitN(toolName, ":", 3)
		if len(parts) == 3 {
			memberPrefix = parts[1] // display name
			toolName = parts[2]     // real tool name
		}
	}

	key := resolveDisplayKey(toolName, args, m.entries)
	entry, ok := m.entries[key]
	if !ok {
		// MCP tools: prefix match
		if strings.HasPrefix(toolName, "mcp_") {
			return ToolDisplay{
				Emoji:    "🔌",
				Verb:     "Using extension",
				Detail:   toolName,
				Category: "tool",
			}
		}
		// Fallback for unknown tools
		return ToolDisplay{
			Emoji:    "🔧",
			Verb:     fmt.Sprintf("Running %s", toolName),
			Category: "tool",
		}
	}

	detail := extractDetail(entry.DetailKey, args)

	// Special case: delegation tools show assignee context.
	if key == "team_tasks:create" && detail != "" {
		title := extractDetail("title", args)
		if title != "" {
			detail = fmt.Sprintf("@%s: %s", detail, title)
			if len(detail) > maxDetailLen {
				detail = detail[:maxDetailLen-3] + "..."
			}
		}
	}

	// Prepend member identity for workflow-forwarded tool calls.
	verb := entry.Verb
	if memberPrefix != "" {
		verb = memberPrefix + ": " + verb
	}

	return ToolDisplay{
		Emoji:    entry.Emoji,
		Verb:     verb,
		Detail:   detail,
		Category: entry.Category,
	}
}

// FormatStatus formats a ToolDisplay into a user-facing status string.
func (d ToolDisplay) FormatStatus(count int) string {
	var sb strings.Builder
	sb.WriteString(d.Emoji)
	sb.WriteString(" ")
	sb.WriteString(d.Verb)
	if d.Detail != "" {
		sb.WriteString(": ")
		sb.WriteString(d.Detail)
	}
	if count > 1 {
		fmt.Fprintf(&sb, " (×%d)", count)
	}
	sb.WriteString("...")
	return sb.String()
}

// resolveDisplayKey checks for sub-action composite keys (e.g., "team_tasks:create").
func resolveDisplayKey(toolName string, args map[string]any, entries map[string]ToolDisplayEntry) string {
	if args != nil {
		if action, ok := args["action"].(string); ok {
			composite := toolName + ":" + action
			if _, found := entries[composite]; found {
				return composite
			}
		}
	}
	return toolName
}

// extractDetail pulls the detail value from args and truncates it.
func extractDetail(key string, args map[string]any) string {
	if key == "" || args == nil {
		return ""
	}
	val, ok := args[key]
	if !ok {
		return ""
	}
	s := fmt.Sprintf("%v", val)
	if len(s) > maxDetailLen {
		s = s[:maxDetailLen-3] + "..."
	}
	return s
}
