package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

const maxSessionHistoryBytes = 80_000 // ~20K tokens

// RegisterSessions registers session introspection tools.
func RegisterSessions(reg *Registry, s store.Store) {
	reg.RegisterWithGroup("sessions_list", "List recent sessions",
		json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","description":"Max sessions to return (default 20)"}}}`),
		GroupSessions, RiskSafe, "builtin", sessionsList(s))

	reg.RegisterWithGroup("session_status", "Get session metadata",
		json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Session ID"}},"required":["session_id"]}`),
		GroupSessions, RiskSafe, "builtin", sessionStatus(s))

	reg.RegisterWithGroup("sessions_history", "Get message history from a session",
		json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Session ID"},"limit":{"type":"integer","description":"Max messages (default 50)"}},"required":["session_id"]}`),
		GroupSessions, RiskSafe, "builtin", sessionsHistory(s))

	reg.RegisterWithGroup("sessions_send", "Send a message to a session",
		json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string","description":"Session ID"},"content":{"type":"string","description":"Message content"}},"required":["session_id","content"]}`),
		GroupSessions, RiskModerate, "builtin", sessionsSend(s))
}

func sessionsList(s store.Store) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Limit int `json:"limit"`
		}
		json.Unmarshal(input, &params)
		if params.Limit <= 0 {
			params.Limit = 20
		}

		sessions, err := s.ListSessions(ctx, params.Limit)
		if err != nil {
			return errorResult("failed to list sessions: " + err.Error()), nil
		}
		if len(sessions) == 0 {
			return &canonical.ToolResult{Content: "No sessions found."}, nil
		}

		var sb strings.Builder
		for _, sess := range sessions {
			label := sess.Label
			if label == "" {
				label = "(untitled)"
			}
			fmt.Fprintf(&sb, "- **%s** [%s] agent=%s channel=%s kind=%s msgs=%d updated=%s\n  %s\n",
				sess.ID[:8], sess.Status, sess.AgentID, sess.Channel, sess.Kind,
				sess.MessageCount, sess.UpdatedAt.Format("2006-01-02 15:04"), label)
		}
		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func sessionStatus(s store.Store) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		sess, err := s.GetSession(ctx, params.SessionID)
		if err != nil {
			return errorResult("session not found: " + err.Error()), nil
		}

		data, _ := json.MarshalIndent(map[string]any{
			"id":               sess.ID,
			"agent_id":         sess.AgentID,
			"channel":          sess.Channel,
			"chat_id":          sess.ChatID,
			"kind":             sess.Kind,
			"label":            sess.Label,
			"status":           sess.Status,
			"model":            sess.Model,
			"provider":         sess.Provider,
			"message_count":    sess.MessageCount,
			"input_tokens":     sess.InputTokens,
			"output_tokens":    sess.OutputTokens,
			"compaction_count": sess.CompactionCount,
			"created_at":       sess.CreatedAt,
			"updated_at":       sess.UpdatedAt,
		}, "", "  ")

		return &canonical.ToolResult{Content: string(data)}, nil
	}
}

func sessionsHistory(s store.Store) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			SessionID string `json:"session_id"`
			Limit     int    `json:"limit"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}
		if params.Limit <= 0 {
			params.Limit = 50
		}

		msgs, err := s.GetMessages(ctx, params.SessionID, params.Limit)
		if err != nil {
			return errorResult("failed to get messages: " + err.Error()), nil
		}
		if len(msgs) == 0 {
			return &canonical.ToolResult{Content: "No messages in this session."}, nil
		}

		var sb strings.Builder
		for _, msg := range msgs {
			fmt.Fprintf(&sb, "### %s\n", msg.Role)
			for _, c := range msg.Content {
				if c.Type == "text" {
					sb.WriteString(c.Text)
					sb.WriteString("\n\n")
				} else if c.Type == "tool_use" {
					fmt.Fprintf(&sb, "[tool_use: %s]\n\n", c.ToolName)
				} else if c.Type == "tool_result" {
					fmt.Fprintf(&sb, "[tool_result: %s]\n\n", c.ToolName)
				}
			}
			// Truncate if too large.
			if sb.Len() > maxSessionHistoryBytes {
				sb.WriteString("\n... [truncated at 80KB]")
				break
			}
		}

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func sessionsSend(s store.Store) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			SessionID string `json:"session_id"`
			Content   string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Verify session exists.
		sess, err := s.GetSession(ctx, params.SessionID)
		if err != nil {
			return errorResult("session not found: " + err.Error()), nil
		}

		// Append message as a user message to the session.
		msgs := []canonical.Message{
			{
				Role: "user",
				Content: []canonical.Content{
					{Type: "text", Text: params.Content},
				},
			},
		}
		if err := s.AppendMessages(ctx, sess.ID, msgs); err != nil {
			return errorResult("failed to send message: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Message sent to session %s", sess.ID[:8])}, nil
	}
}
