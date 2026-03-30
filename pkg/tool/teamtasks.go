package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// TeamTaskExecutor abstracts the TeamExecutor (breaks import cycle with team package).
type TeamTaskExecutor interface {
	Dispatch(ctx context.Context, task store.TeamTask) (string, error)
}

// RegisterTeamTasks registers the unified team_tasks tool.
// This tool provides action-based access to the team task board.
// Role enforcement happens at execution time based on the caller's agent ID.
func RegisterTeamTasks(reg *Registry, s store.Store, executor TeamTaskExecutor) {
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {
				"type": "string",
				"enum": ["create", "list", "get", "cancel", "assign", "complete", "progress", "comment", "search", "approve", "reject"],
				"description": "Action to perform on the team task board"
			},
			"task_id": {
				"type": "string",
				"description": "Task ID (required for get, cancel, assign, complete, progress, comment, approve, reject)"
			},
			"team_id": {
				"type": "string",
				"description": "Team ID (required for create, list, search)"
			},
			"subject": {
				"type": "string",
				"description": "Task title (required for create)"
			},
			"description": {
				"type": "string",
				"description": "Task description (for create)"
			},
			"assignee": {
				"type": "string",
				"description": "Agent ID to assign to (for create, assign)"
			},
			"priority": {
				"type": "integer",
				"description": "Priority (higher = more urgent, default 0)"
			},
			"blocked_by": {
				"type": "array",
				"items": {"type": "string"},
				"description": "Task IDs this task depends on (for create)"
			},
			"require_approval": {
				"type": "boolean",
				"description": "Whether task needs lead approval before completing (for create)"
			},
			"result": {
				"type": "string",
				"description": "Result text (for complete)"
			},
			"percent": {
				"type": "integer",
				"description": "Progress percentage 0-100 (for progress)"
			},
			"text": {
				"type": "string",
				"description": "Progress text or comment content (for progress, comment)"
			},
			"status": {
				"type": "string",
				"description": "Status filter (for list)"
			},
			"query": {
				"type": "string",
				"description": "Search query (for search)"
			},
			"page": {
				"type": "integer",
				"description": "Page number for list/search (default 1)"
			},
			"limit": {
				"type": "integer",
				"description": "Results per page for list/search (default 30, max 100)"
			}
		},
		"required": ["action"]
	}`)

	reg.RegisterWithGroup("team_tasks", "Manage team task board — create, assign, complete, track tasks",
		schema, GroupTeam, RiskModerate, "builtin",
		teamTasksHandler(s, executor))
}

type teamTasksParams struct {
	Action          string   `json:"action"`
	TaskID          string   `json:"task_id"`
	TeamID          string   `json:"team_id"`
	Subject         string   `json:"subject"`
	Description     string   `json:"description"`
	Assignee        string   `json:"assignee"`
	Priority        int      `json:"priority"`
	BlockedBy       []string `json:"blocked_by"`
	RequireApproval bool     `json:"require_approval"`
	Result          string   `json:"result"`
	Percent         int      `json:"percent"`
	Text            string   `json:"text"`
	Status          string   `json:"status"`
	Query           string   `json:"query"`
	Page            int      `json:"page"`
	Limit           int      `json:"limit"`
}

func teamTasksHandler(s store.Store, executor TeamTaskExecutor) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p teamTasksParams
		if err := json.Unmarshal(input, &p); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		agentID, _ := ctx.Value(agentIDKey{}).(string)

		switch p.Action {
		case "create":
			return ttCreate(ctx, s, executor, agentID, &p)
		case "list":
			return ttList(ctx, s, agentID, &p)
		case "get":
			return ttGet(ctx, s, &p)
		case "cancel":
			return ttCancel(ctx, s, agentID, &p)
		case "assign":
			return ttAssign(ctx, s, agentID, &p)
		case "complete":
			return ttComplete(ctx, s, agentID, &p)
		case "progress":
			return ttProgress(ctx, s, agentID, &p)
		case "comment":
			return ttComment(ctx, s, agentID, &p)
		case "search":
			return ttSearch(ctx, s, &p)
		case "approve":
			return ttApprove(ctx, s, agentID, &p)
		case "reject":
			return ttReject(ctx, s, agentID, &p)
		default:
			return errorResult(fmt.Sprintf("unknown action %q — use: create, list, get, cancel, assign, complete, progress, comment, search, approve, reject", p.Action)), nil
		}
	}
}

// requireRole checks the caller's role in the team. Returns (team, role, error result).
func requireRole(ctx context.Context, s store.Store, agentID, requiredRole string) (*store.Team, string, *canonical.ToolResult) {
	team, role, err := s.GetTeamByAgent(ctx, agentID)
	if err != nil {
		return nil, "", errorResult("failed to look up team membership: " + err.Error())
	}
	if team == nil {
		return nil, "", errorResult("you are not a member of any team")
	}
	if requiredRole != "" && role != requiredRole {
		return nil, "", errorResult(fmt.Sprintf("action requires %s role, but you are a %s", requiredRole, role))
	}
	return team, role, nil
}

func ttCreate(ctx context.Context, s store.Store, executor TeamTaskExecutor, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	// Role check: lead only.
	team, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	teamID := p.TeamID
	if teamID == "" {
		teamID = team.ID
	}

	if p.Subject == "" {
		return errorResult("subject is required for create"), nil
	}

	// Validate assignee is a team member if specified.
	if p.Assignee != "" {
		aTeam, _, err := s.GetTeamByAgent(ctx, p.Assignee)
		if err != nil || aTeam == nil || aTeam.ID != teamID {
			return errorResult(fmt.Sprintf("assignee %q is not a member of team %s", p.Assignee, teamID)), nil
		}
	}

	blockedBy := strings.Join(p.BlockedBy, ",")
	status := "pending"
	if blockedBy != "" {
		status = "blocked"
	}

	// Note: TaskNumber and Identifier are left at zero/empty.
	// CreateTask auto-assigns them via NextTaskNumber to avoid double-increment.
	task := store.TeamTask{
		TeamID:          teamID,
		Title:           p.Subject,
		Description:     p.Description,
		Status:          status,
		AssignedTo:      p.Assignee,
		CreatedBy:       agentID,
		BlockedBy:       blockedBy,
		Priority:        p.Priority,
		OwnerAgentID:    agentID,
		RequireApproval: p.RequireApproval,
		MaxRetries:      1,
	}

	// Use executor.Dispatch which handles DB insert + async execution.
	taskID, err := executor.Dispatch(ctx, task)
	if err != nil {
		return errorResult("failed to create task: " + err.Error()), nil
	}

	// Fetch the created task to get the auto-assigned identifier.
	created, _ := s.GetTask(ctx, taskID)
	identifier := taskID[:8]
	if created != nil && created.Identifier != "" {
		identifier = created.Identifier
	}

	statusMsg := status
	if p.Assignee != "" && status == "pending" {
		statusMsg = "dispatched to " + p.Assignee
	}
	return &canonical.ToolResult{
		Content: fmt.Sprintf("Task created: %s (%s) — %s [%s]", taskID[:8], identifier, p.Subject, statusMsg),
	}, nil
}

func ttList(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	// Determine team ID — either from param or from agent's team.
	teamID := p.TeamID
	if teamID == "" {
		team, _, err := s.GetTeamByAgent(ctx, agentID)
		if err != nil || team == nil {
			return errorResult("team_id is required (you are not in a team)"), nil
		}
		teamID = team.ID
	}

	tasks, err := s.ListTasks(ctx, teamID, p.Status)
	if err != nil {
		return errorResult("failed to list tasks: " + err.Error()), nil
	}
	if len(tasks) == 0 {
		if p.Status != "" {
			return &canonical.ToolResult{Content: fmt.Sprintf("No %s tasks.", p.Status)}, nil
		}
		return &canonical.ToolResult{Content: "No tasks on the board."}, nil
	}

	// Pagination.
	limit := p.Limit
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	page := p.Page
	if page <= 0 {
		page = 1
	}
	start := (page - 1) * limit
	if start >= len(tasks) {
		return &canonical.ToolResult{Content: fmt.Sprintf("Page %d is empty (total: %d tasks).", page, len(tasks))}, nil
	}
	end := start + limit
	if end > len(tasks) {
		end = len(tasks)
	}
	pageTasks := tasks[start:end]

	var sb strings.Builder
	if len(tasks) > limit {
		fmt.Fprintf(&sb, "Page %d/%d (%d total)\n\n", page, (len(tasks)+limit-1)/limit, len(tasks))
	}
	for _, t := range pageTasks {
		assignee := ""
		if t.AssignedTo != "" {
			assignee = " → " + t.AssignedTo
		}
		progress := ""
		if t.ProgressPercent > 0 {
			progress = fmt.Sprintf(" (%d%%)", t.ProgressPercent)
		}
		id := t.Identifier
		if id == "" {
			id = t.ID[:8]
		}
		fmt.Fprintf(&sb, "- [%s] %s: %s%s%s\n", t.Status, id, t.Title, assignee, progress)
	}
	return &canonical.ToolResult{Content: sb.String()}, nil
}

func ttGet(ctx context.Context, s store.Store, p *teamTasksParams) (*canonical.ToolResult, error) {
	if p.TaskID == "" {
		return errorResult("task_id is required for get"), nil
	}

	task, err := s.GetTask(ctx, p.TaskID)
	if err != nil {
		return errorResult("failed to get task: " + err.Error()), nil
	}
	if task == nil {
		return errorResult("task not found"), nil
	}

	var sb strings.Builder
	id := task.Identifier
	if id == "" {
		id = task.ID[:8]
	}
	fmt.Fprintf(&sb, "**%s** — %s\n", id, task.Title)
	fmt.Fprintf(&sb, "Status: %s\n", task.Status)
	if task.AssignedTo != "" {
		fmt.Fprintf(&sb, "Assigned to: %s\n", task.AssignedTo)
	}
	if task.Priority > 0 {
		fmt.Fprintf(&sb, "Priority: %d\n", task.Priority)
	}
	if task.ProgressPercent > 0 {
		fmt.Fprintf(&sb, "Progress: %d%%\n", task.ProgressPercent)
	}
	if task.Description != "" {
		fmt.Fprintf(&sb, "Description: %s\n", task.Description)
	}
	if task.BlockedBy != "" {
		fmt.Fprintf(&sb, "Blocked by: %s\n", task.BlockedBy)
	}
	if task.Result != "" {
		result := task.Result
		if len(result) > 500 {
			result = result[:500] + "..."
		}
		fmt.Fprintf(&sb, "Result: %s\n", result)
	}
	if task.ErrorMessage != "" {
		fmt.Fprintf(&sb, "Error: %s\n", task.ErrorMessage)
	}

	// Include comments.
	comments, err := s.ListComments(ctx, task.ID)
	if err == nil && len(comments) > 0 {
		sb.WriteString("\nComments:\n")
		for _, c := range comments {
			author := c.AgentID
			if author == "" {
				author = "user"
			}
			fmt.Fprintf(&sb, "  - [%s] %s: %s\n", c.CreatedAt.Format("15:04"), author, c.Content)
		}
	}

	return &canonical.ToolResult{Content: sb.String()}, nil
}

func ttCancel(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	_, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	if p.TaskID == "" {
		return errorResult("task_id is required for cancel"), nil
	}

	if err := s.CancelTask(ctx, p.TaskID); err != nil {
		return errorResult("failed to cancel: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s cancelled.", p.TaskID[:8])}, nil
}

func ttAssign(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	_, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	if p.TaskID == "" || p.Assignee == "" {
		return errorResult("task_id and assignee are required for assign"), nil
	}

	if err := s.ClaimTask(ctx, p.TaskID, p.Assignee); err != nil {
		return errorResult("failed to assign: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s assigned to %s.", p.TaskID[:8], p.Assignee)}, nil
}

func ttComplete(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	// Members can complete tasks assigned to them.
	team, _, errResult := requireRole(ctx, s, agentID, "")
	if errResult != nil {
		return errResult, nil
	}
	_ = team

	if p.TaskID == "" {
		return errorResult("task_id is required for complete"), nil
	}

	// Verify the task is assigned to this agent.
	task, err := s.GetTask(ctx, p.TaskID)
	if err != nil || task == nil {
		return errorResult("task not found"), nil
	}
	if task.AssignedTo != agentID {
		return errorResult(fmt.Sprintf("task is assigned to %s, not you (%s)", task.AssignedTo, agentID)), nil
	}

	if err := s.CompleteTask(ctx, p.TaskID, p.Result); err != nil {
		return errorResult("failed to complete: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s completed.", p.TaskID[:8])}, nil
}

func ttProgress(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	team, _, errResult := requireRole(ctx, s, agentID, "")
	if errResult != nil {
		return errResult, nil
	}
	_ = team

	if p.TaskID == "" {
		return errorResult("task_id is required for progress"), nil
	}

	if err := s.UpdateTaskProgress(ctx, p.TaskID, p.Percent, p.Text); err != nil {
		return errorResult("failed to update progress: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s progress: %d%%", p.TaskID[:8], p.Percent)}, nil
}

func ttComment(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	team, _, errResult := requireRole(ctx, s, agentID, "")
	if errResult != nil {
		return errResult, nil
	}
	_ = team

	if p.TaskID == "" || p.Text == "" {
		return errorResult("task_id and text are required for comment"), nil
	}

	comment := store.TeamTaskComment{
		ID:          uuid.NewString(),
		TaskID:      p.TaskID,
		AgentID:     agentID,
		Content:     p.Text,
		CommentType: "note",
		CreatedAt:   time.Now(),
	}

	if _, err := s.CreateComment(ctx, comment); err != nil {
		return errorResult("failed to add comment: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: "Comment added."}, nil
}

func ttSearch(ctx context.Context, s store.Store, p *teamTasksParams) (*canonical.ToolResult, error) {
	if p.TeamID == "" {
		return errorResult("team_id is required for search"), nil
	}
	if p.Query == "" {
		return errorResult("query is required for search"), nil
	}

	tasks, err := s.SearchTasks(ctx, p.TeamID, p.Query)
	if err != nil {
		return errorResult("search failed: " + err.Error()), nil
	}
	if len(tasks) == 0 {
		return &canonical.ToolResult{Content: "No matching tasks."}, nil
	}

	var sb strings.Builder
	for _, t := range tasks {
		id := t.Identifier
		if id == "" {
			id = t.ID[:8]
		}
		fmt.Fprintf(&sb, "- [%s] %s: %s\n", t.Status, id, t.Title)
	}
	return &canonical.ToolResult{Content: sb.String()}, nil
}

func ttApprove(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	_, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	if p.TaskID == "" {
		return errorResult("task_id is required for approve"), nil
	}

	task, err := s.GetTask(ctx, p.TaskID)
	if err != nil || task == nil {
		return errorResult("task not found"), nil
	}
	if task.Status != "in_review" {
		return errorResult(fmt.Sprintf("task is %s, not in_review", task.Status)), nil
	}

	// Use UpdateTask to transition in_review → completed (bypasses in_progress check in CompleteTask).
	if err := s.UpdateTask(ctx, p.TaskID, map[string]any{
		"status":       "completed",
		"completed_at": time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return errorResult("failed to approve: " + err.Error()), nil
	}
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s approved and completed.", p.TaskID[:8])}, nil
}

func ttReject(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	_, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	if p.TaskID == "" {
		return errorResult("task_id is required for reject"), nil
	}

	task, err := s.GetTask(ctx, p.TaskID)
	if err != nil || task == nil {
		return errorResult("task not found"), nil
	}
	if task.Status != "in_review" {
		return errorResult(fmt.Sprintf("task is %s, not in_review", task.Status)), nil
	}

	// Reject re-queues the task as pending (not cancel) so it can be reworked.
	if err := s.UpdateTask(ctx, p.TaskID, map[string]any{
		"status":        "pending",
		"error_message": p.Text,
	}); err != nil {
		return errorResult("failed to reject: " + err.Error()), nil
	}

	// Add rejection comment with feedback if provided.
	if p.Text != "" {
		s.CreateComment(ctx, store.TeamTaskComment{
			ID:          uuid.NewString(),
			TaskID:      p.TaskID,
			AgentID:     agentID,
			Content:     "Rejected: " + p.Text,
			CommentType: "system",
			CreatedAt:   time.Now(),
		})
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s rejected.", p.TaskID[:8])}, nil
}
