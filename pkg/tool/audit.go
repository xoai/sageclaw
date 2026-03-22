package tool

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// RegisterAudit registers audit query tools.
func RegisterAudit(reg *Registry, db *sql.DB) {
	reg.Register("audit_search", "Search the audit log",
		json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"agent_id":{"type":"string"},"event_type":{"type":"string"},"since":{"type":"string","description":"ISO timestamp"},"limit":{"type":"integer","default":20}}}`),
		auditSearch(db))

	reg.Register("audit_stats", "Get audit statistics",
		json.RawMessage(`{"type":"object","properties":{"since":{"type":"string","description":"ISO timestamp"}}}`),
		auditStats(db))
}

func auditSearch(db *sql.DB) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			SessionID string `json:"session_id"`
			AgentID   string `json:"agent_id"`
			EventType string `json:"event_type"`
			Since     string `json:"since"`
			Limit     int    `json:"limit"`
		}
		json.Unmarshal(input, &p)
		if p.Limit <= 0 {
			p.Limit = 20
		}

		query := `SELECT session_id, agent_id, event_type, payload, created_at FROM audit_log WHERE 1=1`
		var args []any
		argN := 1

		if p.SessionID != "" {
			query += fmt.Sprintf(` AND session_id = ?`)
			args = append(args, p.SessionID)
		}
		if p.AgentID != "" {
			query += fmt.Sprintf(` AND agent_id = ?`)
			args = append(args, p.AgentID)
		}
		if p.EventType != "" {
			query += fmt.Sprintf(` AND event_type = ?`)
			args = append(args, p.EventType)
		}
		if p.Since != "" {
			query += fmt.Sprintf(` AND created_at >= ?`)
			args = append(args, p.Since)
		}
		_ = argN

		query += fmt.Sprintf(` ORDER BY created_at DESC LIMIT ?`)
		args = append(args, p.Limit)

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return errorResult("query failed: " + err.Error()), nil
		}
		defer rows.Close()

		var sb strings.Builder
		count := 0
		for rows.Next() {
			var sessionID, agentID, eventType, payload, createdAt string
			rows.Scan(&sessionID, &agentID, &eventType, &payload, &createdAt)
			fmt.Fprintf(&sb, "[%s] %s session=%s agent=%s\n  %s\n\n",
				createdAt, eventType, sessionID[:min(8, len(sessionID))], agentID, truncate(payload, 100))
			count++
		}

		if count == 0 {
			return &canonical.ToolResult{Content: "No audit entries found."}, nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("%d entries:\n\n%s", count, sb.String())}, nil
	}
}

func auditStats(db *sql.DB) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			Since string `json:"since"`
		}
		json.Unmarshal(input, &p)

		query := `SELECT event_type, COUNT(*) as cnt FROM audit_log`
		var args []any
		if p.Since != "" {
			query += ` WHERE created_at >= ?`
			args = append(args, p.Since)
		}
		query += ` GROUP BY event_type ORDER BY cnt DESC`

		rows, err := db.QueryContext(ctx, query, args...)
		if err != nil {
			return errorResult("query failed: " + err.Error()), nil
		}
		defer rows.Close()

		var sb strings.Builder
		sb.WriteString("Audit Statistics:\n\n")
		total := 0
		for rows.Next() {
			var eventType string
			var count int
			rows.Scan(&eventType, &count)
			fmt.Fprintf(&sb, "  %-20s %d\n", eventType, count)
			total += count
		}
		fmt.Fprintf(&sb, "\n  %-20s %d\n", "TOTAL", total)

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
