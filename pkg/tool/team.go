package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// TeamOps abstracts team operations (breaks import cycle with orchestration).
type TeamOps interface {
	CreateTask(ctx context.Context, teamID, title, description, createdBy string) (string, error)
	ClaimTask(ctx context.Context, taskID, agentID string) error
	CompleteTask(ctx context.Context, taskID, result string) error
	UpdateTaskStatus(ctx context.Context, taskID, status string) error
	ListTasks(ctx context.Context, teamID, status string) ([]store.TeamTask, error)
	SendMessage(ctx context.Context, teamID, fromAgent, toAgent, content string) error
	GetMessages(ctx context.Context, agentID string, unreadOnly bool) ([]store.TeamMessage, error)
	MarkRead(ctx context.Context, messageID string) error
}

// RegisterTeamForRole registers team tools appropriate for the agent's role.
// Leads get task management tools but NOT mailbox tools.
// Members get all tools including mailbox.
func RegisterTeamForRole(reg *Registry, ops TeamOps, isLead bool) {
	// Task tools — available to both leads and members.
	reg.RegisterWithGroup("team_create_task", "Create a task on the team board",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"assignee":{"type":"string","description":"Agent ID to assign to"},"blocked_by":{"type":"string","description":"Comma-separated task IDs this task depends on"}},"required":["team_id","title"]}`),
		GroupTeam, RiskModerate, "builtin", teamCreateTask(ops))

	reg.RegisterWithGroup("team_assign_task", "Assign a task to a team member",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"agent_id":{"type":"string"}},"required":["task_id","agent_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamClaimTask(ops))

	reg.RegisterWithGroup("team_claim_task", "Claim an open task",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamClaimSelf(ops))

	reg.RegisterWithGroup("team_complete_task", "Mark a task as completed with result",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"result":{"type":"string"}},"required":["task_id","result"]}`),
		GroupTeam, RiskModerate, "builtin", teamCompleteTask(ops))

	reg.RegisterWithGroup("team_list_tasks", "List tasks on the team board",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"status":{"type":"string","description":"Filter: open, claimed, completed, blocked"}},"required":["team_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamListTasks(ops))

	// Mailbox tools — members only (leads coordinate via tasks, not messages).
	if !isLead {
		reg.RegisterWithGroup("team_send", "Send a message to a team member or broadcast",
			json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"to_agent":{"type":"string","description":"Target agent ID (empty=broadcast)"},"content":{"type":"string"}},"required":["team_id","content"]}`),
			GroupTeam, RiskModerate, "builtin", teamSend(ops))

		reg.RegisterWithGroup("team_inbox", "Check team inbox messages",
			json.RawMessage(`{"type":"object","properties":{"unread_only":{"type":"boolean","default":true}}}`),
			GroupTeam, RiskModerate, "builtin", teamInbox(ops))
	}
}

// RegisterTeam registers all team tools (backward compatible — all tools for all roles).
func RegisterTeam(reg *Registry, ops TeamOps) {
	reg.RegisterWithGroup("team_create_task", "Create a task on the team board",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"title":{"type":"string"},"description":{"type":"string"},"assignee":{"type":"string","description":"Agent ID to assign to"},"blocked_by":{"type":"string","description":"Comma-separated task IDs this task depends on"}},"required":["team_id","title"]}`),
		GroupTeam, RiskModerate, "builtin", teamCreateTask(ops))

	reg.RegisterWithGroup("team_assign_task", "Assign a task to a team member",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"agent_id":{"type":"string"}},"required":["task_id","agent_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamClaimTask(ops))

	reg.RegisterWithGroup("team_claim_task", "Claim an open task",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"}},"required":["task_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamClaimSelf(ops))

	reg.RegisterWithGroup("team_complete_task", "Mark a task as completed with result",
		json.RawMessage(`{"type":"object","properties":{"task_id":{"type":"string"},"result":{"type":"string"}},"required":["task_id","result"]}`),
		GroupTeam, RiskModerate, "builtin", teamCompleteTask(ops))

	reg.RegisterWithGroup("team_list_tasks", "List tasks on the team board",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"status":{"type":"string","description":"Filter: open, claimed, completed, blocked"}},"required":["team_id"]}`),
		GroupTeam, RiskModerate, "builtin", teamListTasks(ops))

	reg.RegisterWithGroup("team_send", "Send a message to a team member or broadcast",
		json.RawMessage(`{"type":"object","properties":{"team_id":{"type":"string"},"to_agent":{"type":"string","description":"Target agent ID (empty=broadcast)"},"content":{"type":"string"}},"required":["team_id","content"]}`),
		GroupTeam, RiskModerate, "builtin", teamSend(ops))

	reg.RegisterWithGroup("team_inbox", "Check team inbox messages",
		json.RawMessage(`{"type":"object","properties":{"unread_only":{"type":"boolean","default":true}}}`),
		GroupTeam, RiskModerate, "builtin", teamInbox(ops))
}

func teamCreateTask(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TeamID      string `json:"team_id"`
			Title       string `json:"title"`
			Description string `json:"description"`
			Assignee    string `json:"assignee"`
			BlockedBy   string `json:"blocked_by"`
		}
		json.Unmarshal(input, &p)
		agentID, _ := ctx.Value(agentIDKey{}).(string)
		id, err := ops.CreateTask(ctx, p.TeamID, p.Title, p.Description, agentID)
		if err != nil {
			return errorResult(err.Error()), nil
		}

		// Handle assignment.
		if p.Assignee != "" {
			ops.ClaimTask(ctx, id, p.Assignee)
		}

		// Handle blocked_by — mark as blocked if dependencies specified.
		if p.BlockedBy != "" {
			ops.UpdateTaskStatus(ctx, id, "blocked")
		}

		status := "open"
		if p.BlockedBy != "" {
			status = "blocked"
		} else if p.Assignee != "" {
			status = "assigned to " + p.Assignee
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Task created: %s — %s (%s)", id[:8], p.Title, status)}, nil
	}
}

func teamClaimTask(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TaskID  string `json:"task_id"`
			AgentID string `json:"agent_id"`
		}
		json.Unmarshal(input, &p)
		if err := ops.ClaimTask(ctx, p.TaskID, p.AgentID); err != nil {
			return errorResult(err.Error()), nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Task %s assigned to %s", p.TaskID[:8], p.AgentID)}, nil
	}
}

func teamClaimSelf(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TaskID string `json:"task_id"`
		}
		json.Unmarshal(input, &p)
		agentID, _ := ctx.Value(agentIDKey{}).(string)
		if err := ops.ClaimTask(ctx, p.TaskID, agentID); err != nil {
			return errorResult(err.Error()), nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Task %s claimed", p.TaskID[:8])}, nil
	}
}

func teamCompleteTask(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TaskID string `json:"task_id"`
			Result string `json:"result"`
		}
		json.Unmarshal(input, &p)
		if err := ops.CompleteTask(ctx, p.TaskID, p.Result); err != nil {
			return errorResult(err.Error()), nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Task %s completed", p.TaskID[:8])}, nil
	}
}

func teamListTasks(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TeamID string `json:"team_id"`
			Status string `json:"status"`
		}
		json.Unmarshal(input, &p)
		tasks, err := ops.ListTasks(ctx, p.TeamID, p.Status)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		if len(tasks) == 0 {
			return &canonical.ToolResult{Content: "No tasks found."}, nil
		}
		var sb strings.Builder
		for _, t := range tasks {
			assigned := ""
			if t.AssignedTo != "" {
				assigned = " → " + t.AssignedTo
			}
			fmt.Fprintf(&sb, "- [%s] %s: %s%s\n", t.Status, t.ID[:8], t.Title, assigned)
		}
		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

func teamSend(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			TeamID  string `json:"team_id"`
			ToAgent string `json:"to_agent"`
			Content string `json:"content"`
		}
		json.Unmarshal(input, &p)
		agentID, _ := ctx.Value(agentIDKey{}).(string)
		if err := ops.SendMessage(ctx, p.TeamID, agentID, p.ToAgent, p.Content); err != nil {
			return errorResult(err.Error()), nil
		}
		target := "team"
		if p.ToAgent != "" {
			target = p.ToAgent
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Message sent to %s", target)}, nil
	}
}

func teamInbox(ops TeamOps) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			UnreadOnly bool `json:"unread_only"`
		}
		p.UnreadOnly = true
		json.Unmarshal(input, &p)
		agentID, _ := ctx.Value(agentIDKey{}).(string)
		msgs, err := ops.GetMessages(ctx, agentID, p.UnreadOnly)
		if err != nil {
			return errorResult(err.Error()), nil
		}
		if len(msgs) == 0 {
			return &canonical.ToolResult{Content: "No messages."}, nil
		}
		var sb strings.Builder
		for _, m := range msgs {
			read := " "
			if !m.Read {
				read = "●"
			}
			fmt.Fprintf(&sb, "%s [%s] from %s: %s\n", read, m.ID[:8], m.FromAgent, m.Content)
		}
		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}
