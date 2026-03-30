package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// planStore holds per-session task plans.
var planStore = struct {
	sync.RWMutex
	plans map[string]*taskPlan // sessionID → plan
}{plans: make(map[string]*taskPlan)}

type taskPlan struct {
	Tasks []planTask `json:"tasks"`
}

type planTask struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"` // "pending", "in_progress", "done", "skipped"
}

// RegisterPlan registers the within-session planning tool.
// Allows the agent to create structured task lists, track progress,
// and check off completed items during multi-step operations.
func RegisterPlan(reg *Registry) {
	reg.RegisterWithGroup("plan", "Create and manage a task plan for multi-step operations. "+
		"Use for tasks requiring 3+ steps to stay organized.",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"action": {
					"type": "string",
					"enum": ["create", "update", "complete", "skip", "show"],
					"description": "create: make a new plan. update: change a task title. complete/skip: mark task done/skipped. show: display current plan."
				},
				"tasks": {
					"type": "array",
					"items": {"type": "string"},
					"description": "Task titles (for 'create' action only)."
				},
				"task_id": {
					"type": "integer",
					"description": "Task ID to update/complete/skip."
				},
				"title": {
					"type": "string",
					"description": "New title (for 'update' action)."
				},
				"session_id": {
					"type": "string",
					"description": "Session ID (auto-injected by the agent loop)."
				}
			},
			"required": ["action"]
		}`),
		GroupCore, RiskSafe, "",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			var params struct {
				Action    string   `json:"action"`
				Tasks     []string `json:"tasks"`
				TaskID    int      `json:"task_id"`
				Title     string   `json:"title"`
				SessionID string   `json:"session_id"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return &canonical.ToolResult{Content: "Invalid input: " + err.Error(), IsError: true}, nil
			}

			sid := params.SessionID
			if sid == "" {
				sid = "default"
			}

			switch params.Action {
			case "create":
				return planCreate(sid, params.Tasks), nil
			case "update":
				return planUpdate(sid, params.TaskID, params.Title), nil
			case "complete":
				return planSetStatus(sid, params.TaskID, "done"), nil
			case "skip":
				return planSetStatus(sid, params.TaskID, "skipped"), nil
			case "show":
				return planShow(sid), nil
			default:
				return &canonical.ToolResult{Content: "Unknown action: " + params.Action, IsError: true}, nil
			}
		},
	)
}

func planCreate(sessionID string, tasks []string) *canonical.ToolResult {
	if len(tasks) == 0 {
		return &canonical.ToolResult{Content: "No tasks provided. Include a 'tasks' array.", IsError: true}
	}

	plan := &taskPlan{}
	for i, title := range tasks {
		plan.Tasks = append(plan.Tasks, planTask{
			ID:     i + 1,
			Title:  title,
			Status: "pending",
		})
	}

	planStore.Lock()
	planStore.plans[sessionID] = plan
	planStore.Unlock()

	return &canonical.ToolResult{Content: formatPlan(plan)}
}

func planUpdate(sessionID string, taskID int, title string) *canonical.ToolResult {
	planStore.Lock()
	defer planStore.Unlock()

	plan, ok := planStore.plans[sessionID]
	if !ok {
		return &canonical.ToolResult{Content: "No plan exists for this session. Use action 'create' first.", IsError: true}
	}

	for i := range plan.Tasks {
		if plan.Tasks[i].ID == taskID {
			plan.Tasks[i].Title = title
			return &canonical.ToolResult{Content: formatPlan(plan)}
		}
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Task %d not found.", taskID), IsError: true}
}

func planSetStatus(sessionID string, taskID int, status string) *canonical.ToolResult {
	planStore.Lock()
	defer planStore.Unlock()

	plan, ok := planStore.plans[sessionID]
	if !ok {
		return &canonical.ToolResult{Content: "No plan exists for this session.", IsError: true}
	}

	for i := range plan.Tasks {
		if plan.Tasks[i].ID == taskID {
			plan.Tasks[i].Status = status
			return &canonical.ToolResult{Content: formatPlan(plan)}
		}
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Task %d not found.", taskID), IsError: true}
}

func planShow(sessionID string) *canonical.ToolResult {
	planStore.RLock()
	defer planStore.RUnlock()

	plan, ok := planStore.plans[sessionID]
	if !ok {
		return &canonical.ToolResult{Content: "No plan exists for this session."}
	}

	return &canonical.ToolResult{Content: formatPlan(plan)}
}

func formatPlan(plan *taskPlan) string {
	var b strings.Builder
	done := 0
	total := len(plan.Tasks)

	for _, t := range plan.Tasks {
		icon := "[ ]"
		switch t.Status {
		case "done":
			icon = "[x]"
			done++
		case "in_progress":
			icon = "[>]"
		case "skipped":
			icon = "[-]"
			done++ // Count skipped as completed for progress.
		}
		b.WriteString(fmt.Sprintf("%s %d. %s\n", icon, t.ID, t.Title))
	}

	b.WriteString(fmt.Sprintf("\nProgress: %d/%d complete", done, total))
	return b.String()
}
