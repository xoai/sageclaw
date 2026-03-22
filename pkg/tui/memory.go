package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/memory"
)

type memoryModel struct {
	engine  memory.MemoryEngine
	entries []memory.Entry
	query   string
}

func (m *memoryModel) refresh() {
	if m.engine == nil {
		return
	}
	ctx := context.Background()
	entries, err := m.engine.List(ctx, nil, 20, 0)
	if err != nil {
		return
	}
	m.entries = entries
}

func (m *memoryModel) search(query string) {
	if m.engine == nil || query == "" {
		m.refresh()
		return
	}
	ctx := context.Background()
	entries, err := m.engine.Search(ctx, query, memory.SearchOptions{Limit: 20})
	if err != nil {
		return
	}
	m.entries = entries
	m.query = query
}

func (m *memoryModel) view(width, height int) string {
	if len(m.entries) == 0 {
		return subtitleStyle.Render("No memories stored.")
	}

	var lines []string
	header := "Recent Memories"
	if m.query != "" {
		header = fmt.Sprintf("Search: %q", m.query)
	}
	lines = append(lines, titleStyle.Render(header))
	lines = append(lines, strings.Repeat("─", width))

	for _, e := range m.entries {
		title := e.Title
		if len(title) > 40 {
			title = title[:40] + "..."
		}
		tags := ""
		if len(e.Tags) > 0 {
			tags = "[" + strings.Join(e.Tags, ", ") + "]"
		}
		content := e.Content
		if len(content) > 60 {
			content = content[:60] + "..."
		}
		scoreStr := ""
		if e.Score > 0 {
			scoreStr = fmt.Sprintf(" (%.2f)", e.Score)
		}
		lines = append(lines, fmt.Sprintf("%s %s%s", titleStyle.Render(title), subtitleStyle.Render(tags), scoreStr))
		lines = append(lines, fmt.Sprintf("  %s", content))
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}
