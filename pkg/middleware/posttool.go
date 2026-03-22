package middleware

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/xoai/sageclaw/pkg/security"
)

// DBProvider is anything with a DB() method (store.Store satisfies this).
type DBProvider interface {
	DB() *sql.DB
}

// PostToolAudit logs tool calls and results to the audit trail.
func PostToolAudit(store DBProvider) Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPostTool {
			return next(ctx, data)
		}

		// Call next first, then audit (allows later middleware to modify result).
		err := next(ctx, data)

		// Log the tool call.
		if data.ToolCall != nil {
			scrubbedInput := security.Scrub(string(data.ToolCall.Input))
			payload, _ := json.Marshal(map[string]any{
				"tool":       data.ToolCall.Name,
				"input":      json.RawMessage(scrubbedInput),
				"has_result":  data.ToolResult != nil,
				"is_error":   data.ToolResult != nil && data.ToolResult.IsError,
				"timestamp":  time.Now().UTC().Format(time.RFC3339),
			})

			sessionID := ""
			if data.Metadata != nil {
				if sid, ok := data.Metadata["session_id"].(string); ok {
					sessionID = sid
				}
			}
			agentID := ""
			if data.Metadata != nil {
				if aid, ok := data.Metadata["agent_id"].(string); ok {
					agentID = aid
				}
			}

			store.DB().ExecContext(ctx,
				`INSERT INTO audit_log (session_id, agent_id, event_type, payload) VALUES (?, ?, ?, ?)`,
				sessionID, agentID, "tool.call", string(payload),
			)
		}

		return err
	}
}

// PostToolScrub removes potential secrets from tool results.
func PostToolScrub() Middleware {
	return func(ctx context.Context, data *HookData, next NextFunc) error {
		err := next(ctx, data)

		if data.ToolResult != nil {
			data.ToolResult.Content = security.Scrub(data.ToolResult.Content)
		}

		return err
	}
}
