package orchestration

import (
	"context"
	"log"
	"strings"

	"github.com/xoai/sageclaw/pkg/store"
)

// Team represents a configured team of agents.
type Team struct {
	ID      string
	Name    string
	LeadID  string
	Members []string
}

// TeamManager provides team operations.
type TeamManager struct {
	store store.Store
	teams []Team
}

// NewTeamManager creates a team manager.
func NewTeamManager(s store.Store, teams []Team) *TeamManager {
	return &TeamManager{store: s, teams: teams}
}

// GetTeam returns a team by ID.
func (tm *TeamManager) GetTeam(teamID string) (*Team, bool) {
	for _, t := range tm.teams {
		if t.ID == teamID {
			return &t, true
		}
	}
	return nil, false
}

// CreateTask creates a task on the team board.
func (tm *TeamManager) CreateTask(ctx context.Context, teamID, title, description, createdBy string) (string, error) {
	return tm.store.CreateTask(ctx, store.TeamTask{
		TeamID:      teamID,
		Title:       title,
		Description: description,
		CreatedBy:   createdBy,
	})
}

// ClaimTask assigns a task to an agent.
func (tm *TeamManager) ClaimTask(ctx context.Context, taskID, agentID string) error {
	return tm.store.ClaimTask(ctx, taskID, agentID)
}

// CompleteTask marks a task as done with a result, then auto-unblocks dependents.
func (tm *TeamManager) CompleteTask(ctx context.Context, taskID, result string) error {
	if err := tm.store.CompleteTask(ctx, taskID, result); err != nil {
		return err
	}

	// Auto-unblock: find tasks blocked by this one and check if all deps are met.
	tm.autoUnblock(ctx, taskID)

	return nil
}

// autoUnblock finds tasks whose blocked_by includes taskID and unblocks them
// if all their dependencies are now completed.
func (tm *TeamManager) autoUnblock(ctx context.Context, completedTaskID string) {
	// Get all blocked tasks across all teams.
	for _, team := range tm.teams {
		tasks, err := tm.store.ListTasks(ctx, team.ID, "blocked")
		if err != nil {
			continue
		}

		for _, task := range tasks {
			if task.BlockedBy == "" {
				continue
			}

			// Check if this task depends on the completed task.
			deps := strings.Split(task.BlockedBy, ",")
			dependsOnCompleted := false
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep == completedTaskID || strings.HasPrefix(completedTaskID, dep) || strings.HasPrefix(dep, completedTaskID[:min(8, len(completedTaskID))]) {
					dependsOnCompleted = true
					break
				}
			}

			if !dependsOnCompleted {
				continue
			}

			// Check if ALL dependencies are completed.
			allMet := true
			for _, dep := range deps {
				dep = strings.TrimSpace(dep)
				if dep == "" {
					continue
				}
				// Check if this dependency is completed.
				depTasks, _ := tm.store.ListTasks(ctx, team.ID, "")
				found := false
				for _, dt := range depTasks {
					if dt.ID == dep || strings.HasPrefix(dt.ID, dep) {
						if dt.Status == "completed" {
							found = true
						}
						break
					}
				}
				if !found {
					allMet = false
					break
				}
			}

			if allMet {
				// Unblock: change status from "blocked" to "open" (or "claimed" if assigned).
				newStatus := "open"
				if task.AssignedTo != "" {
					newStatus = "claimed"
				}
				tm.store.UpdateTaskStatus(ctx, task.ID, newStatus)
				log.Printf("team: auto-unblocked task %s (%s) — all dependencies met", task.ID[:8], task.Title)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// UpdateTaskStatus changes a task's status directly.
func (tm *TeamManager) UpdateTaskStatus(ctx context.Context, taskID, status string) error {
	return tm.store.UpdateTaskStatus(ctx, taskID, status)
}

// ListTasks returns tasks for a team, optionally filtered by status.
func (tm *TeamManager) ListTasks(ctx context.Context, teamID, status string) ([]store.TeamTask, error) {
	return tm.store.ListTasks(ctx, teamID, status)
}

// SendMessage sends a message in the team mailbox.
func (tm *TeamManager) SendMessage(ctx context.Context, teamID, fromAgent, toAgent, content string) error {
	return tm.store.SendTeamMessage(ctx, store.TeamMessage{
		TeamID:    teamID,
		FromAgent: fromAgent,
		ToAgent:   toAgent,
		Content:   content,
	})
}

// GetMessages returns messages for an agent.
func (tm *TeamManager) GetMessages(ctx context.Context, agentID string, unreadOnly bool) ([]store.TeamMessage, error) {
	return tm.store.GetTeamMessages(ctx, agentID, unreadOnly)
}

// MarkRead marks a message as read.
func (tm *TeamManager) MarkRead(ctx context.Context, messageID string) error {
	return tm.store.MarkMessageRead(ctx, messageID)
}
