package openaicompat

// KnownProvider returns a pre-configured Config for well-known providers.
// The caller must still set APIKey (and optionally override BaseURL).
// Returns nil if the provider name is not recognized.
func KnownProvider(name string) *Config {
	cfg, ok := knownProviders[name]
	if !ok {
		return nil
	}
	cp := cfg // Don't mutate the registry entry.
	if cfg.Headers != nil {
		cp.Headers = make(map[string]string, len(cfg.Headers))
		for k, v := range cfg.Headers {
			cp.Headers[k] = v
		}
	}
	return &cp
}

// KnownProviderNames returns the list of all recognized provider names.
func KnownProviderNames() []string {
	names := make([]string, 0, len(knownProviders))
	for name := range knownProviders {
		names = append(names, name)
	}
	return names
}

var knownProviders = map[string]Config{
	"openrouter": {
		Name:    "openrouter",
		BaseURL: "https://openrouter.ai/api/v1",
		Headers: map[string]string{
			"HTTP-Referer": "https://sageclaw.dev",
			"X-Title":      "SageClaw",
		},
	},
	"deepseek": {
		Name:    "deepseek",
		BaseURL: "https://api.deepseek.com/v1",
		Quirks: Quirks{
			ThinkingField: "reasoning_content",
		},
	},
	"xai": {
		Name:    "xai",
		BaseURL: "https://api.x.ai/v1",
	},
	"groq": {
		Name:    "groq",
		BaseURL: "https://api.groq.com/openai/v1",
	},
	"together": {
		Name:    "together",
		BaseURL: "https://api.together.xyz/v1",
	},
	"fireworks": {
		Name:    "fireworks",
		BaseURL: "https://api.fireworks.ai/inference/v1",
	},
	"mistral": {
		Name:    "mistral",
		BaseURL: "https://api.mistral.ai/v1",
	},
	"ollama": {
		Name:    "ollama",
		BaseURL: "http://localhost:11434/v1",
		Quirks: Quirks{
			NoStreamOptions: true,
		},
	},
}
