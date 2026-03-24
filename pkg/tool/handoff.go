package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// HandoffFunc transfers a conversation to another agent.
type HandoffFunc func(ctx context.Context, sessionID, sourceAgentID, targetAgentID, reason string) error

// RegisterHandoff registers the handoff tool.
func RegisterHandoff(reg *Registry, handoffFn HandoffFunc) {
	reg.RegisterWithGroup("handoff", "Transfer this conversation to another agent",
		json.RawMessage(`{"type":"object","properties":{"target_agent_id":{"type":"string","description":"Agent to hand off to"},"reason":{"type":"string","description":"Why the handoff is happening"}},"required":["target_agent_id"]}`),
		GroupOrchestration, RiskSensitive, "builtin", handoffTool(handoffFn))
}

func handoffTool(handoffFn HandoffFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TargetAgentID string `json:"target_agent_id"`
			Reason        string `json:"reason"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		sessionID, _ := ctx.Value(sessionIDKey{}).(string)
		agentID, _ := ctx.Value(agentIDKey{}).(string)

		if err := handoffFn(ctx, sessionID, agentID, p.TargetAgentID, p.Reason); err != nil {
			return errorResult("handoff failed: " + err.Error()), nil
		}

		return &canonical.ToolResult{
			Content: fmt.Sprintf("Conversation handed off to %s. The next message will be handled by the new agent.", p.TargetAgentID),
		}, nil
	}
}

type sessionIDKey struct{}

// WithSessionID adds the session ID to context.
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, sessionIDKey{}, sessionID)
}
