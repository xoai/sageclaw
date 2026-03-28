package relay

import (
	"os"

	"gopkg.in/yaml.v3"
)

// Config configures the relay server.
type Config struct {
	Domain string    `yaml:"domain"` // e.g. "sageclaw.io" — subdomain wildcard routing
	Listen string    `yaml:"listen"` // e.g. ":443" or ":8080"
	TLS    TLSConfig `yaml:"tls"`
	Auth   AuthConfig `yaml:"auth"`
	Limits LimitsConfig `yaml:"limits"`
	DB     string    `yaml:"db"` // SQLite path for token store (managed mode)
}

// TLSConfig for relay TLS.
type TLSConfig struct {
	CertFile string `yaml:"cert_file"` // path to PEM cert (wildcard cert for *.domain)
	KeyFile  string `yaml:"key_file"`  // path to PEM key
}

// AuthConfig for relay authentication.
type AuthConfig struct {
	Mode  string `yaml:"mode"`  // "token" (default) or "open" (development only)
	Token string `yaml:"token"` // shared secret for self-hosted single-token mode
}

// LimitsConfig for abuse prevention.
type LimitsConfig struct {
	MaxTunnels           int   `yaml:"max_tunnels"`            // default: 100
	MaxConcurrentPerTunnel int `yaml:"max_concurrent_requests"` // default: 50
	MaxBodySize          int64 `yaml:"max_body_size"`           // default: 10MB
}

// Defaults returns a Config with sensible defaults.
func (c Config) Defaults() Config {
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.Auth.Mode == "" {
		c.Auth.Mode = "token"
	}
	if c.Limits.MaxTunnels == 0 {
		c.Limits.MaxTunnels = 100
	}
	if c.Limits.MaxConcurrentPerTunnel == 0 {
		c.Limits.MaxConcurrentPerTunnel = 50
	}
	if c.Limits.MaxBodySize == 0 {
		c.Limits.MaxBodySize = 10 << 20 // 10MB
	}
	if c.DB == "" {
		c.DB = "relay.db"
	}
	return c
}

// LoadConfig reads a YAML config file.
func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg.Defaults(), nil
}
