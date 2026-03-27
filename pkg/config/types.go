package config

// AppConfig holds all loaded configuration.
type AppConfig struct {
	Agents     map[string]AgentConfig   `yaml:"agents"`
	Delegation []DelegationLinkConfig   `yaml:"links"`
	Teams      []TeamConfig             `yaml:"teams"`
	Router     *RouterConfig            `yaml:"router"`
	Audio      AudioConfig              `yaml:"audio"`
}

// AudioConfig configures voice message audio storage.
type AudioConfig struct {
	StoragePath string `yaml:"storage_path"` // Default: "data/audio"
	MaxAgeDays  int    `yaml:"max_age_days"` // 0 = keep forever (default)
}

// AgentConfig defines an agent.
type AgentConfig struct {
	Name         string   `yaml:"name"`
	Tier         string   `yaml:"tier"`
	Model        string   `yaml:"model"`
	SystemPrompt string   `yaml:"system_prompt"`
	MaxTokens    int      `yaml:"max_tokens"`
	Tools        []string `yaml:"tools"`
	Skills       []string `yaml:"skills"`
}

// DelegationLinkConfig defines a delegation path.
type DelegationLinkConfig struct {
	Source        string `yaml:"source"`
	Target        string `yaml:"target"`
	Direction     string `yaml:"direction"`
	MaxConcurrent int    `yaml:"max_concurrent"`
	TimeoutSec    int    `yaml:"timeout"` // Per-link timeout in seconds. 0 = default (300s).
}

// TeamConfig defines a team.
type TeamConfig struct {
	ID      string   `yaml:"id"`
	Name    string   `yaml:"name"`
	Lead    string   `yaml:"lead"`
	Members []string `yaml:"members"`
}

// RouterConfig overrides auto-detected provider routing.
type RouterConfig struct {
	Tiers    map[string]TierConfig `yaml:"tiers"`
	Fallback string                `yaml:"fallback"`
}

// TierConfig maps a tier to a provider and model.
type TierConfig struct {
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}
