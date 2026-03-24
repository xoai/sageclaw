package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// RegisterCron registers cron management tools.
func RegisterCron(reg *Registry, store store.CronStore) {
	reg.RegisterWithGroup("cron_create", "Create a scheduled cron job",
		json.RawMessage(`{"type":"object","properties":{"schedule":{"type":"string","description":"Schedule expression: @every 5m, @hourly, @daily, or */N * * * *"},"prompt":{"type":"string","description":"Prompt to send to the agent on schedule"},"agent_id":{"type":"string","description":"Agent ID (default: current agent)"}},"required":["schedule","prompt"]}`),
		GroupCron, RiskModerate, "builtin", cronCreate(store))

	reg.RegisterWithGroup("cron_list", "List active cron jobs",
		json.RawMessage(`{"type":"object","properties":{}}`),
		GroupCron, RiskModerate, "builtin", cronList(store))

	reg.RegisterWithGroup("cron_delete", "Delete a cron job",
		json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Cron job ID"}},"required":["id"]}`),
		GroupCron, RiskModerate, "builtin", cronDelete(store))
}

func cronCreate(store store.CronStore) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
			AgentID  string `json:"agent_id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if !isValidSchedule(params.Schedule) {
			return errorResult("invalid schedule. Supported: @every Nm/Nh, @hourly, @daily, */N * * * *"), nil
		}

		if params.AgentID == "" {
			params.AgentID = "default"
		}

		id, err := store.CreateCronJob(ctx, params.AgentID, params.Schedule, params.Prompt)
		if err != nil {
			return errorResult("failed to create cron job: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Cron job created: %s\nSchedule: %s\nPrompt: %s", id, params.Schedule, params.Prompt)}, nil
	}
}

func cronList(store store.CronStore) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		jobs, err := store.ListCronJobs(ctx)
		if err != nil {
			return errorResult("failed to list cron jobs: " + err.Error()), nil
		}

		if len(jobs) == 0 {
			return &canonical.ToolResult{Content: "No cron jobs configured."}, nil
		}

		var sb strings.Builder
		for _, job := range jobs {
			enabled := "enabled"
			if !job.Enabled {
				enabled = "disabled"
			}
			fmt.Fprintf(&sb, "- **%s** [%s] Schedule: `%s` — %s\n", job.ID[:8], enabled, job.Schedule, job.Prompt)
		}

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func cronDelete(store store.CronStore) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if err := store.DeleteCronJob(ctx, params.ID); err != nil {
			return errorResult("failed to delete cron job: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Cron job deleted: %s", params.ID)}, nil
	}
}

// isValidSchedule checks if a schedule expression is supported.
func isValidSchedule(s string) bool {
	s = strings.TrimSpace(s)
	if s == "@hourly" || s == "@daily" || s == "@weekly" || s == "@monthly" {
		return true
	}
	if strings.HasPrefix(s, "@every ") {
		return true
	}
	// Basic cron: */N * * * *
	if strings.HasPrefix(s, "*/") {
		return true
	}
	return false
}
