// Package agentcfg provides file-based agent configuration.
//
// Each agent is a folder containing structured files:
//
//	agents/
//	  default/
//	    identity.yaml      — name, role, model, metadata
//	    soul.md            — personality, voice, values
//	    behavior.md        — rules, constraints, decision frameworks
//	    tools.yaml         — enabled tools + per-tool config
//	    memory.yaml        — memory scope, retention, search prefs
//	    heartbeat.yaml     — proactive cron schedules
//	    channels.yaml      — which channels + per-channel overrides
//
// Files on disk are the source of truth. The DB caches for runtime.
package agentcfg

// AgentConfig is the full configuration for one agent, loaded from a folder.
type AgentConfig struct {
	// ID is the folder name (e.g. "default", "researcher").
	ID string `json:"id" yaml:"-"`

	// Source indicates where the config came from: "file" or "db".
	Source string `json:"source" yaml:"-"`

	// Dir is the absolute path to the agent's config folder.
	Dir string `json:"-" yaml:"-"`

	Identity  Identity        `json:"identity" yaml:"identity"`
	Soul      string          `json:"soul" yaml:"-"`      // Raw markdown content of soul.md
	Behavior  string          `json:"behavior" yaml:"-"`  // Raw markdown content of behavior.md
	Bootstrap string          `json:"bootstrap" yaml:"-"` // Raw markdown content of bootstrap.md (auto-deleted after first run)
	Tools     ToolsConfig     `json:"tools" yaml:"tools"`
	Memory    MemoryConfig    `json:"memory" yaml:"memory"`
	Heartbeat HeartbeatConfig `json:"heartbeat" yaml:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels" yaml:"channels"`
}

// Identity defines who the agent is (identity.yaml).
type Identity struct {
	Name          string   `json:"name" yaml:"name"`
	Role          string   `json:"role" yaml:"role"`
	Model         string   `json:"model" yaml:"model"`                     // Tier (strong/fast/local) or model ID
	MaxTokens     int      `json:"max_tokens" yaml:"max_tokens"`
	MaxIterations int      `json:"max_iterations" yaml:"max_iterations"`
	Avatar        string   `json:"avatar" yaml:"avatar"`                   // Emoji or URL
	Tags          []string `json:"tags" yaml:"tags"`
	Status        string   `json:"status" yaml:"status"`                   // "active" (default), "inactive"
}

// ToolsConfig defines which tools the agent can use (tools.yaml).
type ToolsConfig struct {
	// Enabled lists tool names. Empty = all tools available (backward compat).
	Enabled []string `json:"enabled" yaml:"enabled"`

	// Profile sets the base tool set: full, coding, messaging, readonly, minimal.
	// Default: "" (treated as "full").
	Profile string `json:"profile,omitempty" yaml:"profile"`

	// Deny removes tools or groups. Use "group:runtime" to deny an entire group.
	Deny []string `json:"deny,omitempty" yaml:"deny"`

	// AlsoAllow adds tools back after deny. Use "group:fs" to re-allow a group.
	AlsoAllow []string `json:"also_allow,omitempty" yaml:"also_allow"`

	// ShellDenyGroups controls which deny pattern groups are active for exec.
	// All groups are enabled by default. Set a group to false to disable it.
	ShellDenyGroups map[string]bool `json:"shell_deny_groups,omitempty" yaml:"shell_deny_groups"`

	// MCPServers defines external MCP server connections.
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty" yaml:"mcp_servers"`

	// Config holds per-tool configuration overrides.
	Config map[string]map[string]any `json:"config,omitempty" yaml:"config"`
}

// MCPServerConfig defines how to connect to an external MCP server.
type MCPServerConfig struct {
	Transport  string            `json:"transport" yaml:"transport"`               // stdio, sse, streamable-http
	Command    string            `json:"command,omitempty" yaml:"command"`          // stdio only
	Args       []string          `json:"args,omitempty" yaml:"args"`               // stdio only
	Env        map[string]string `json:"env,omitempty" yaml:"env"`                 // stdio only
	URL        string            `json:"url,omitempty" yaml:"url"`                 // sse/http only
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers"`         // sse/http only
	ToolPrefix string            `json:"tool_prefix,omitempty" yaml:"tool_prefix"` // prefix for tool names
	TimeoutSec int               `json:"timeout_sec,omitempty" yaml:"timeout_sec"` // per-call timeout (default 60)
	Trust      string            `json:"trust,omitempty" yaml:"trust"`             // trusted or untrusted (default)
	Enabled    *bool             `json:"enabled,omitempty" yaml:"enabled"`         // default true
}

// MemoryConfig defines memory behavior (memory.yaml).
type MemoryConfig struct {
	Scope         string   `json:"scope" yaml:"scope"`                 // "project" or "global"
	AutoStore     bool     `json:"auto_store" yaml:"auto_store"`
	RetentionDays int      `json:"retention_days" yaml:"retention_days"` // 0 = forever
	SearchLimit   int      `json:"search_limit" yaml:"search_limit"`
	TagsBoost     []string `json:"tags_boost" yaml:"tags_boost"`
}

// HeartbeatConfig defines proactive schedules (heartbeat.yaml).
type HeartbeatConfig struct {
	Schedules []HeartbeatSchedule `json:"schedules" yaml:"schedules"`
}

// HeartbeatSchedule is a single proactive cron schedule.
type HeartbeatSchedule struct {
	Name    string `json:"name" yaml:"name"`
	Cron    string `json:"cron" yaml:"cron"`
	Prompt  string `json:"prompt" yaml:"prompt"`
	Channel string `json:"channel" yaml:"channel"`
}

// ChannelsConfig defines which channels this agent serves (channels.yaml).
type ChannelsConfig struct {
	// Serve lists channel names. Empty = serve all channels.
	Serve []string `json:"serve" yaml:"serve"`

	// Overrides holds per-channel config overrides.
	Overrides map[string]ChannelOverride `json:"overrides,omitempty" yaml:"overrides"`
}

// ChannelOverride is per-channel configuration.
type ChannelOverride struct {
	MaxTokens int `json:"max_tokens,omitempty" yaml:"max_tokens"`
}

// Defaults returns an AgentConfig with sensible defaults.
func Defaults(id string) AgentConfig {
	return AgentConfig{
		ID:     id,
		Source: "file",
		Identity: Identity{
			Name:          id,
			Role:          "AI assistant",
			Model:         "strong",
			MaxTokens:     8192,
			MaxIterations: 25,
		},
		Memory: MemoryConfig{
			Scope:       "project",
			AutoStore:   true,
			SearchLimit: 10,
		},
	}
}
