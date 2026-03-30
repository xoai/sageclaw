package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// RegisterMessage registers the proactive message tool.
// Agents can only send to channels they are bound to.
func RegisterMessage(reg *Registry, s store.Store, msgBus bus.MessageBus) {
	reg.RegisterWithGroup("message", "Send a proactive message to a channel the agent is bound to",
		json.RawMessage(`{"type":"object","properties":{`+
			`"channel":{"type":"string","description":"Connection ID (e.g. tg_abc123)"},`+
			`"chat_id":{"type":"string","description":"Chat/user ID on that channel"},`+
			`"content":{"type":"string","description":"Message text"}`+
			`},"required":["channel","chat_id","content"]}`),
		GroupMessaging, RiskModerate, "builtin", messageTool(s, msgBus))
}

func messageTool(s store.Store, msgBus bus.MessageBus) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Channel string `json:"channel"`
			ChatID  string `json:"chat_id"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Verify agent is bound to this channel.
		agentID, _ := ctx.Value(agentIDKey{}).(string)
		conn, err := s.GetConnection(ctx, params.Channel)
		if err != nil {
			return errorResult("connection not found: " + err.Error()), nil
		}
		if conn.AgentID != agentID {
			return errorResult(fmt.Sprintf(
				"agent %q is not bound to connection %q (bound to %q)",
				agentID, params.Channel, conn.AgentID)), nil
		}

		// Publish outbound message via bus.
		env := bus.Envelope{
			AgentID: agentID,
			Channel: params.Channel,
			ChatID:  params.ChatID,
			Messages: []canonical.Message{
				{
					Role: "assistant",
					Content: []canonical.Content{
						{Type: "text", Text: params.Content},
					},
				},
			},
		}
		if err := msgBus.PublishOutbound(ctx, env); err != nil {
			return errorResult("failed to send message: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Message sent to %s/%s", params.Channel, params.ChatID)}, nil
	}
}
