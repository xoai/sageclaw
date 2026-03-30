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

	// SkillsDir is the path to the marketplace skills install directory.
	// Set by the caller (main.go) so AssembleSystemPrompt can read SKILL.md files.
	SkillsDir string `json:"-" yaml:"-"`

	Identity  Identity        `json:"identity" yaml:"identity"`
	Soul      string          `json:"soul" yaml:"-"`      // Raw markdown content of soul.md
	Behavior  string          `json:"behavior" yaml:"-"`  // Raw markdown content of behavior.md
	Bootstrap string          `json:"bootstrap" yaml:"-"` // Raw markdown content of bootstrap.md (auto-deleted after first run)
	Tools     ToolsConfig     `json:"tools" yaml:"tools"`
	Memory    MemoryConfig    `json:"memory" yaml:"memory"`
	Heartbeat HeartbeatConfig `json:"heartbeat" yaml:"heartbeat"`
	Channels  ChannelsConfig  `json:"channels" yaml:"channels"`
	Skills    SkillsConfig    `json:"skills" yaml:"skills"`
	Voice     VoiceConfig     `json:"voice" yaml:"voice"`

	// TeamInfo is populated at runtime for agents that belong to a team.
	// Set by the caller before calling AssembleSystemPrompt.
	TeamInfo *TeamInfo `json:"-" yaml:"-"`
}

// TeamInfo holds team context for system prompt injection.
type TeamInfo struct {
	TeamID      string
	TeamName    string
	Description string
	Role        string // "lead" or "member"
	LeadName    string // Display name of the lead agent
	Members     []TeamMemberInfo
}

// TeamMemberInfo describes a team member for prompt injection.
type TeamMemberInfo struct {
	AgentID     string
	DisplayName string
	Role        string // "lead" or "member"
	Description string
}

// Identity defines who the agent is (identity.yaml).
type Identity struct {
	Name          string   `json:"name" yaml:"name"`
	Role          string   `json:"role" yaml:"role"`
	Model         string   `json:"model" yaml:"model"`                     // Tier (strong/fast/local) or model ID
	MaxTokens        int      `json:"max_tokens" yaml:"max_tokens"`
	MaxIterations    int      `json:"max_iterations" yaml:"max_iterations"`
	MaxRequestTokens int      `json:"max_request_tokens" yaml:"max_request_tokens"` // Hard cap on input tokens per API request (0 = no cap).
	TokensPerMinute  int      `json:"tokens_per_minute,omitempty" yaml:"tokens_per_minute"` // Agent-level TPM override. 0 = use provider default.
	ThinkingLevel string   `json:"thinking_level,omitempty" yaml:"thinking_level"` // "low", "medium", "high" — enables extended thinking on supported models.
	Avatar        string   `json:"avatar" yaml:"avatar"`                   // Emoji or URL
	Tags          []string `json:"tags" yaml:"tags"`
	Status        string   `json:"status" yaml:"status"`                   // "active" (default), "inactive"
}

// ToolsConfig defines which tools the agent can use (tools.yaml).
type ToolsConfig struct {
	// Profile sets the base tool set: full, coding, messaging, readonly, minimal.
	// Default: "" (treated as "full").
	Profile string `json:"profile,omitempty" yaml:"profile"`

	// Deny removes tools or groups. Use "group:runtime" to deny an entire group.
	// Tool-level: "write_file". Group-level: "group:runtime".
	Deny []string `json:"deny,omitempty" yaml:"deny"`

	// ShellDenyGroups controls which deny pattern groups are active for exec.
	// All groups are enabled by default. Set a group to false to disable it.
	ShellDenyGroups map[string]bool `json:"shell_deny_groups,omitempty" yaml:"shell_deny_groups"`

	// MCPServers defines external MCP server connections.
	MCPServers map[string]MCPServerConfig `json:"mcp_servers,omitempty" yaml:"mcp_servers"`

	// ExecSecurity controls command execution approval: "deny", "safe-only", "ask".
	// Default: "safe-only" — auto-approves safe commands, blocks others.
	ExecSecurity string `json:"exec_security,omitempty" yaml:"exec_security"`

	// ExecAllowlist adds or overrides safe binaries for exec approval.
	// Merged on top of tool.DefaultSafeBinaries. Set a binary to false to block it.
	ExecAllowlist map[string]bool `json:"exec_allowlist,omitempty" yaml:"exec_allowlist"`

	// Grounding enables search grounding. Values: "google_search" (Gemini), "web_search" (OpenAI).
	Grounding string `json:"grounding,omitempty" yaml:"grounding"`

	// CodeExecution enables native code execution (Gemini code_execution tool).
	CodeExecution bool `json:"code_execution,omitempty" yaml:"code_execution"`

	// Config holds per-tool configuration overrides.
	Config map[string]map[string]any `json:"config,omitempty" yaml:"config"`

	// Headless mode: no consent prompts. In-profile tools execute freely.
	// Always-consent groups blocked unless listed in PreAuthorize.
	Headless bool `json:"headless,omitempty" yaml:"headless"`

	// PreAuthorize lists always-consent groups to auto-approve in headless mode.
	// Examples: "runtime", "orchestration", "mcp:weather-server".
	PreAuthorize []string `json:"pre_authorize,omitempty" yaml:"pre_authorize"`

	// Deprecated fields — kept for YAML parsing so we can warn on load.
	DeprecatedEnabled        []string `json:"-" yaml:"enabled,omitempty"`
	DeprecatedAlsoAllow      []string `json:"-" yaml:"also_allow,omitempty"`
	DeprecatedNonInteractive *bool    `json:"-" yaml:"non_interactive,omitempty"`
	DeprecatedPreAuthGroups  []string `json:"-" yaml:"pre_authorized_groups,omitempty"`
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

// SkillsConfig defines which marketplace skills this agent uses (skills.yaml).
type SkillsConfig struct {
	// Skills lists skill names from the skills/ directory.
	// Empty = no marketplace skills loaded (explicit assignment).
	Skills []string `json:"skills" yaml:"skills"`
}

// Default voice constants.
const (
	// DefaultVoiceName is the default Gemini voice preset for SageClaw agents.
	// Sadaltager = "Knowledgeable" — fits an AI assistant persona.
	DefaultVoiceName = "Sadaltager"

	// DefaultVoiceModel is the default Gemini Live native audio model.
	// Note: "gemini-live-2.5-flash-native-audio" is Vertex AI only.
	// The Google AI Studio (v1beta) API uses the preview model ID.
	DefaultVoiceModel = "gemini-2.5-flash-native-audio-preview-12-2025"
)

// VoiceConfig defines voice messaging capabilities.
// When enabled, the agent can receive and respond with voice messages
// using a native audio model (e.g. Gemini Live).
type VoiceConfig struct {
	Enabled      bool   `json:"enabled" yaml:"enabled"`
	Model        string `json:"model,omitempty" yaml:"model"`                 // Audio model ID
	VoiceName    string `json:"voice_name,omitempty" yaml:"voice_name"`       // Voice preset (e.g. "Kore", "Sadaltager")
	LanguageCode string `json:"language_code,omitempty" yaml:"language_code"` // BCP-47 code (e.g. "en-US", "vi-VN"). Empty = auto-detect.
}

// HasVoice returns true if this agent has voice messaging enabled.
func (cfg *AgentConfig) HasVoice() bool {
	return cfg.Voice.Enabled
}

// VoiceModel returns the voice model ID, with a default fallback.
func (cfg *AgentConfig) VoiceModel() string {
	if cfg.Voice.Model != "" {
		return cfg.Voice.Model
	}
	return DefaultVoiceModel
}

// VoiceNameOrDefault returns the voice preset name, defaulting to Sadaltager.
func (cfg *AgentConfig) VoiceNameOrDefault() string {
	if cfg.Voice.VoiceName != "" {
		return cfg.Voice.VoiceName
	}
	return DefaultVoiceName
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
