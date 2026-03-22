package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

type sessionsModel struct {
	sessions []sessionInfo
	store    *sqlite.Store
}

type sessionInfo struct {
	ID        string
	Channel   string
	ChatID    string
	MsgCount  int
	LastMsg   string
}

func (m *sessionsModel) refresh() {
	if m.store == nil {
		return
	}
	ctx := context.Background()
	rows, err := m.store.DB().QueryContext(ctx,
		`SELECT s.id, s.channel, s.chat_id,
			(SELECT COUNT(*) FROM messages WHERE session_id = s.id) as msg_count,
			COALESCE((SELECT content FROM messages WHERE session_id = s.id ORDER BY id DESC LIMIT 1), '')
		 FROM sessions s ORDER BY s.updated_at DESC LIMIT 20`)
	if err != nil {
		return
	}
	defer rows.Close()

	m.sessions = nil
	for rows.Next() {
		var si sessionInfo
		var lastContent string
		rows.Scan(&si.ID, &si.Channel, &si.ChatID, &si.MsgCount, &lastContent)
		if len(lastContent) > 50 {
			lastContent = lastContent[:50] + "..."
		}
		si.LastMsg = lastContent
		m.sessions = append(m.sessions, si)
	}
}

func (m *sessionsModel) view(width, height int) string {
	if len(m.sessions) == 0 {
		return subtitleStyle.Render("No sessions yet.")
	}

	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("%-8s  %-10s  %-12s  %4s  %s", "ID", "Channel", "Chat", "Msgs", "Last Message")))
	lines = append(lines, strings.Repeat("─", width))

	for _, s := range m.sessions {
		lines = append(lines, fmt.Sprintf("%-8s  %-10s  %-12s  %4d  %s",
			short(s.ID), s.Channel, short(s.ChatID), s.MsgCount, s.LastMsg))
	}

	return strings.Join(lines, "\n")
}
