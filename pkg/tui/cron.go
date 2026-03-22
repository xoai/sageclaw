package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

type cronModel struct {
	jobs  []sqlite.CronJob
	store *sqlite.Store
}

func (m *cronModel) refresh() {
	if m.store == nil {
		return
	}
	ctx := context.Background()
	jobs, err := m.store.ListCronJobs(ctx)
	if err != nil {
		return
	}
	m.jobs = jobs
}

func (m *cronModel) view(width, height int) string {
	if len(m.jobs) == 0 {
		return subtitleStyle.Render("No cron jobs configured.")
	}

	var lines []string
	lines = append(lines, titleStyle.Render(fmt.Sprintf("%-8s  %-10s  %-15s  %s", "ID", "Status", "Schedule", "Prompt")))
	lines = append(lines, strings.Repeat("─", width))

	for _, j := range m.jobs {
		status := eventStart.Render("enabled")
		if !j.Enabled {
			status = subtitleStyle.Render("disabled")
		}
		prompt := j.Prompt
		if len(prompt) > 40 {
			prompt = prompt[:40] + "..."
		}
		lines = append(lines, fmt.Sprintf("%-8s  %-10s  %-15s  %s",
			short(j.ID), status, j.Schedule, prompt))
	}

	return strings.Join(lines, "\n")
}
