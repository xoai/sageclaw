package tunnel

// DefaultRelayURL is the managed relay endpoint.
const DefaultRelayURL = "wss://relay.sageclaw.io/connect"

// Config configures the tunnel client.
type Config struct {
	Mode         string `yaml:"mode"`          // "managed" (default), "self-hosted", "disabled"
	RelayURL     string `yaml:"relay_url"`     // WebSocket URL of the relay server
	Token        string `yaml:"token"`         // Self-hosted: shared secret token
	AutoStart    bool   `yaml:"auto_start"`    // Start tunnel on SageClaw boot
	AutoWebhook  bool   `yaml:"auto_webhook"`  // Auto-register webhooks on tunnel start
	LocalPort    int    `yaml:"-"`             // Set programmatically, not from config
}

// Enabled returns true if the tunnel is not explicitly disabled.
func (c Config) Enabled() bool {
	return c.Mode != "disabled"
}

// Defaults returns a Config with sensible defaults applied.
func (c Config) Defaults() Config {
	if c.Mode == "" {
		c.Mode = "managed"
	}
	if c.RelayURL == "" {
		c.RelayURL = DefaultRelayURL
	}
	return c
}
