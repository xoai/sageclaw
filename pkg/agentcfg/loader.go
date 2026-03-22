package agentcfg

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LoadAgent reads a single agent config from a directory.
// Only identity.yaml is required; all other files are optional.
func LoadAgent(dir string) (*AgentConfig, error) {
	id := filepath.Base(dir)

	// identity.yaml is required.
	identityPath := filepath.Join(dir, "identity.yaml")
	identityData, err := os.ReadFile(identityPath)
	if err != nil {
		return nil, fmt.Errorf("reading identity.yaml: %w", err)
	}

	cfg := Defaults(id)
	cfg.Dir = dir

	if err := yaml.Unmarshal(identityData, &cfg.Identity); err != nil {
		return nil, fmt.Errorf("parsing identity.yaml: %w", err)
	}
	// Ensure ID matches folder name.
	cfg.Identity.Name = firstNonEmpty(cfg.Identity.Name, id)

	// soul.md — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "soul.md")); err == nil {
		cfg.Soul = string(data)
	}

	// behavior.md — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "behavior.md")); err == nil {
		cfg.Behavior = string(data)
	}

	// bootstrap.md — optional, auto-deleted after first run.
	if data, err := os.ReadFile(filepath.Join(dir, "bootstrap.md")); err == nil {
		cfg.Bootstrap = string(data)
	}

	// Default status to "active".
	if cfg.Identity.Status == "" {
		cfg.Identity.Status = "active"
	}

	// tools.yaml — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "tools.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &cfg.Tools); err != nil {
			log.Printf("agentcfg: %s/tools.yaml parse error: %v (using defaults)", id, err)
		}
	}

	// memory.yaml — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "memory.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &cfg.Memory); err != nil {
			log.Printf("agentcfg: %s/memory.yaml parse error: %v (using defaults)", id, err)
		}
	}

	// heartbeat.yaml — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "heartbeat.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &cfg.Heartbeat); err != nil {
			log.Printf("agentcfg: %s/heartbeat.yaml parse error: %v (using defaults)", id, err)
		}
	}

	// channels.yaml — optional.
	if data, err := os.ReadFile(filepath.Join(dir, "channels.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &cfg.Channels); err != nil {
			log.Printf("agentcfg: %s/channels.yaml parse error: %v (using defaults)", id, err)
		}
	}

	return &cfg, nil
}

// LoadAll reads all agent configs from the base directory.
// Each subdirectory containing an identity.yaml is treated as an agent.
func LoadAll(baseDir string) (map[string]*AgentConfig, error) {
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]*AgentConfig{}, nil
		}
		return nil, fmt.Errorf("reading agents dir: %w", err)
	}

	agents := make(map[string]*AgentConfig)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dir := filepath.Join(baseDir, entry.Name())

		// Skip directories without identity.yaml.
		if _, err := os.Stat(filepath.Join(dir, "identity.yaml")); os.IsNotExist(err) {
			continue
		}

		cfg, err := LoadAgent(dir)
		if err != nil {
			log.Printf("agentcfg: skipping %s: %v", entry.Name(), err)
			continue
		}

		agents[cfg.ID] = cfg
	}

	return agents, nil
}

// SaveAgent writes an AgentConfig to a directory, creating files as needed.
func SaveAgent(cfg *AgentConfig, dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating agent dir: %w", err)
	}

	// identity.yaml
	identityData, err := yaml.Marshal(&cfg.Identity)
	if err != nil {
		return fmt.Errorf("marshaling identity: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, "identity.yaml"), identityData); err != nil {
		return err
	}

	// soul.md — only write if non-empty.
	if cfg.Soul != "" {
		if err := writeFileAtomic(filepath.Join(dir, "soul.md"), []byte(cfg.Soul)); err != nil {
			return err
		}
	}

	// behavior.md — only write if non-empty.
	if cfg.Behavior != "" {
		if err := writeFileAtomic(filepath.Join(dir, "behavior.md"), []byte(cfg.Behavior)); err != nil {
			return err
		}
	}

	// bootstrap.md — only write if non-empty.
	if cfg.Bootstrap != "" {
		if err := writeFileAtomic(filepath.Join(dir, "bootstrap.md"), []byte(cfg.Bootstrap)); err != nil {
			return err
		}
	}

	// tools.yaml — only write if tools are configured.
	if len(cfg.Tools.Enabled) > 0 || len(cfg.Tools.Config) > 0 {
		data, _ := yaml.Marshal(&cfg.Tools)
		if err := writeFileAtomic(filepath.Join(dir, "tools.yaml"), data); err != nil {
			return err
		}
	}

	// memory.yaml
	memData, _ := yaml.Marshal(&cfg.Memory)
	if err := writeFileAtomic(filepath.Join(dir, "memory.yaml"), memData); err != nil {
		return err
	}

	// heartbeat.yaml — only write if schedules exist.
	if len(cfg.Heartbeat.Schedules) > 0 {
		data, _ := yaml.Marshal(&cfg.Heartbeat)
		if err := writeFileAtomic(filepath.Join(dir, "heartbeat.yaml"), data); err != nil {
			return err
		}
	}

	// channels.yaml — only write if configured.
	if len(cfg.Channels.Serve) > 0 || len(cfg.Channels.Overrides) > 0 {
		data, _ := yaml.Marshal(&cfg.Channels)
		if err := writeFileAtomic(filepath.Join(dir, "channels.yaml"), data); err != nil {
			return err
		}
	}

	return nil
}

// writeFileAtomic writes data to a temp file then renames, preventing corruption.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming %s: %w", path, err)
	}
	return nil
}

// ConsumeBootstrap deletes bootstrap.md after the agent's first conversation.
// Returns the bootstrap content (empty if no bootstrap exists).
func ConsumeBootstrap(agentDir string) string {
	path := filepath.Join(agentDir, "bootstrap.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	// Delete the file — it's a one-time ritual.
	os.Remove(path)
	log.Printf("agentcfg: consumed bootstrap.md for %s", filepath.Base(agentDir))
	return string(data)
}

// IsActive returns true if the agent's status is "active" (or unset).
func (cfg *AgentConfig) IsActive() bool {
	return cfg.Identity.Status == "" || cfg.Identity.Status == "active"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
