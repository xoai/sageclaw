package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// TeamTaskExecutor abstracts the TeamExecutor (breaks import cycle with team package).
type TeamTaskExecutor interface {
	Dispatch(ctx context.Context, task store.TeamTask) (string, error)
	LaunchIfReady(ctx context.Context, task store.TeamTask)
	EmitTaskFailed(ctx context.Context, teamID, taskID string) // For blocker escalation.
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
				"enum": ["create", "list", "get", "cancel", "assign", "complete", "progress", "comment", "search", "approve", "reject", "send", "inbox"],
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
			"parent_id": {
				"type": "string",
				"description": "Parent task ID for creating subtasks (for create)"
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
			"to_agent": {
				"type": "string",
				"description": "Recipient agent ID (for send; empty = broadcast to team)"
			},
			"unread_only": {
				"type": "boolean",
				"description": "Only show unread messages (for inbox, default true)"
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
	ParentID        string   `json:"parent_id"`
	Priority        int      `json:"priority"`
	BlockedBy       []string `json:"blocked_by"`
	RequireApproval bool     `json:"require_approval"`
	Result          string   `json:"result"`
	Percent         int      `json:"percent"`
	Text            string   `json:"text"`
	Status          string   `json:"status"`
	Query           string   `json:"query"`
	ToAgent         string   `json:"to_agent"`
	UnreadOnly      *bool    `json:"unread_only"`
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
			return ttComment(ctx, s, executor, agentID, &p)
		case "search":
			return ttSearch(ctx, s, &p)
		case "approve":
			return ttApprove(ctx, s, agentID, &p)
		case "reject":
			return ttReject(ctx, s, executor, agentID, &p)
		case "send":
			return ttSend(ctx, s, agentID, &p)
		case "inbox":
			return ttInbox(ctx, s, agentID, &p)
		default:
			return errorResult(fmt.Sprintf("unknown action %q — use: create, list, get, cancel, assign, complete, progress, comment, search, approve, reject, send, inbox", p.Action)), nil
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
	t, _, errResult := requireRole(ctx, s, agentID, "lead")
	if errResult != nil {
		return errResult, nil
	}

	teamID := p.TeamID
	if teamID == "" {
		teamID = t.ID
	}

	if p.Subject == "" {
		return errorResult("subject is required for create"), nil
	}

	// Guard: lead must check the board before creating tasks (prevents duplicates).
	// Only enforced when the TeamTasksGuard is in context (lead agents only).
	if hasGuard(ctx) && !hasListedTasks(ctx) {
		return errorResult("Check existing tasks first: call team_tasks(action: \"list\") or " +
			"team_tasks(action: \"search\", query: \"...\") before creating new tasks. " +
			"This prevents duplicates."), nil
	}

	// Guard: lead cannot assign tasks to itself.
	if p.Assignee == agentID {
		members := listMemberNames(ctx, s, teamID)
		return errorResult("Team lead cannot self-assign tasks. Either handle this work directly " +
			"(respond to the user yourself) or assign to a team member. " +
			"Available members: " + members), nil
	}

	// Validate assignee is a team member if specified.
	if p.Assignee != "" {
		aTeam, aRole, err := s.GetTeamByAgent(ctx, p.Assignee)
		if err != nil || aTeam == nil || aTeam.ID != teamID {
			return errorResult(fmt.Sprintf("assignee %q is not a member of team %s", p.Assignee, teamID)), nil
		}
		// Also block assigning to the lead (even if a different lead agent ID).
		if aRole == "lead" {
			members := listMemberNames(ctx, s, teamID)
			return errorResult("Cannot assign tasks to the team lead. Assign to a member: " + members), nil
		}
	}

	// Duplicate task prevention: same title + assignee within 5 minutes.
	if dup := checkDuplicate(ctx, s, teamID, p.Subject, p.Assignee); dup != nil {
		return &canonical.ToolResult{
			Content: fmt.Sprintf("Task already exists: %s (%s). Use team_tasks(action='get', task_id='%s') to check it.",
				dup.Identifier, dup.Status, dup.ID),
		}, nil
	}

	// Validate parent_id if provided.
	if p.ParentID != "" {
		parent, err := s.GetTask(ctx, p.ParentID)
		if err != nil || parent == nil {
			return errorResult("parent task not found"), nil
		}
		if parent.TeamID != teamID {
			return errorResult("parent task belongs to different team"), nil
		}
		if isTerminal(parent.Status) {
			return errorResult("parent task is in terminal state"), nil
		}
	}

	blockedBy := strings.Join(p.BlockedBy, ",")
	status := "pending"
	if blockedBy != "" {
		status = "blocked"
	}

	// Check for pending dispatch queue (post-turn dispatch pattern).
	// If a queue exists, tasks are created in DB but not dispatched yet.
	queue := PendingDispatchFromCtx(ctx)
	batchID := ""
	if queue != nil {
		batchID = queue.BatchID()
	}

	task := store.TeamTask{
		TeamID:          teamID,
		Title:           p.Subject,
		Description:     p.Description,
		Status:          status,
		AssignedTo:      p.Assignee,
		CreatedBy:       agentID,
		BlockedBy:       blockedBy,
		ParentID:        p.ParentID,
		Priority:        p.Priority,
		OwnerAgentID:    agentID,
		BatchID:         batchID,
		RequireApproval: p.RequireApproval,
		MaxRetries:      1,
	}

	if queue != nil {
		// Queue mode: insert to DB (for blocked_by resolution) but don't dispatch.
		taskID, err := s.CreateTask(ctx, task)
		if err != nil {
			return errorResult("failed to create task: " + err.Error()), nil
		}
		if p.ParentID != "" {
			if err := s.IncrementSubtaskCount(ctx, p.ParentID); err != nil {
				log.Printf("[team_tasks] warning: failed to increment subtask count for %s: %v", p.ParentID, err)
			}
		}
		task.ID = taskID
		queue.Push(task)

		created, _ := s.GetTask(ctx, taskID)
		identifier := taskID
		if created != nil && created.Identifier != "" {
			identifier = created.Identifier
		}
		return &canonical.ToolResult{
			Content: fmt.Sprintf("Task created: %s (id: %s) — %s [queued for dispatch after this turn]", identifier, taskID, p.Subject),
		}, nil
	}

	// Direct dispatch mode: insert + launch immediately.
	taskID, err := executor.Dispatch(ctx, task)
	if err != nil {
		return errorResult("failed to create task: " + err.Error()), nil
	}
	if p.ParentID != "" {
		if err := s.IncrementSubtaskCount(ctx, p.ParentID); err != nil {
			log.Printf("[team_tasks] warning: failed to increment subtask count for %s: %v", p.ParentID, err)
		}
	}

	created, _ := s.GetTask(ctx, taskID)
	identifier := taskID
	if created != nil && created.Identifier != "" {
		identifier = created.Identifier
	}

	statusMsg := status
	if p.Assignee != "" && status == "pending" {
		statusMsg = "dispatched to " + p.Assignee
	}
	return &canonical.ToolResult{
		Content: fmt.Sprintf("Task created: %s (id: %s) — %s [%s]", identifier, taskID, p.Subject, statusMsg),
	}, nil
}

// taskShortID returns the first 8 chars of a task ID, or the full ID if shorter.
func taskShortID(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// isTerminal returns true if the status represents a final state.
func isTerminal(status string) bool {
	return status == "completed" || status == "cancelled" || status == "failed"
}

// checkDuplicate queries for a recent task with the same title and assignee.
func checkDuplicate(ctx context.Context, s store.Store, teamID, title, assignee string) *store.TeamTask {
	dup, err := s.FindDuplicateTask(ctx, teamID, title, assignee)
	if err != nil {
		return nil
	}
	return dup
}

func ttList(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	markListed(ctx) // Satisfies list-before-create guard.

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
			id = t.ID
		}
		fmt.Fprintf(&sb, "- [%s] %s (id: %s): %s%s%s\n", t.Status, id, t.ID, t.Title, assignee, progress)
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
		id = task.ID
	}
	fmt.Fprintf(&sb, "**%s** (id: %s) — %s\n", id, task.ID, task.Title)
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
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s cancelled.", taskShortID(p.TaskID))}, nil
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
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s assigned to %s.", taskShortID(p.TaskID), p.Assignee)}, nil
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
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s completed.", taskShortID(p.TaskID))}, nil
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
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s progress: %d%%", taskShortID(p.TaskID), p.Percent)}, nil
}

func ttComment(ctx context.Context, s store.Store, executor TeamTaskExecutor, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	t, _, errResult := requireRole(ctx, s, agentID, "")
	if errResult != nil {
		return errResult, nil
	}
	_ = t

	if p.TaskID == "" || p.Text == "" {
		return errorResult("task_id and text are required for comment"), nil
	}

	commentType := "note"

	// Detect blocker type from the text prefix (e.g. "blocker: ...").
	if p.Status == "blocker" || strings.HasPrefix(strings.ToLower(p.Text), "blocker:") {
		commentType = "blocker"
	}

	comment := store.TeamTaskComment{
		ID:          uuid.NewString(),
		TaskID:      p.TaskID,
		AgentID:     agentID,
		Content:     p.Text,
		CommentType: commentType,
		CreatedAt:   time.Now(),
	}

	if _, err := s.CreateComment(ctx, comment); err != nil {
		return errorResult("failed to add comment: " + err.Error()), nil
	}

	// Blocker escalation: auto-fail the task and notify the lead.
	if commentType == "blocker" {
		task, _ := s.GetTask(ctx, p.TaskID)
		teamID := ""
		if task != nil {
			teamID = task.TeamID
		}
		s.UpdateTask(ctx, p.TaskID, map[string]any{
			"status":        "failed",
			"error_message": fmt.Sprintf("[blocker] %s", p.Text),
		})
		// Emit event so the notifier wakes the lead.
		if teamID != "" {
			executor.EmitTaskFailed(ctx, teamID, p.TaskID)
		}
		return &canonical.ToolResult{Content: "Blocker added. Task failed and escalated to lead."}, nil
	}

	return &canonical.ToolResult{Content: "Comment added."}, nil
}

func ttSearch(ctx context.Context, s store.Store, p *teamTasksParams) (*canonical.ToolResult, error) {
	markListed(ctx) // Satisfies list-before-create guard.

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
			id = t.ID
		}
		fmt.Fprintf(&sb, "- [%s] %s (id: %s): %s\n", t.Status, id, t.ID, t.Title)
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
	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s approved and completed.", taskShortID(p.TaskID))}, nil
}

func ttReject(ctx context.Context, s store.Store, executor TeamTaskExecutor, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
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

	// Reject re-queues the task as pending so it can be reworked.
	// Reset dispatch_attempts so the circuit breaker doesn't trip on rework dispatch.
	if err := s.UpdateTask(ctx, p.TaskID, map[string]any{
		"status":             "pending",
		"error_message":      p.Text,
		"dispatch_attempts":  0,
	}); err != nil {
		return errorResult("failed to reject: " + err.Error()), nil
	}

	// Add rejection comment with feedback.
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

	// Re-dispatch to the same assignee with the feedback context.
	if task.AssignedTo != "" {
		task.Status = "pending"
		task.DispatchAttempts = 0
		executor.LaunchIfReady(ctx, *task)
		return &canonical.ToolResult{Content: fmt.Sprintf("Task %s rejected and re-dispatched to %s.", taskShortID(p.TaskID), task.AssignedTo)}, nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Task %s rejected. No assignee — assign it to re-dispatch.", taskShortID(p.TaskID))}, nil
}

func ttSend(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	if p.Text == "" {
		return errorResult("text is required for send"), nil
	}

	team, _, err := s.GetTeamByAgent(ctx, agentID)
	if err != nil || team == nil {
		return errorResult("you are not a member of any team"), nil
	}

	msg := store.TeamMessage{
		ID:        uuid.NewString(),
		TeamID:    team.ID,
		FromAgent: agentID,
		ToAgent:   p.ToAgent, // Empty = broadcast.
		Content:   p.Text,
	}
	if err := s.SendTeamMessage(ctx, msg); err != nil {
		return errorResult("failed to send message: " + err.Error()), nil
	}

	if p.ToAgent != "" {
		return &canonical.ToolResult{Content: fmt.Sprintf("Message sent to %s.", p.ToAgent)}, nil
	}
	return &canonical.ToolResult{Content: "Message broadcast to team."}, nil
}

func ttInbox(ctx context.Context, s store.Store, agentID string, p *teamTasksParams) (*canonical.ToolResult, error) {
	unreadOnly := true
	if p.UnreadOnly != nil {
		unreadOnly = *p.UnreadOnly
	}

	msgs, err := s.GetTeamMessages(ctx, agentID, unreadOnly)
	if err != nil {
		return errorResult("failed to get messages: " + err.Error()), nil
	}
	if len(msgs) == 0 {
		if unreadOnly {
			return &canonical.ToolResult{Content: "No unread messages."}, nil
		}
		return &canonical.ToolResult{Content: "No messages."}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Messages (%d):\n", len(msgs)))
	for _, m := range msgs {
		readTag := ""
		if !m.Read {
			readTag = " [NEW]"
		}
		target := "broadcast"
		if m.ToAgent != "" {
			target = "to " + m.ToAgent
		}
		sb.WriteString(fmt.Sprintf("  From %s (%s)%s: %s\n", m.FromAgent, target, readTag, m.Content))

		// Mark as read.
		if !m.Read {
			s.MarkMessageRead(ctx, m.ID)
		}
	}
	return &canonical.ToolResult{Content: sb.String()}, nil
}

// --- List-before-create guard ---

type teamTasksListedKey struct{}

// TeamTasksGuard tracks whether the lead has called list/search before create.
// Must be injected into the context as a pointer so tool calls can mutate it.
type TeamTasksGuard struct {
	Listed bool
}

// hasGuard checks if a TeamTasksGuard is present in the context (lead agents only).
func hasGuard(ctx context.Context) bool {
	_, ok := ctx.Value(teamTasksListedKey{}).(*TeamTasksGuard)
	return ok
}

// hasListedTasks checks if the lead has called list/search in this run context.
func hasListedTasks(ctx context.Context) bool {
	g, _ := ctx.Value(teamTasksListedKey{}).(*TeamTasksGuard)
	return g != nil && g.Listed
}

// markListed sets the guard flag — called from list/search handlers.
func markListed(ctx context.Context) {
	if g, ok := ctx.Value(teamTasksListedKey{}).(*TeamTasksGuard); ok && g != nil {
		g.Listed = true
	}
}

// WithTeamTasksGuard injects a fresh guard into the context.
func WithTeamTasksGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, teamTasksListedKey{}, &TeamTasksGuard{})
}

// WithTeamTasksGuardValue injects an existing guard pointer into the context.
// Used to share guard state across iterations within a single Run().
func WithTeamTasksGuardValue(ctx context.Context, guard *TeamTasksGuard) context.Context {
	return context.WithValue(ctx, teamTasksListedKey{}, guard)
}

// listMemberNames returns a comma-separated list of member agent IDs for error messages.
func listMemberNames(ctx context.Context, s store.Store, teamID string) string {
	members, err := s.ListTeamMembers(ctx, teamID)
	if err != nil || len(members) == 0 {
		return "(no members found)"
	}
	var names []string
	for _, m := range members {
		if m.Role != "lead" {
			names = append(names, m.AgentID+" ("+m.DisplayName+")")
		}
	}
	if len(names) == 0 {
		return "(no members found)"
	}
	return strings.Join(names, ", ")
}
