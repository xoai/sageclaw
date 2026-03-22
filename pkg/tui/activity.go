package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
)

const maxEvents = 200

type activityModel struct {
	events []eventEntry
	offset int
}

type eventEntry struct {
	time  time.Time
	event agent.Event
}

func (m *activityModel) addEvent(e agent.Event) {
	m.events = append(m.events, eventEntry{time: time.Now(), event: e})
	if len(m.events) > maxEvents {
		m.events = m.events[len(m.events)-maxEvents:]
	}
}

func (m *activityModel) view(width, height int) string {
	if len(m.events) == 0 {
		return subtitleStyle.Render("No activity yet. Waiting for messages...")
	}

	var lines []string
	// Show most recent events that fit.
	start := len(m.events) - height
	if start < 0 {
		start = 0
	}

	for _, entry := range m.events[start:] {
		line := m.formatEvent(entry)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

func (m *activityModel) formatEvent(entry eventEntry) string {
	ts := entry.time.Format("15:04:05")
	e := entry.event

	switch e.Type {
	case agent.EventRunStarted:
		return eventStart.Render(fmt.Sprintf("[%s] ▶ run started  session=%s", ts, short(e.SessionID)))
	case agent.EventRunCompleted:
		return eventDone.Render(fmt.Sprintf("[%s] ✓ run complete session=%s", ts, short(e.SessionID)))
	case agent.EventRunFailed:
		return eventError.Render(fmt.Sprintf("[%s] ✗ run failed   session=%s error=%v", ts, short(e.SessionID), e.Error))
	case agent.EventToolCall:
		name := ""
		if e.ToolCall != nil {
			name = e.ToolCall.Name
		}
		return eventToolCall.Render(fmt.Sprintf("[%s] ⚡ tool call   %s", ts, name))
	case agent.EventToolResult:
		status := "ok"
		if e.ToolResult != nil && e.ToolResult.IsError {
			status = "error"
		}
		return eventToolResult.Render(fmt.Sprintf("[%s] ← tool result %s", ts, status))
	case agent.EventChunk:
		text := e.Text
		if len(text) > 60 {
			text = text[:60] + "..."
		}
		return eventChunk.Render(fmt.Sprintf("[%s]   %s", ts, text))
	default:
		return fmt.Sprintf("[%s] %s", ts, e.Type)
	}
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
