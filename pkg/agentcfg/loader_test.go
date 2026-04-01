package agentcfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAgent(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "test-agent")
	os.MkdirAll(agentDir, 0755)

	// Write identity.yaml
	os.WriteFile(filepath.Join(agentDir, "identity.yaml"), []byte(`
name: TestBot
role: Test assistant
model: fast
max_tokens: 4096
max_iterations: 10
avatar: "🤖"
tags: [test, demo]
`), 0644)

	// Write soul.md
	os.WriteFile(filepath.Join(agentDir, "soul.md"), []byte(`# Soul

You are a helpful test bot.

## Voice
- Brief and precise
`), 0644)

	// Write behavior.md
	os.WriteFile(filepath.Join(agentDir, "behavior.md"), []byte(`# Behavior

## Rules
- Always respond in English
- Keep responses short
`), 0644)

	// Write tools.yaml
	os.WriteFile(filepath.Join(agentDir, "tools.yaml"), []byte(`
profile: coding
deny:
  - group:runtime
config:
  web_search:
    max_results: 5
`), 0644)

	// Write memory.yaml
	os.WriteFile(filepath.Join(agentDir, "memory.yaml"), []byte(`
scope: project
auto_store: true
retention_days: 30
search_limit: 5
tags_boost:
  - important
`), 0644)

	// Write heartbeat.yaml
	os.WriteFile(filepath.Join(agentDir, "heartbeat.yaml"), []byte(`
schedules:
  - name: daily-check
    cron: "0 9 * * *"
    prompt: "Check for updates"
    channel: web
`), 0644)

	// Write channels.yaml
	os.WriteFile(filepath.Join(agentDir, "channels.yaml"), []byte(`
serve:
  - web
  - telegram
overrides:
  telegram:
    max_tokens: 2048
`), 0644)

	cfg, err := LoadAgent(agentDir)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}

	// Identity
	if cfg.ID != "test-agent" {
		t.Errorf("ID = %q, want %q", cfg.ID, "test-agent")
	}
	if cfg.Identity.Name != "TestBot" {
		t.Errorf("Name = %q, want %q", cfg.Identity.Name, "TestBot")
	}
	if cfg.Identity.Role != "Test assistant" {
		t.Errorf("Role = %q, want %q", cfg.Identity.Role, "Test assistant")
	}
	if cfg.Identity.Model != "fast" {
		t.Errorf("Model = %q, want %q", cfg.Identity.Model, "fast")
	}
	if cfg.Identity.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", cfg.Identity.MaxTokens)
	}
	if cfg.Identity.Avatar != "🤖" {
		t.Errorf("Avatar = %q, want 🤖", cfg.Identity.Avatar)
	}
	if len(cfg.Identity.Tags) != 2 {
		t.Errorf("Tags = %v, want 2 tags", cfg.Identity.Tags)
	}

	// Soul
	if cfg.Soul == "" {
		t.Error("Soul should not be empty")
	}
	if len(cfg.Soul) < 20 {
		t.Errorf("Soul too short: %q", cfg.Soul)
	}

	// Behavior
	if cfg.Behavior == "" {
		t.Error("Behavior should not be empty")
	}

	// Tools
	if cfg.Tools.Profile != "coding" {
		t.Errorf("Tools.Profile = %q, want %q", cfg.Tools.Profile, "coding")
	}
	if len(cfg.Tools.Deny) != 1 || cfg.Tools.Deny[0] != "group:runtime" {
		t.Errorf("Tools.Deny = %v, want [group:runtime]", cfg.Tools.Deny)
	}
	if cfg.Tools.Config["web_search"]["max_results"] != 5 {
		t.Errorf("web_search.max_results = %v, want 5", cfg.Tools.Config["web_search"]["max_results"])
	}

	// Memory
	if cfg.Memory.Scope != "project" {
		t.Errorf("Memory.Scope = %q, want project", cfg.Memory.Scope)
	}
	if !cfg.Memory.AutoStore {
		t.Error("Memory.AutoStore should be true")
	}
	if cfg.Memory.RetentionDays != 30 {
		t.Errorf("Memory.RetentionDays = %d, want 30", cfg.Memory.RetentionDays)
	}

	// Heartbeat
	if len(cfg.Heartbeat.Schedules) != 1 {
		t.Fatalf("Heartbeat.Schedules = %d, want 1", len(cfg.Heartbeat.Schedules))
	}
	if cfg.Heartbeat.Schedules[0].Name != "daily-check" {
		t.Errorf("schedule name = %q, want daily-check", cfg.Heartbeat.Schedules[0].Name)
	}

	// Channels
	if len(cfg.Channels.Serve) != 2 {
		t.Errorf("Channels.Serve = %v, want 2", cfg.Channels.Serve)
	}
	if cfg.Channels.Overrides["telegram"].MaxTokens != 2048 {
		t.Errorf("telegram override max_tokens = %d, want 2048", cfg.Channels.Overrides["telegram"].MaxTokens)
	}
}

func TestLoadAgent_MissingIdentity(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "empty-agent")
	os.MkdirAll(agentDir, 0755)

	_, err := LoadAgent(agentDir)
	if err == nil {
		t.Fatal("LoadAgent should fail without identity.yaml")
	}
}

func TestLoadAgent_MinimalConfig(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "minimal")
	os.MkdirAll(agentDir, 0755)

	// Only identity.yaml — everything else is optional.
	os.WriteFile(filepath.Join(agentDir, "identity.yaml"), []byte(`
name: MinimalBot
model: strong
`), 0644)

	cfg, err := LoadAgent(agentDir)
	if err != nil {
		t.Fatalf("LoadAgent: %v", err)
	}

	if cfg.Identity.Name != "MinimalBot" {
		t.Errorf("Name = %q, want MinimalBot", cfg.Identity.Name)
	}
	if cfg.Soul != "" {
		t.Error("Soul should be empty for minimal config")
	}
	if cfg.Behavior != "" {
		t.Error("Behavior should be empty for minimal config")
	}
	// Defaults should be applied.
	if cfg.Memory.Scope != "project" {
		t.Errorf("default Memory.Scope = %q, want project", cfg.Memory.Scope)
	}
}

func TestLoadAll(t *testing.T) {
	dir := t.TempDir()

	// Create two agents.
	for _, name := range []string{"alpha", "beta"} {
		agentDir := filepath.Join(dir, name)
		os.MkdirAll(agentDir, 0755)
		os.WriteFile(filepath.Join(agentDir, "identity.yaml"), []byte("name: "+name+"\nmodel: strong\n"), 0644)
	}

	// Create a non-agent directory (no identity.yaml).
	os.MkdirAll(filepath.Join(dir, "notanagent"), 0755)

	agents, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	if len(agents) != 2 {
		t.Errorf("LoadAll returned %d agents, want 2", len(agents))
	}
	if agents["alpha"] == nil {
		t.Error("missing agent 'alpha'")
	}
	if agents["beta"] == nil {
		t.Error("missing agent 'beta'")
	}
}

func TestLoadAll_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	agents, err := LoadAll(dir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("LoadAll returned %d agents for empty dir, want 0", len(agents))
	}
}

func TestLoadAll_MissingDir(t *testing.T) {
	agents, err := LoadAll("/nonexistent/path")
	if err != nil {
		t.Fatalf("LoadAll should not error for missing dir: %v", err)
	}
	if len(agents) != 0 {
		t.Errorf("want 0 agents for missing dir, got %d", len(agents))
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "roundtrip")

	original := &AgentConfig{
		ID:     "roundtrip",
		Source: "file",
		Identity: Identity{
			Name:      "RoundTrip",
			Role:      "Test bot",
			Model:     "fast",
			MaxTokens: 2048,
			Tags:      []string{"test"},
		},
		Soul:     "# Soul\n\nI am a test bot.",
		Behavior: "# Behavior\n\nBe concise.",
		Tools: ToolsConfig{
			Profile: "messaging",
		},
		Memory: MemoryConfig{
			Scope:     "global",
			AutoStore: true,
		},
		Heartbeat: HeartbeatConfig{
			Schedules: []HeartbeatSchedule{
				{Name: "check", Cron: "0 * * * *", Prompt: "check", Channel: "web"},
			},
		},
		Channels: ChannelsConfig{
			Serve: []string{"web"},
		},
	}

	if err := SaveAgent(original, agentDir); err != nil {
		t.Fatalf("SaveAgent: %v", err)
	}

	loaded, err := LoadAgent(agentDir)
	if err != nil {
		t.Fatalf("LoadAgent after save: %v", err)
	}

	if loaded.Identity.Name != original.Identity.Name {
		t.Errorf("Name = %q, want %q", loaded.Identity.Name, original.Identity.Name)
	}
	if loaded.Identity.Model != original.Identity.Model {
		t.Errorf("Model = %q, want %q", loaded.Identity.Model, original.Identity.Model)
	}
	if loaded.Soul != original.Soul {
		t.Errorf("Soul mismatch:\ngot:  %q\nwant: %q", loaded.Soul, original.Soul)
	}
	if loaded.Behavior != original.Behavior {
		t.Errorf("Behavior mismatch")
	}
	if loaded.Tools.Profile != "messaging" {
		t.Errorf("Tools.Profile = %q, want %q", loaded.Tools.Profile, "messaging")
	}
	if loaded.Memory.Scope != "global" {
		t.Errorf("Memory.Scope = %q, want global", loaded.Memory.Scope)
	}
	if len(loaded.Heartbeat.Schedules) != 1 {
		t.Errorf("Heartbeat.Schedules = %d, want 1", len(loaded.Heartbeat.Schedules))
	}
}

func TestAssembleSystemPrompt(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{
			Name: "TestBot",
			Role: "research assistant",
		},
		Soul:     "# Soul\n\nYou are curious and thorough.",
		Behavior: "# Behavior\n\nAlways cite sources.",
		Memory: MemoryConfig{
			Scope:     "project",
			AutoStore: true,
			TagsBoost: []string{"important"},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	if !contains(prompt, "You are TestBot, research assistant.") {
		t.Error("prompt should contain role line")
	}
	if !contains(prompt, "curious and thorough") {
		t.Error("prompt should contain soul content")
	}
	if !contains(prompt, "cite sources") {
		t.Error("prompt should contain behavior content")
	}
	if !contains(prompt, "scope: project") {
		t.Error("prompt should contain memory scope")
	}
	if !contains(prompt, "auto-store: enabled") {
		t.Error("prompt should contain auto-store")
	}
	// Identity anchoring.
	if !contains(prompt, "non-negotiable") {
		t.Error("prompt should contain identity anchoring")
	}
}

func TestAssembleSystemPrompt_WithBootstrap(t *testing.T) {
	cfg := &AgentConfig{
		Identity:  Identity{Name: "Bot", Role: "helper"},
		Bootstrap: "Introduce yourself and learn about the user.",
	}

	prompt := AssembleSystemPrompt(cfg)
	if !contains(prompt, "FIRST RUN") {
		t.Error("prompt should contain FIRST RUN marker")
	}
	if !contains(prompt, "Introduce yourself") {
		t.Error("prompt should contain bootstrap content")
	}
}

func TestAssembleSystemPrompt_WithVoice(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{Name: "Bot", Role: "assistant"},
		Voice:    VoiceConfig{Enabled: true},
	}

	prompt := AssembleSystemPrompt(cfg)
	if !contains(prompt, "VOICE MODE") {
		t.Error("voice-enabled prompt should contain VOICE MODE instruction")
	}
	if !contains(prompt, "concise and conversational") {
		t.Error("voice prompt should include conversational guidance")
	}
	// No language instruction when language_code is empty.
	if contains(prompt, "RESPOND IN") {
		t.Error("should not have language instruction when language_code is empty")
	}
}

func TestAssembleSystemPrompt_WithVoiceLanguage(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{Name: "Bot", Role: "assistant"},
		Voice:    VoiceConfig{Enabled: true, LanguageCode: "vi-VN"},
	}

	prompt := AssembleSystemPrompt(cfg)
	if !contains(prompt, "RESPOND IN vi-VN") {
		t.Error("voice prompt should contain language instruction for vi-VN")
	}
	if !contains(prompt, "UNMISTAKABLY IN vi-VN") {
		t.Error("voice prompt should contain emphatic language instruction")
	}
}

func TestAssembleSystemPrompt_WithTeamLead(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{Name: "LeadBot", Role: "team lead"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Content Team",
			Role:     "lead",
			LeadName: "LeadBot",
			Members: []TeamMemberInfo{
				{AgentID: "lead-bot", DisplayName: "LeadBot", Role: "lead", Description: "Team lead"},
				{AgentID: "researcher", DisplayName: "Researcher", Role: "member", Description: "Handles research"},
				{AgentID: "writer", DisplayName: "Writer", Role: "member", Description: "Writes articles"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)
	if !contains(prompt, "## Team: Content Team") {
		t.Error("prompt should contain team header")
	}
	if !contains(prompt, "team lead") {
		t.Error("prompt should contain lead role")
	}
	if !contains(prompt, "team lead** of Content Team") {
		t.Error("prompt should use hybrid role format with team name")
	}
	if !contains(prompt, "**Researcher** (`researcher`)") {
		t.Error("prompt should contain member listing")
	}
	if !contains(prompt, "team_tasks") {
		t.Error("prompt should reference team_tasks tool")
	}
	if !contains(prompt, "team-task-result") {
		t.Error("prompt should contain injection warning")
	}
	if !contains(prompt, "parent_id") {
		t.Error("prompt should contain subtask creation example with parent_id")
	}
	// Delegation guidance (v2: [Delegation Analysis] directive).
	if !contains(prompt, "Delegation Guidance") {
		t.Error("prompt should contain Delegation Guidance section")
	}
	if !contains(prompt, "[Delegation Analysis]") {
		t.Error("prompt should reference [Delegation Analysis] block")
	}
	if contains(prompt, "MANDATORY DEFAULT") {
		t.Error("prompt should NOT contain pure-orchestrator MANDATORY DEFAULT")
	}
	// Task planning section.
	if !contains(prompt, "Task Planning") {
		t.Error("prompt should contain Task Planning section")
	}
	if !contains(prompt, "Anti-pattern") {
		t.Error("prompt should contain task planning anti-pattern")
	}
	// Rules.
	if !contains(prompt, "Follow the [Delegation Analysis]") {
		t.Error("prompt should contain analysis-following rule")
	}
	if !contains(prompt, "Search before create") {
		t.Error("prompt should contain search-before-create rule")
	}
	// Communication template removed.
	if contains(prompt, "### Communication") {
		t.Error("prompt should NOT contain prescriptive Communication template")
	}
}

func TestAssembleSystemPrompt_WithTeamLead_ZeroMembers(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{Name: "SoloLead", Role: "team lead"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Solo Team",
			Role:     "lead",
			LeadName: "SoloLead",
			Members: []TeamMemberInfo{
				{AgentID: "solo-lead", DisplayName: "SoloLead", Role: "lead"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)
	if contains(prompt, "## Team:") {
		t.Error("zero-member lead should NOT get team section")
	}
}

func TestAssembleSystemPrompt_WithTeamMember(t *testing.T) {
	cfg := &AgentConfig{
		Identity: Identity{Name: "Researcher", Role: "research agent"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Content Team",
			Role:     "member",
			LeadName: "LeadBot",
		},
	}

	prompt := AssembleSystemPrompt(cfg)
	if !contains(prompt, "## Team: Content Team") {
		t.Error("prompt should contain team header")
	}
	if !contains(prompt, "Role: member of **Content Team**") {
		t.Error("prompt should contain enriched member role with team name")
	}
	if !contains(prompt, "**LeadBot**") {
		t.Error("prompt should reference lead name")
	}
	if !contains(prompt, "### Your Workflow") {
		t.Error("prompt should contain workflow section")
	}
	if !contains(prompt, "blocker:") {
		t.Error("prompt should contain blocker escalation instructions")
	}
	if !contains(prompt, `team_tasks(action: "send"`) {
		t.Error("prompt should contain send action reference")
	}
	if !contains(prompt, "### Rules") {
		t.Error("prompt should contain rules section")
	}
	if !contains(prompt, "auto-completes the task") {
		t.Error("prompt should warn against manual complete")
	}
	if contains(prompt, "team-task-result") {
		t.Error("member prompt should NOT contain injection warning")
	}
}

func TestToRuntimeConfig_VoiceFields(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "test",
		Identity: Identity{Name: "Bot", Model: "strong", MaxTokens: 4096, MaxIterations: 10},
		Voice:    VoiceConfig{Enabled: true, Model: "custom-model", VoiceName: "Kore"},
	}

	rc := ToRuntimeConfig(cfg)
	if !rc.VoiceEnabled {
		t.Error("VoiceEnabled should be true")
	}
	if rc.VoiceModel != "custom-model" {
		t.Errorf("VoiceModel = %q, want custom-model", rc.VoiceModel)
	}
	if rc.VoiceName != "Kore" {
		t.Errorf("VoiceName = %q, want Kore", rc.VoiceName)
	}
}

func TestToRuntimeConfig_VoiceDefaults(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "test",
		Identity: Identity{Name: "Bot", Model: "strong"},
		Voice:    VoiceConfig{Enabled: true},
	}

	rc := ToRuntimeConfig(cfg)
	if rc.VoiceModel != DefaultVoiceModel {
		t.Errorf("VoiceModel = %q, want %q", rc.VoiceModel, DefaultVoiceModel)
	}
	if rc.VoiceName != DefaultVoiceName {
		t.Errorf("VoiceName = %q, want %q", rc.VoiceName, DefaultVoiceName)
	}
}

func TestVoiceNameOrDefault(t *testing.T) {
	cfg := &AgentConfig{Voice: VoiceConfig{VoiceName: "Puck"}}
	if cfg.VoiceNameOrDefault() != "Puck" {
		t.Error("should return configured voice name")
	}

	cfg2 := &AgentConfig{}
	if cfg2.VoiceNameOrDefault() != DefaultVoiceName {
		t.Errorf("should return default %q, got %q", DefaultVoiceName, cfg2.VoiceNameOrDefault())
	}
}

func TestTruncateContext(t *testing.T) {
	// Short content — no truncation.
	short := "Hello world"
	if TruncateContext(short, 100) != short {
		t.Error("short content should not be truncated")
	}

	// Long content — truncated.
	long := strings.Repeat("x", 1000)
	truncated := TruncateContext(long, 200)
	if len(truncated) > 250 { // 200 + marker text
		t.Errorf("truncated length = %d, want <= 250", len(truncated))
	}
	if !contains(truncated, "truncated") {
		t.Error("should contain truncation marker")
	}
}

func TestAgentStatus(t *testing.T) {
	active := &AgentConfig{Identity: Identity{Status: "active"}}
	if !active.IsActive() {
		t.Error("active agent should be active")
	}

	inactive := &AgentConfig{Identity: Identity{Status: "inactive"}}
	if inactive.IsActive() {
		t.Error("inactive agent should not be active")
	}

	defaultStatus := &AgentConfig{Identity: Identity{}}
	if !defaultStatus.IsActive() {
		t.Error("agent with no status should default to active")
	}
}

func TestToRuntimeConfig(t *testing.T) {
	cfg := &AgentConfig{
		ID: "test",
		Identity: Identity{
			Name:          "Test",
			Role:          "helper",
			Model:         "claude-sonnet-4-20250514",
			MaxTokens:     4096,
			MaxIterations: 15,
		},
		Tools: ToolsConfig{
			Profile:  "coding",
			Deny:     []string{"group:runtime"},
			Headless: true,
			PreAuthorize: []string{"mcp:weather"},
		},
	}

	rc := ToRuntimeConfig(cfg)

	if rc.AgentID != "test" {
		t.Errorf("AgentID = %q, want test", rc.AgentID)
	}
	if rc.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", rc.Model)
	}
	if rc.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", rc.MaxTokens)
	}
	if rc.MaxIterations != 15 {
		t.Errorf("MaxIterations = %d, want 15", rc.MaxIterations)
	}
	if rc.ToolProfile != "coding" {
		t.Errorf("ToolProfile = %q, want coding", rc.ToolProfile)
	}
	if len(rc.ToolDeny) != 1 || rc.ToolDeny[0] != "group:runtime" {
		t.Errorf("ToolDeny = %v, want [group:runtime]", rc.ToolDeny)
	}
	if !rc.Headless {
		t.Error("Headless should be true")
	}
	if len(rc.PreAuthorize) != 1 || rc.PreAuthorize[0] != "mcp:weather" {
		t.Errorf("PreAuthorize = %v, want [mcp:weather]", rc.PreAuthorize)
	}
	if rc.SystemPrompt == "" {
		t.Error("SystemPrompt should not be empty")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstring(s, substr)
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
