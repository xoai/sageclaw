package security

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// GuardrailProvider evaluates tool calls before execution.
// Implement this interface to add custom security policies.
type GuardrailProvider interface {
	// EvaluateToolCall checks whether a tool call should be allowed.
	// Returns (true, "") to allow, or (false, reason) to deny.
	EvaluateToolCall(ctx context.Context, toolName string, input json.RawMessage) (allow bool, reason string)

	// Name returns the provider name for logging.
	Name() string
}

// GuardrailChain evaluates tool calls against multiple providers in order.
// All providers must allow the call for it to proceed.
type GuardrailChain struct {
	providers []GuardrailProvider
}

// NewGuardrailChain creates a chain of guardrail providers.
func NewGuardrailChain(providers ...GuardrailProvider) *GuardrailChain {
	return &GuardrailChain{providers: providers}
}

// Add appends a provider to the chain.
func (c *GuardrailChain) Add(p GuardrailProvider) {
	c.providers = append(c.providers, p)
}

// Evaluate checks all providers. Returns (true, "") if all allow,
// or (false, reason) with the first denial reason.
func (c *GuardrailChain) Evaluate(ctx context.Context, toolName string, input json.RawMessage) (bool, string) {
	for _, p := range c.providers {
		allow, reason := p.EvaluateToolCall(ctx, toolName, input)
		if !allow {
			log.Printf("guardrail: %s denied %s: %s", p.Name(), toolName, reason)
			return false, reason
		}
	}
	return true, ""
}

// --- Built-in providers ---

// ShellDenyProvider wraps the existing deny pattern groups as a GuardrailProvider.
type ShellDenyProvider struct {
	disabledGroups map[string]bool
}

// NewShellDenyProvider creates a guardrail from existing shell deny patterns.
func NewShellDenyProvider(disabledGroups map[string]bool) *ShellDenyProvider {
	return &ShellDenyProvider{disabledGroups: disabledGroups}
}

func (p *ShellDenyProvider) Name() string { return "shell-deny" }

func (p *ShellDenyProvider) EvaluateToolCall(ctx context.Context, toolName string, input json.RawMessage) (bool, string) {
	// Only check shell execution tools.
	if toolName != "exec" && toolName != "shell" {
		return true, ""
	}

	// Extract command from input.
	var params struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &params); err != nil || params.Command == "" {
		return true, "" // Can't parse — let the tool handle validation.
	}

	if err := CheckCommand(params.Command, p.disabledGroups); err != nil {
		return false, err.Error()
	}
	return true, ""
}

// ToolAllowlistProvider only allows specific tools to execute.
type ToolAllowlistProvider struct {
	allowed map[string]bool
}

// NewToolAllowlistProvider creates a guardrail that only allows listed tools.
func NewToolAllowlistProvider(tools []string) *ToolAllowlistProvider {
	m := make(map[string]bool, len(tools))
	for _, t := range tools {
		m[t] = true
	}
	return &ToolAllowlistProvider{allowed: m}
}

func (p *ToolAllowlistProvider) Name() string { return "tool-allowlist" }

func (p *ToolAllowlistProvider) EvaluateToolCall(ctx context.Context, toolName string, input json.RawMessage) (bool, string) {
	if len(p.allowed) == 0 {
		return true, "" // Empty allowlist = allow all.
	}
	if p.allowed[toolName] {
		return true, ""
	}
	return false, fmt.Sprintf("tool %q not in allowlist", toolName)
}
