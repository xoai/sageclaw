package config

// AppConfig holds all loaded configuration.
type AppConfig struct {
	Agents     map[string]AgentConfig   `yaml:"agents"`
	Delegation []DelegationLinkConfig   `yaml:"links"`
	Teams      []TeamConfig             `yaml:"teams"`
	Router     *RouterConfig            `yaml:"router"`
	Audio      AudioConfig              `yaml:"audio"`
	Tunnel     TunnelConfig             `yaml:"tunnel"`
}

// TunnelConfig configures the native reverse tunnel.
type TunnelConfig struct {
	Mode        string `yaml:"mode"`         // "managed" (default), "self-hosted", "disabled"
	RelayURL    string `yaml:"relay_url"`    // WebSocket URL of the relay server
	Token       string `yaml:"token"`        // Self-hosted: shared secret token
	AutoStart   bool   `yaml:"auto_start"`   // Start tunnel on SageClaw boot
	AutoWebhook bool   `yaml:"auto_webhook"` // Auto-register webhooks on tunnel start
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
