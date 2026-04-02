package provider

import "strings"

// Capabilities describes what a specific model can do.
// Used by the agent loop for optimization (e.g., skip tools for non-tool models,
// adjust max_tokens for context window, enable thinking for supported models).
type Capabilities struct {
	ContextWindow   int            `json:"context_window"`             // Max input tokens.
	MaxOutputTokens int            `json:"max_output_tokens,omitempty"` // Max output tokens (0 = provider default).
	Thinking        bool           `json:"thinking,omitempty"`         // Supports extended thinking / reasoning.
	Caching         bool           `json:"caching,omitempty"`          // Supports prompt caching.
	Tools           bool           `json:"tools,omitempty"`            // Supports tool/function calling.
	Streaming       bool           `json:"streaming,omitempty"`        // Supports streaming responses.
	Vision          bool           `json:"vision,omitempty"`           // Can analyze images.
	ImageGen        bool           `json:"image_gen,omitempty"`        // Can generate images.
	TTS             bool           `json:"tts,omitempty"`              // Can generate speech.
	SearchGrounding bool           `json:"search_grounding,omitempty"` // Supports search grounding (Google Search, web search).
	CodeExecution   bool           `json:"code_execution,omitempty"`   // Supports native code execution.
	JSON            bool           `json:"json,omitempty"`             // Supports JSON mode / structured output.
	Features        map[string]any `json:"features,omitempty"`         // Provider-specific features.
}

// ModelCapabilities is an optional interface for providers that can report
// per-model capabilities. If a provider doesn't implement this, consumers
// should fall back to DefaultCapabilities or the KnownModels registry.
//
// Note: this supersedes the older ProviderCapabilities interface (Supports(cap))
// which reports provider-level capabilities. Prefer GetCapabilities() for
// per-model resolution. ProviderCapabilities is retained for backward compat
// with media.go and will be unified in a future refactor.
type ModelCapabilities interface {
	GetModelCapabilities(model string) Capabilities
}

// GetCapabilities returns capabilities for a model on a given provider.
// Resolution order:
//  1. Provider implements ModelCapabilities → use it
//  2. KnownModelCapabilities registry has an entry → use it
//  3. Default capabilities
func GetCapabilities(p Provider, model string) Capabilities {
	if mc, ok := p.(ModelCapabilities); ok {
		return mc.GetModelCapabilities(model)
	}
	if caps, ok := LookupModelCapabilities(model); ok {
		return caps
	}
	return DefaultCapabilities()
}

// DefaultCapabilities returns conservative defaults for unknown models.
func DefaultCapabilities() Capabilities {
	return Capabilities{
		ContextWindow: 8192,
		Tools:         true,
		Streaming:     true,
		JSON:          true,
	}
}

// LookupModelCapabilities checks the known model capabilities registry.
// Returns the capabilities and true if found, zero value and false if not.
// When multiple prefixes match, the longest prefix wins (most specific).
func LookupModelCapabilities(model string) (Capabilities, bool) {
	// Exact match first.
	if caps, ok := knownModelCaps[model]; ok {
		return caps, true
	}
	// Longest-prefix match (e.g., "gpt-4o-mini-2025" matches "gpt-4o-mini" not "gpt-4o").
	var bestPrefix string
	var bestCaps Capabilities
	for prefix, caps := range knownModelCaps {
		if strings.HasPrefix(model, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
			bestCaps = caps
		}
	}
	if bestPrefix != "" {
		return bestCaps, true
	}
	return Capabilities{}, false
}

// knownModelCaps maps model ID prefixes to their capabilities.
// Uses the shortest unambiguous prefix for each model family.
var knownModelCaps = map[string]Capabilities{
	// --- Anthropic ---
	"claude-opus-4": {
		ContextWindow: 200000, MaxOutputTokens: 32000,
		Thinking: true, Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
	"claude-sonnet-4": {
		ContextWindow: 200000, MaxOutputTokens: 16000,
		Thinking: true, Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
	"claude-haiku-4": {
		ContextWindow: 200000, MaxOutputTokens: 8192,
		Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},

	// --- OpenAI ---
	"gpt-4.1": {
		ContextWindow: 1000000, MaxOutputTokens: 32768,
		Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true, ImageGen: false,
	},
	"gpt-4.1-mini": {
		ContextWindow: 1000000, MaxOutputTokens: 16384,
		Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
	"gpt-4.1-nano": {
		ContextWindow: 1000000, MaxOutputTokens: 8192,
		Caching: true, Tools: true, Streaming: true,
		JSON: true,
	},
	"gpt-4o": {
		ContextWindow: 128000, MaxOutputTokens: 16384,
		Tools: true, Streaming: true,
		Vision: true, JSON: true, ImageGen: false,
	},
	"gpt-4o-mini": {
		ContextWindow: 128000, MaxOutputTokens: 16384,
		Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
	"o3": {
		ContextWindow: 200000, MaxOutputTokens: 100000,
		Thinking: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
	"o3-mini": {
		ContextWindow: 200000, MaxOutputTokens: 65536,
		Thinking: true, Tools: true, Streaming: true,
		JSON: true,
	},
	"o4-mini": {
		ContextWindow: 200000, MaxOutputTokens: 100000,
		Thinking: true, Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},

	// --- Gemini ---
	"gemini-2.5-pro": {
		ContextWindow: 1000000, MaxOutputTokens: 65536,
		Thinking: true, Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true, SearchGrounding: true, CodeExecution: true,
	},
	"gemini-2.5-flash": {
		ContextWindow: 1000000, MaxOutputTokens: 65536,
		Thinking: true, Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true, SearchGrounding: true, CodeExecution: true,
	},
	"gemini-2.0-flash": {
		ContextWindow: 1000000, MaxOutputTokens: 8192,
		Caching: true, Tools: true, Streaming: true,
		Vision: true, JSON: true, SearchGrounding: true, CodeExecution: true,
	},
	"gemini-2.5-flash-lite": {
		ContextWindow: 1000000, MaxOutputTokens: 8192,
		Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},

	// --- DeepSeek ---
	"deepseek-reasoner": {
		ContextWindow: 64000, MaxOutputTokens: 8192,
		Thinking: true, Tools: false, Streaming: true,
		JSON: true,
	},
	"deepseek-chat": {
		ContextWindow: 64000, MaxOutputTokens: 8192,
		Tools: true, Streaming: true,
		JSON: true,
	},

	// --- xAI ---
	"grok-3": {
		ContextWindow: 131072, MaxOutputTokens: 16384,
		Thinking: true, Tools: true, Streaming: true,
		Vision: true, JSON: true, SearchGrounding: true,
	},

	// --- Meta (via OpenRouter/Groq/Together) ---
	"llama-4": {
		ContextWindow: 1000000, MaxOutputTokens: 16384,
		Tools: true, Streaming: true,
		Vision: true, JSON: true,
	},
}
