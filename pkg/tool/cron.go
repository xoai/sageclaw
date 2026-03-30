package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// RegisterCron registers cron management tools.
func RegisterCron(reg *Registry, store store.CronStore) {
	reg.RegisterWithGroup("cron_create", "Create a scheduled cron job",
		json.RawMessage(`{"type":"object","properties":{"schedule":{"type":"string","description":"Schedule: @every 5m, @hourly, @daily, */N * * * *, or @at 2026-03-30T15:00:00Z (one-time)"},"prompt":{"type":"string","description":"Prompt to send to the agent on schedule"},"agent_id":{"type":"string","description":"Agent ID (default: current agent)"}},"required":["schedule","prompt"]}`),
		GroupCron, RiskModerate, "builtin", cronCreate(store))

	reg.RegisterWithGroup("cron_list", "List active cron jobs",
		json.RawMessage(`{"type":"object","properties":{}}`),
		GroupCron, RiskModerate, "builtin", cronList(store))

	reg.RegisterWithGroup("cron_update", "Update a cron job's schedule or prompt",
		json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Cron job ID"},"schedule":{"type":"string","description":"New schedule (optional)"},"prompt":{"type":"string","description":"New prompt (optional)"}},"required":["id"]}`),
		GroupCron, RiskModerate, "builtin", cronUpdate(store))

	reg.RegisterWithGroup("cron_delete", "Delete a cron job",
		json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Cron job ID"}},"required":["id"]}`),
		GroupCron, RiskModerate, "builtin", cronDelete(store))

	reg.RegisterWithGroup("cron_run", "Trigger a cron job immediately",
		json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","description":"Cron job ID to run now"}},"required":["id"]}`),
		GroupCron, RiskModerate, "builtin", cronRun(store))
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
			return errorResult("invalid schedule. Supported: @every Nm/Nh, @hourly, @daily, */N * * * *, @at ISO8601"), nil
		}

		if params.AgentID == "" {
			params.AgentID = "default"
		}

		// Validate one-time schedule is in the future.
		if strings.HasPrefix(params.Schedule, "@at ") {
			ts := strings.TrimPrefix(params.Schedule, "@at ")
			t, err := time.Parse(time.RFC3339, ts)
			if err != nil {
				return errorResult("invalid @at timestamp — use ISO 8601: @at 2026-03-30T15:00:00Z"), nil
			}
			if t.Before(time.Now()) {
				return errorResult("@at timestamp must be in the future"), nil
			}
		}

		id, err := store.CreateCronJob(ctx, params.AgentID, params.Schedule, params.Prompt)
		if err != nil {
			return errorResult("failed to create cron job: " + err.Error()), nil
		}

		kind := "recurring"
		if strings.HasPrefix(params.Schedule, "@at ") {
			kind = "one-time"
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Cron job created (%s): %s\nSchedule: %s\nPrompt: %s", kind, id, params.Schedule, params.Prompt)}, nil
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
			kind := ""
			if strings.HasPrefix(job.Schedule, "@at ") {
				kind = " [one-time]"
			}
			fmt.Fprintf(&sb, "- **%s** [%s%s] Schedule: `%s` — %s\n", shortID(job.ID), enabled, kind, job.Schedule, job.Prompt)
		}

		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func cronUpdate(store store.CronStore) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			ID       string `json:"id"`
			Schedule string `json:"schedule"`
			Prompt   string `json:"prompt"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.Schedule == "" && params.Prompt == "" {
			return errorResult("provide at least one of: schedule, prompt"), nil
		}

		var schedule, prompt *string
		if params.Schedule != "" {
			if !isValidSchedule(params.Schedule) {
				return errorResult("invalid schedule. Supported: @every Nm/Nh, @hourly, @daily, */N * * * *, @at ISO8601"), nil
			}
			if strings.HasPrefix(params.Schedule, "@at ") {
				ts := strings.TrimPrefix(params.Schedule, "@at ")
				t, _ := time.Parse(time.RFC3339, ts) // already validated by isValidSchedule
				if t.Before(time.Now()) {
					return errorResult("@at timestamp must be in the future"), nil
				}
			}
			schedule = &params.Schedule
		}
		if params.Prompt != "" {
			prompt = &params.Prompt
		}

		if err := store.UpdateCronJob(ctx, params.ID, schedule, prompt); err != nil {
			return errorResult("failed to update cron job: " + err.Error()), nil
		}

		var changes []string
		if schedule != nil {
			changes = append(changes, "schedule → "+*schedule)
		}
		if prompt != nil {
			changes = append(changes, "prompt updated")
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Cron job %s updated: %s", shortID(params.ID), strings.Join(changes, ", "))}, nil
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

func cronRun(store store.CronStore) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Verify the job exists.
		job, err := store.GetCronJob(ctx, params.ID)
		if err != nil {
			return errorResult("cron job not found: " + err.Error()), nil
		}

		// Mark as last run now (the scheduler will pick up the trigger).
		if err := store.UpdateCronLastRun(ctx, params.ID, time.Now()); err != nil {
			return errorResult("failed to trigger run: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Triggered cron job %s (agent: %s)\nPrompt: %s", shortID(job.ID), job.AgentID, job.Prompt)}, nil
	}
}

// shortID truncates an ID for display, safe for any length.
func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

// isValidSchedule checks if a schedule expression is supported.
func isValidSchedule(s string) bool {
	s = strings.TrimSpace(s)
	if s == "@hourly" || s == "@daily" || s == "@weekly" || s == "@monthly" {
		return true
	}
	if strings.HasPrefix(s, "@every ") {
		_, err := time.ParseDuration(strings.TrimPrefix(s, "@every "))
		return err == nil
	}
	// One-time: @at ISO8601
	if strings.HasPrefix(s, "@at ") {
		ts := strings.TrimPrefix(s, "@at ")
		_, err := time.Parse(time.RFC3339, ts)
		return err == nil
	}
	// Basic cron: */N * * * * — validate N is a positive integer.
	if strings.HasPrefix(s, "*/") {
		rest := strings.TrimPrefix(s, "*/")
		parts := strings.Fields(rest)
		if len(parts) == 0 {
			return false
		}
		for _, c := range parts[0] {
			if c < '0' || c > '9' {
				return false
			}
		}
		return len(parts[0]) > 0
	}
	return false
}
