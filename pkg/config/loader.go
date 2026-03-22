package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// Load reads configuration from YAML files in the given directory.
// Missing files are silently skipped (all configs are optional).
func Load(configDir string) (*AppConfig, error) {
	cfg := &AppConfig{
		Agents: make(map[string]AgentConfig),
	}

	// agents.yaml
	if data, err := readAndExpand(filepath.Join(configDir, "agents.yaml")); err == nil {
		var wrapper struct {
			Agents map[string]AgentConfig `yaml:"agents"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing agents.yaml: %w", err)
		}
		cfg.Agents = wrapper.Agents
	}

	// delegation.yaml
	if data, err := readAndExpand(filepath.Join(configDir, "delegation.yaml")); err == nil {
		var wrapper struct {
			Links []DelegationLinkConfig `yaml:"links"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing delegation.yaml: %w", err)
		}
		cfg.Delegation = wrapper.Links
	}

	// teams.yaml
	if data, err := readAndExpand(filepath.Join(configDir, "teams.yaml")); err == nil {
		var wrapper struct {
			Teams map[string]struct {
				Name    string   `yaml:"name"`
				Lead    string   `yaml:"lead"`
				Members []string `yaml:"members"`
			} `yaml:"teams"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing teams.yaml: %w", err)
		}
		for id, t := range wrapper.Teams {
			cfg.Teams = append(cfg.Teams, TeamConfig{
				ID: id, Name: t.Name, Lead: t.Lead, Members: t.Members,
			})
		}
	}

	// router.yaml
	if data, err := readAndExpand(filepath.Join(configDir, "router.yaml")); err == nil {
		var wrapper struct {
			Router RouterConfig `yaml:"router"`
		}
		if err := yaml.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("parsing router.yaml: %w", err)
		}
		if len(wrapper.Router.Tiers) > 0 {
			cfg.Router = &wrapper.Router
		}
	}

	return cfg, nil
}

// readAndExpand reads a file and expands ${VAR} environment variables.
func readAndExpand(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded := expandEnvVars(string(data))
	return []byte(expanded), nil
}

// expandEnvVars replaces ${VAR} with the environment variable value.
func expandEnvVars(s string) string {
	return envVarPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := strings.TrimSuffix(strings.TrimPrefix(match, "${"), "}")
		if val := os.Getenv(varName); val != "" {
			return val
		}
		return match // Keep original if not set.
	})
}
