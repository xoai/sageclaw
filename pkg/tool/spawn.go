package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// SubagentSpawner abstracts the SubagentManager (breaks import cycle with agent package).
type SubagentSpawner interface {
	Spawn(ctx context.Context, parentAgentID, sessionID, task, label, mode string) (taskID, result string, err error)
	List(parentAgentID, sessionID string) []SubagentInfo
	Cancel(taskID string) error
	CancelAll(parentAgentID, sessionID string)
}

// SubagentInfo is the tool-facing view of a subagent task.
type SubagentInfo struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// RegisterSpawnTools registers the spawn and subagents tools.
// If spawner is nil, tools are registered as no-ops (for agents without subagent support).
func RegisterSpawnTools(reg *Registry, spawner SubagentSpawner) {
	spawnSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"task": {
				"type": "string",
				"description": "What the subagent should do"
			},
			"label": {
				"type": "string",
				"description": "Short label for tracking"
			},
			"mode": {
				"type": "string",
				"enum": ["sync", "async"],
				"default": "async",
				"description": "sync blocks until done, async returns immediately"
			},
			"agent_id": {
				"type": "string",
				"description": "DO NOT USE — spawn creates a clone of yourself. Use delegate for other agents."
			}
		},
		"required": ["task"]
	}`)

	reg.RegisterWithGroup("spawn", "Spawn a subagent to run a task in parallel",
		spawnSchema, GroupOrchestration, RiskSensitive, "builtin",
		spawnHandler(spawner))

	subagentsSchema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["list", "cancel", "cancel_all"],
				"description": "Action to perform on subagents"
			},
			"task_id": {
				"type": "string",
				"description": "Subagent task ID (for cancel)"
			}
		},
		"required": ["action"]
	}`)

	reg.RegisterWithGroup("subagents", "List, check, or cancel spawned subagents",
		subagentsSchema, GroupOrchestration, RiskSafe, "builtin",
		subagentsHandler(spawner))
}

func spawnHandler(spawner SubagentSpawner) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			Task    string `json:"task"`
			Label   string `json:"label"`
			Mode    string `json:"mode"`
			AgentID string `json:"agent_id"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Reject agent_id parameter.
		if p.AgentID != "" {
			return &canonical.ToolResult{
				Content: "spawn creates a clone of yourself. To delegate to a different agent, use the delegate tool instead.",
			}, nil
		}

		if p.Task == "" {
			return errorResult("task is required"), nil
		}

		if spawner == nil {
			return errorResult("subagent spawning is not available"), nil
		}

		agentID, _ := ctx.Value(agentIDKey{}).(string)
		sessionID := SessionIDFromCtx(ctx)

		taskID, result, err := spawner.Spawn(ctx, agentID, sessionID, p.Task, p.Label, p.Mode)
		if err != nil {
			return errorResult("spawn failed: " + err.Error()), nil
		}

		if p.Mode == "sync" {
			content := result
			if len(content) > 4000 {
				content = content[:4000] + "\n... (truncated)"
			}
			return &canonical.ToolResult{
				Content: fmt.Sprintf("Subagent %s (%s) completed:\n%s", taskID, p.Label, content),
			}, nil
		}

		return &canonical.ToolResult{
			Content: fmt.Sprintf("Subagent spawned: %s (label: %s, mode: async). Results will be injected when complete.", taskID, p.Label),
		}, nil
	}
}

func subagentsHandler(spawner SubagentSpawner) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			Action string `json:"action"`
			TaskID string `json:"task_id"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if spawner == nil {
			return errorResult("subagent management is not available"), nil
		}

		agentID, _ := ctx.Value(agentIDKey{}).(string)
		sessionID := SessionIDFromCtx(ctx)

		switch p.Action {
		case "list":
			tasks := spawner.List(agentID, sessionID)
			if len(tasks) == 0 {
				return &canonical.ToolResult{Content: "No active subagents."}, nil
			}
			var sb strings.Builder
			for _, t := range tasks {
				fmt.Fprintf(&sb, "- [%s] %s: %s", t.Status, t.ID, t.Label)
				if t.Error != "" {
					fmt.Fprintf(&sb, " (error: %s)", t.Error)
				}
				sb.WriteString("\n")
			}
			return &canonical.ToolResult{Content: sb.String()}, nil

		case "cancel":
			if p.TaskID == "" {
				return errorResult("task_id is required for cancel"), nil
			}
			if err := spawner.Cancel(p.TaskID); err != nil {
				return errorResult("cancel failed: " + err.Error()), nil
			}
			return &canonical.ToolResult{Content: fmt.Sprintf("Subagent %s cancelled.", p.TaskID)}, nil

		case "cancel_all":
			spawner.CancelAll(agentID, sessionID)
			return &canonical.ToolResult{Content: "All subagents cancelled."}, nil

		default:
			return errorResult(fmt.Sprintf("unknown action %q — use: list, cancel, cancel_all", p.Action)), nil
		}
	}
}
