package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// DelegateFunc is the function signature for delegation (breaks import cycle with orchestration).
type DelegateFunc func(ctx context.Context, sourceID, targetID, prompt, mode string) (recordID string, result string, err error)

// DelegateStatusFunc checks delegation status.
type DelegateStatusFunc func(ctx context.Context, delegationID string) (status, sourceID, targetID, prompt, result string, err error)

// RegisterDelegate registers delegation tools.
func RegisterDelegate(reg *Registry, delegateFn DelegateFunc, statusFn DelegateStatusFunc) {
	reg.RegisterWithGroup("delegate", "Delegate a task to another agent",
		json.RawMessage(`{"type":"object","properties":{"agent_id":{"type":"string","description":"Target agent ID"},"prompt":{"type":"string","description":"Task prompt for the target agent"},"mode":{"type":"string","description":"sync (wait for result) or async (return immediately)","default":"sync"}},"required":["agent_id","prompt"]}`),
		GroupOrchestration, RiskSensitive, "builtin", delegateToolFn(delegateFn))

	reg.RegisterWithGroup("delegation_status", "Check the status of an async delegation",
		json.RawMessage(`{"type":"object","properties":{"delegation_id":{"type":"string","description":"Delegation record ID"}},"required":["delegation_id"]}`),
		GroupOrchestration, RiskSensitive, "builtin", delegationStatusFn(statusFn))
}

func delegateToolFn(delegateFn DelegateFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			AgentID string `json:"agent_id"`
			Prompt  string `json:"prompt"`
			Mode    string `json:"mode"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Extract source agent from context (set by agent loop).
		sourceID, _ := ctx.Value(agentIDKey{}).(string)
		if sourceID == "" {
			sourceID = "default"
		}

		recordID, result, err := delegateFn(ctx, sourceID, params.AgentID, params.Prompt, params.Mode)
		if err != nil {
			return errorResult("delegation failed: " + err.Error()), nil
		}

		if result != "" {
			return &canonical.ToolResult{Content: fmt.Sprintf("Delegation complete (id: %s).\n\nResult:\n%s", recordID, result)}, nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Delegation started (async). ID: %s\nUse delegation_status to check progress.", recordID)}, nil
	}
}

func delegationStatusFn(statusFn DelegateStatusFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			DelegationID string `json:"delegation_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		status, sourceID, targetID, prompt, result, err := statusFn(ctx, params.DelegationID)
		if err != nil {
			return errorResult("status check failed: " + err.Error()), nil
		}

		content := fmt.Sprintf("Delegation: status=%s\nFrom: %s → To: %s\nPrompt: %s",
			status, sourceID, targetID, prompt)
		if result != "" {
			content += "\n\nResult:\n" + result
		}

		return &canonical.ToolResult{Content: content}, nil
	}
}

type agentIDKey struct{}

// WithAgentID adds the current agent ID to context.
func WithAgentID(ctx context.Context, agentID string) context.Context {
	return context.WithValue(ctx, agentIDKey{}, agentID)
}
