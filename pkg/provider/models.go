package provider

// ModelInfo describes a model available from a provider.
type ModelInfo struct {
	ID            string  `json:"id"`             // e.g. "anthropic/claude-sonnet-4-20250514"
	Provider      string  `json:"provider"`       // e.g. "anthropic"
	ModelID       string  `json:"model_id"`       // e.g. "claude-sonnet-4-20250514" (raw API model ID)
	Name          string  `json:"name"`           // e.g. "Claude Sonnet 4"
	Tier          string  `json:"tier"`           // "strong", "fast", "vision", "reasoning"
	InputCost     float64 `json:"input_cost"`     // $ per 1M input tokens
	OutputCost    float64 `json:"output_cost"`    // $ per 1M output tokens
	CacheCost     float64 `json:"cache_cost"`     // $ per 1M cached input tokens (0 if no caching)
	ThinkingCost  float64 `json:"thinking_cost"`  // $ per 1M thinking/reasoning tokens (0 = use OutputCost)
	ContextWindow int     `json:"context_window"` // Max context in tokens
}

// EffectiveThinkingCost returns the cost per 1M thinking tokens.
// Falls back to OutputCost if ThinkingCost is not explicitly set.
func (m *ModelInfo) EffectiveThinkingCost() float64 {
	if m.ThinkingCost > 0 {
		return m.ThinkingCost
	}
	return m.OutputCost
}

// KnownModels is the registry of well-known models across all providers.
// Prices as of March 2026.
var KnownModels = []ModelInfo{
	// --- Anthropic ---
	{ID: "anthropic/claude-opus-4-20250514", Provider: "anthropic", ModelID: "claude-opus-4-20250514", Name: "Claude Opus 4", Tier: "reasoning", InputCost: 15.0, OutputCost: 75.0, CacheCost: 1.5, ContextWindow: 200000},
	{ID: "anthropic/claude-sonnet-4-20250514", Provider: "anthropic", ModelID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4", Tier: "strong", InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3, ContextWindow: 200000},
	{ID: "anthropic/claude-haiku-4-5-20251001", Provider: "anthropic", ModelID: "claude-haiku-4-5-20251001", Name: "Claude Haiku 4.5", Tier: "fast", InputCost: 0.8, OutputCost: 4.0, CacheCost: 0.08, ContextWindow: 200000},

	// --- OpenAI ---
	{ID: "openai/gpt-4.1", Provider: "openai", ModelID: "gpt-4.1", Name: "GPT-4.1", Tier: "strong", InputCost: 2.0, OutputCost: 8.0, CacheCost: 0.5, ContextWindow: 1000000},
	{ID: "openai/gpt-4.1-mini", Provider: "openai", ModelID: "gpt-4.1-mini", Name: "GPT-4.1 Mini", Tier: "fast", InputCost: 0.4, OutputCost: 1.6, CacheCost: 0.1, ContextWindow: 1000000},
	{ID: "openai/gpt-4.1-nano", Provider: "openai", ModelID: "gpt-4.1-nano", Name: "GPT-4.1 Nano", Tier: "fast", InputCost: 0.1, OutputCost: 0.4, CacheCost: 0.025, ContextWindow: 1000000},
	{ID: "openai/gpt-4o", Provider: "openai", ModelID: "gpt-4o", Name: "GPT-4o", Tier: "strong", InputCost: 2.5, OutputCost: 10.0, CacheCost: 1.25, ContextWindow: 128000},
	{ID: "openai/gpt-4o-mini", Provider: "openai", ModelID: "gpt-4o-mini", Name: "GPT-4o Mini", Tier: "fast", InputCost: 0.15, OutputCost: 0.6, CacheCost: 0.075, ContextWindow: 128000},
	{ID: "openai/o3", Provider: "openai", ModelID: "o3", Name: "o3", Tier: "reasoning", InputCost: 10.0, OutputCost: 40.0, CacheCost: 2.5, ContextWindow: 200000},
	{ID: "openai/o3-mini", Provider: "openai", ModelID: "o3-mini", Name: "o3 Mini", Tier: "reasoning", InputCost: 1.1, OutputCost: 4.4, CacheCost: 0.275, ContextWindow: 200000},
	{ID: "openai/o4-mini", Provider: "openai", ModelID: "o4-mini", Name: "o4 Mini", Tier: "reasoning", InputCost: 1.1, OutputCost: 4.4, CacheCost: 0.275, ContextWindow: 200000},

	// --- Google Gemini ---
	{ID: "gemini/gemini-2.5-pro", Provider: "gemini", ModelID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", Tier: "strong", InputCost: 1.25, OutputCost: 10.0, CacheCost: 0.315, ContextWindow: 1000000},
	{ID: "gemini/gemini-2.5-flash", Provider: "gemini", ModelID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", Tier: "fast", InputCost: 0.15, OutputCost: 0.6, CacheCost: 0.0375, ContextWindow: 1000000},
	{ID: "gemini/gemini-2.0-flash", Provider: "gemini", ModelID: "gemini-2.0-flash", Name: "Gemini 2.0 Flash", Tier: "fast", InputCost: 0.1, OutputCost: 0.4, CacheCost: 0.025, ContextWindow: 1000000},
	{ID: "gemini/gemini-2.0-flash-lite", Provider: "gemini", ModelID: "gemini-2.0-flash-lite", Name: "Gemini 2.0 Flash Lite", Tier: "fast", InputCost: 0.075, OutputCost: 0.3, CacheCost: 0.01875, ContextWindow: 1000000},

	// --- OpenRouter (popular models via OpenRouter) ---
	{ID: "openrouter/anthropic/claude-sonnet-4", Provider: "openrouter", ModelID: "anthropic/claude-sonnet-4-20250514", Name: "Claude Sonnet 4 (via OpenRouter)", Tier: "strong", InputCost: 3.0, OutputCost: 15.0, ContextWindow: 200000},
	{ID: "openrouter/openai/gpt-4o", Provider: "openrouter", ModelID: "openai/gpt-4o", Name: "GPT-4o (via OpenRouter)", Tier: "strong", InputCost: 2.5, OutputCost: 10.0, ContextWindow: 128000},
	{ID: "openrouter/google/gemini-2.5-pro", Provider: "openrouter", ModelID: "google/gemini-2.5-pro-preview", Name: "Gemini 2.5 Pro (via OpenRouter)", Tier: "strong", InputCost: 1.25, OutputCost: 10.0, ContextWindow: 1000000},
	{ID: "openrouter/meta-llama/llama-4-maverick", Provider: "openrouter", ModelID: "meta-llama/llama-4-maverick", Name: "Llama 4 Maverick (via OpenRouter)", Tier: "strong", InputCost: 0.5, OutputCost: 0.7, ContextWindow: 1000000},
	{ID: "openrouter/deepseek/deepseek-r1", Provider: "openrouter", ModelID: "deepseek/deepseek-r1", Name: "DeepSeek R1 (via OpenRouter)", Tier: "reasoning", InputCost: 0.55, OutputCost: 2.19, ContextWindow: 163840},
	{ID: "openrouter/qwen/qwen3-235b", Provider: "openrouter", ModelID: "qwen/qwen3-235b-a22b", Name: "Qwen 3 235B (via OpenRouter)", Tier: "strong", InputCost: 0.2, OutputCost: 1.2, ContextWindow: 131072},

	// --- GitHub Copilot ---
	{ID: "github/gpt-4o", Provider: "github", ModelID: "gpt-4o", Name: "GPT-4o (via GitHub)", Tier: "strong", InputCost: 0, OutputCost: 0, ContextWindow: 128000},
	{ID: "github/claude-sonnet-4", Provider: "github", ModelID: "claude-sonnet-4-20250514", Name: "Claude Sonnet 4 (via GitHub)", Tier: "strong", InputCost: 0, OutputCost: 0, ContextWindow: 200000},

	// --- Ollama (local, free) ---
	{ID: "ollama/llama3.2:3b", Provider: "ollama", ModelID: "llama3.2:3b", Name: "Llama 3.2 3B", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 128000},
	{ID: "ollama/llama3.2:1b", Provider: "ollama", ModelID: "llama3.2:1b", Name: "Llama 3.2 1B", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 128000},
	{ID: "ollama/mistral", Provider: "ollama", ModelID: "mistral", Name: "Mistral 7B", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 32000},
	{ID: "ollama/qwen2.5:7b", Provider: "ollama", ModelID: "qwen2.5:7b", Name: "Qwen 2.5 7B", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 128000},
	{ID: "ollama/phi3", Provider: "ollama", ModelID: "phi3", Name: "Phi-3", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 128000},
	{ID: "ollama/gemma2", Provider: "ollama", ModelID: "gemma2", Name: "Gemma 2", Tier: "local", InputCost: 0, OutputCost: 0, ContextWindow: 8192},
}

// ModelsForProvider returns models available for a specific provider.
func ModelsForProvider(provider string) []ModelInfo {
	var result []ModelInfo
	for _, m := range KnownModels {
		if m.Provider == provider {
			result = append(result, m)
		}
	}
	return result
}

// FindModel looks up a model by its full ID (e.g. "anthropic/claude-sonnet-4-20250514")
// or raw model ID (e.g. "claude-sonnet-4-20250514").
func FindModel(id string) *ModelInfo {
	for _, m := range KnownModels {
		if m.ID == id || m.ModelID == id {
			return &m
		}
	}
	return nil
}

// EstimateCost calculates the cost for a request given token counts.
func EstimateCost(model *ModelInfo, inputTokens, outputTokens, cacheReadTokens int) float64 {
	if model == nil {
		return 0
	}
	inputCost := float64(inputTokens) / 1_000_000 * model.InputCost
	outputCost := float64(outputTokens) / 1_000_000 * model.OutputCost
	// Cache reads are cheaper — use cache cost for cached tokens.
	cacheSavings := 0.0
	if model.CacheCost > 0 && cacheReadTokens > 0 {
		cacheSavings = float64(cacheReadTokens) / 1_000_000 * (model.InputCost - model.CacheCost)
	}
	return inputCost + outputCost - cacheSavings
}
