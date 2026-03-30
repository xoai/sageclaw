package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
)

// WakeLeadFunc is called when the lead agent needs to process inbox results.
// It receives the lead's agent ID, the team ID, and a system message with results.
type WakeLeadFunc func(ctx context.Context, leadAgentID, teamID, systemMessage string)

// TeamProgressNotifier monitors task events and decides when to wake the lead agent.
// It implements channel-adaptive behavior:
//   - progressive (default): wake lead when the entire batch completes
//   - detailed: wake lead immediately on each task completion
type TeamProgressNotifier struct {
	store     store.Store
	executor  *TeamExecutor
	wakeLead  WakeLeadFunc

	mu           sync.Mutex
	batchCounts  map[string]int // batchID → total tasks in batch (set on first create)
}

// NewTeamProgressNotifier creates a notifier.
func NewTeamProgressNotifier(s store.Store, exec *TeamExecutor, wake WakeLeadFunc) *TeamProgressNotifier {
	return &TeamProgressNotifier{
		store:       s,
		executor:    exec,
		wakeLead:    wake,
		batchCounts: make(map[string]int),
	}
}

// HandleEvent processes a team event and may trigger lead wakeup.
// Call this from the team executor's event handler.
func (n *TeamProgressNotifier) HandleEvent(e agent.Event) {
	switch e.Type {
	case agent.EventTeamTaskCreated:
		n.trackBatch(e)
	case agent.EventTeamTaskCompleted, agent.EventTeamTaskFailed:
		n.onTaskDone(e)
	}
}

// trackBatch records a new task's batch membership for progressive mode.
func (n *TeamProgressNotifier) trackBatch(e agent.Event) {
	// AgentID carries teamID, Text carries taskID.
	teamID := e.AgentID
	taskID := e.Text
	if teamID == "" || taskID == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	task, err := n.store.GetTask(ctx, taskID)
	if err != nil || task == nil || task.BatchID == "" {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	n.batchCounts[task.BatchID]++
}

// onTaskDone checks whether the lead should be woken up based on verbosity settings.
func (n *TeamProgressNotifier) onTaskDone(e agent.Event) {
	teamID := e.AgentID
	if teamID == "" || n.wakeLead == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Load team to get verbosity and lead ID.
	team, err := n.store.GetTeam(ctx, teamID)
	if err != nil || team == nil {
		return
	}

	verbosity := n.getVerbosity(team)
	inbox := n.executor.GetInbox(teamID)

	switch verbosity {
	case "detailed":
		// Wake immediately on every completion.
		n.wakeIfReady(ctx, team, inbox)

	default: // "progressive"
		// Wake only when the entire batch is complete, or if no batch, wake on any completion.
		taskID := e.Text
		task, err := n.store.GetTask(ctx, taskID)
		if err != nil || task == nil {
			n.wakeIfReady(ctx, team, inbox)
			return
		}

		if task.BatchID == "" {
			// No batch — wake immediately.
			n.wakeIfReady(ctx, team, inbox)
			return
		}

		// Check if this batch is fully complete.
		n.mu.Lock()
		totalInBatch := n.batchCounts[task.BatchID]
		n.mu.Unlock()

		if totalInBatch > 0 && inbox.BatchComplete(task.BatchID, totalInBatch) {
			n.wakeIfReady(ctx, team, inbox)
		}
	}
}

// wakeIfReady constructs a system message from inbox items and calls wakeLead.
func (n *TeamProgressNotifier) wakeIfReady(ctx context.Context, team *store.Team, inbox *TeamInbox) {
	if !inbox.HasItems() {
		return
	}

	completions := inbox.ConsumeAll()
	if len(completions) == 0 {
		return
	}

	msg := n.buildWakeupMessage(completions)
	log.Printf("[team] waking lead %s for team %s with %d results", team.LeadID, team.ID, len(completions))
	n.wakeLead(ctx, team.LeadID, team.ID, msg)
}

// buildWakeupMessage constructs the system message for the lead from completed tasks.
func (n *TeamProgressNotifier) buildWakeupMessage(completions []TaskCompletion) string {
	var sb strings.Builder
	sb.WriteString("Task results ready:\n\n")

	for _, c := range completions {
		sb.WriteString(fmt.Sprintf("<team-task-result task-id=%q agent=%q status=%q>\n",
			c.TaskID, c.AgentKey, c.Status))
		if c.Status == "failed" {
			sb.WriteString(fmt.Sprintf("FAILED: %s\n", c.Error))
		} else {
			sb.WriteString(c.Result)
			sb.WriteString("\n")
		}
		sb.WriteString("</team-task-result>\n\n")
	}

	sb.WriteString("Synthesize these results for the user. Attribute each contribution with **[Role]** prefix.")
	return sb.String()
}

// getVerbosity reads chat_verbosity from team settings JSON.
func (n *TeamProgressNotifier) getVerbosity(team *store.Team) string {
	if team.Settings == "" {
		return "progressive"
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(team.Settings), &settings); err != nil {
		return "progressive"
	}
	if v, ok := settings["chat_verbosity"].(string); ok && v != "" {
		return v
	}
	return "progressive"
}
