package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_AgentsYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "agents.yaml"), []byte(`
agents:
  researcher:
    name: "Research Agent"
    tier: strong
    system_prompt: "You research."
    max_tokens: 4096
    tools: [web_search, memory_search]
    skills: [memory]
  coder:
    name: "Coding Agent"
    tier: strong
    tools: [read_file, write_file]
`), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(cfg.Agents))
	}
	r := cfg.Agents["researcher"]
	if r.Name != "Research Agent" {
		t.Fatalf("wrong name: %s", r.Name)
	}
	if r.Tier != "strong" {
		t.Fatalf("wrong tier: %s", r.Tier)
	}
	if len(r.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(r.Tools))
	}
}

func TestLoad_DelegationYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "delegation.yaml"), []byte(`
links:
  - source: coordinator
    target: researcher
    direction: async
    max_concurrent: 2
  - source: coordinator
    target: coder
    direction: sync
`), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(cfg.Delegation) != 2 {
		t.Fatalf("expected 2 links, got %d", len(cfg.Delegation))
	}
	if cfg.Delegation[0].Source != "coordinator" || cfg.Delegation[0].Target != "researcher" {
		t.Fatalf("wrong link: %+v", cfg.Delegation[0])
	}
	if cfg.Delegation[0].MaxConcurrent != 2 {
		t.Fatalf("expected max_concurrent 2, got %d", cfg.Delegation[0].MaxConcurrent)
	}
}

func TestLoad_TeamsYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "teams.yaml"), []byte(`
teams:
  productivity:
    name: "Personal Productivity"
    lead: coordinator
    members: [researcher, coder]
`), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if len(cfg.Teams) != 1 {
		t.Fatalf("expected 1 team, got %d", len(cfg.Teams))
	}
	if cfg.Teams[0].Lead != "coordinator" {
		t.Fatalf("wrong lead: %s", cfg.Teams[0].Lead)
	}
	if len(cfg.Teams[0].Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(cfg.Teams[0].Members))
	}
}

func TestLoad_RouterYAML(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "router.yaml"), []byte(`
router:
  tiers:
    local:
      provider: ollama
      model: "llama3.2:3b"
    strong:
      provider: anthropic
      model: "claude-sonnet-4-20250514"
  fallback: strong
`), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if cfg.Router == nil {
		t.Fatal("expected router config")
	}
	if len(cfg.Router.Tiers) != 2 {
		t.Fatalf("expected 2 tiers, got %d", len(cfg.Router.Tiers))
	}
	if cfg.Router.Fallback != "strong" {
		t.Fatalf("wrong fallback: %s", cfg.Router.Fallback)
	}
}

func TestLoad_EnvExpansion(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("TEST_SAGECLAW_KEY", "my-secret-key")
	defer os.Unsetenv("TEST_SAGECLAW_KEY")

	os.WriteFile(filepath.Join(dir, "agents.yaml"), []byte(`
agents:
  default:
    name: "Test"
    system_prompt: "Key is ${TEST_SAGECLAW_KEY}"
`), 0644)

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	if cfg.Agents["default"].SystemPrompt != "Key is my-secret-key" {
		t.Fatalf("env not expanded: %s", cfg.Agents["default"].SystemPrompt)
	}
}

func TestLoad_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	// No config files at all — should return empty config, not error.
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(cfg.Agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(cfg.Agents))
	}
}
