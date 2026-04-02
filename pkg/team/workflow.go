package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
)

// Workflow states.
const (
	StateAnalyze    = "analyze"
	StatePlan       = "plan"
	StateCreate     = "create"
	StateExecute    = "execute"
	StateMonitor    = "monitor"
	StateSynthesize = "synthesize"
	StateDeliver    = "deliver"
	StateComplete   = "complete"
	StateCancelled  = "cancelled"
	StateFailed     = "failed"
)

// IsTerminal returns true if the state is a terminal state.
func IsTerminal(state string) bool {
	return state == StateComplete || state == StateCancelled || state == StateFailed
}

// validTransitions defines allowed state transitions.
var validTransitions = map[string][]string{
	StateAnalyze:    {StatePlan, StateFailed},
	StatePlan:       {StateCreate, StateFailed},
	StateCreate:     {StateExecute, StateFailed},
	StateExecute:    {StateMonitor, StateFailed, StateCancelled},
	StateMonitor:    {StateSynthesize, StateFailed, StateCancelled},
	StateSynthesize: {StateDeliver, StateFailed},
	StateDeliver:    {StateComplete, StateFailed},
}

// AnalyzeResult is the structured output from the _workflow_analyze tool call.
type AnalyzeResult struct {
	Delegate   bool    `json:"delegate"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason"`
}

// PlanTask is a single task in the plan from _workflow_plan.
type PlanTask struct {
	Subject     string   `json:"subject"`
	Assignee    string   `json:"assignee"`
	Description string   `json:"description"`
	BlockedBy   []string `json:"blocked_by,omitempty"`
}

// PlanResult is the structured output from the _workflow_plan tool call.
type PlanResult struct {
	Tasks        []PlanTask `json:"tasks"`
	Announcement string     `json:"announcement"`
}

// WorkflowEngine manages deterministic team workflows.
type WorkflowEngine struct {
	store        store.Store
	executor     *TeamExecutor
	eventHandler agent.EventHandler
	monitor      *WorkflowMonitor
	relay        *WorkflowRelay
}

// NewWorkflowEngine creates a new workflow engine.
func NewWorkflowEngine(s store.Store, exec *TeamExecutor, onEvent agent.EventHandler) *WorkflowEngine {
	w := &WorkflowEngine{
		store:        s,
		executor:     exec,
		eventHandler: onEvent,
	}
	w.monitor = NewWorkflowMonitor(s, w, onEvent)
	return w
}

// Monitor returns the workflow monitor for event handler wiring.
func (w *WorkflowEngine) Monitor() *WorkflowMonitor {
	return w.monitor
}

// SetRelay sets the workflow relay for progress forwarding.
func (w *WorkflowEngine) SetRelay(relay *WorkflowRelay) {
	w.relay = relay
}

// transition advances a workflow to a new state with optimistic concurrency.
func (w *WorkflowEngine) transition(ctx context.Context, wf *store.TeamWorkflow, newState string, fields map[string]any) error {
	// Validate transition.
	allowed := validTransitions[wf.State]
	valid := false
	for _, s := range allowed {
		if s == newState {
			valid = true
			break
		}
	}
	// Any non-terminal state can transition to cancelled.
	if newState == StateCancelled && !IsTerminal(wf.State) {
		valid = true
	}
	if !valid {
		return fmt.Errorf("invalid transition: %s → %s", wf.State, newState)
	}

	oldState := wf.State
	if err := w.store.UpdateWorkflowState(ctx, wf.ID, newState, wf.Version, fields); err != nil {
		return err
	}
	wf.State = newState
	wf.Version++

	log.Printf("[workflow:%s] %s → %s", wf.ID[:min(8, len(wf.ID))], oldState, newState)

	if w.eventHandler != nil {
		w.eventHandler(agent.Event{
			Type:    agent.EventWorkflowStateChanged,
			AgentID: wf.TeamID,
			Text:    fmt.Sprintf("%s→%s", oldState, newState),
			TeamData: &agent.TeamEventData{
				TeamID: wf.TeamID,
			},
		})
	}
	return nil
}

// HandleAnalyze processes the _workflow_analyze tool call result.
// Returns a tool result string and whether a workflow was started.
func (w *WorkflowEngine) HandleAnalyze(ctx context.Context, teamID, sessionID, userMessage, toolInput string) (string, bool, error) {
	// Concurrent workflow guard.
	active, err := w.store.GetActiveWorkflow(ctx, teamID, sessionID)
	if err != nil {
		return "", false, fmt.Errorf("checking active workflow: %w", err)
	}
	if active != nil {
		taskCount := 0
		if active.TaskIDs != "" {
			taskCount = len(strings.Split(active.TaskIDs, ","))
		}
		return fmt.Sprintf("A delegation is already in progress (%d tasks running, state: %s). Wait for it to complete or ask the user to cancel it.", taskCount, active.State), false, nil
	}

	// Parse the analyze result.
	var result AnalyzeResult
	if err := json.Unmarshal([]byte(toolInput), &result); err != nil {
		return fmt.Sprintf("Invalid analyze output: %v. Expected JSON with delegate, confidence, reason fields.", err), false, nil
	}

	// Gate: low confidence.
	if result.Delegate && result.Confidence < 0.6 {
		return "Confidence is low. Ask the user: should you handle this directly or delegate to the team?", false, nil
	}

	// No delegation needed.
	if !result.Delegate {
		return fmt.Sprintf("Understood — handling directly. Reason: %s", result.Reason), false, nil
	}

	// Create workflow and advance to plan state.
	wf := store.TeamWorkflow{
		TeamID:      teamID,
		SessionID:   sessionID,
		UserMessage: userMessage,
	}
	id, err := w.store.CreateWorkflow(ctx, wf)
	if err != nil {
		return "", false, fmt.Errorf("creating workflow: %w", err)
	}

	// Fetch the created workflow for transition.
	created, err := w.store.GetWorkflow(ctx, id)
	if err != nil || created == nil {
		return "", false, fmt.Errorf("fetching created workflow: %w", err)
	}

	if err := w.transition(ctx, created, StatePlan, nil); err != nil {
		return "", false, fmt.Errorf("transitioning to plan: %w", err)
	}

	if w.eventHandler != nil {
		w.eventHandler(agent.Event{
			Type:    agent.EventWorkflowStarted,
			AgentID: teamID,
			Text:    id,
			TeamData: &agent.TeamEventData{
				TeamID: teamID,
			},
		})
	}

	return fmt.Sprintf("Delegation approved (confidence: %.0f%%). Workflow %s started. Now call _workflow_plan to create the task breakdown.", result.Confidence*100, id[:8]), true, nil
}

// HandlePlan processes the _workflow_plan tool call result.
// Returns a tool result string for the LLM.
func (w *WorkflowEngine) HandlePlan(ctx context.Context, teamID, sessionID, toolInput string, memberRoster []string) (string, error) {
	// Find active workflow in plan state.
	wf, err := w.store.GetActiveWorkflow(ctx, teamID, sessionID)
	if err != nil {
		return "", fmt.Errorf("getting active workflow: %w", err)
	}
	if wf == nil {
		return "No active workflow. Call _workflow_analyze first to decide whether to delegate.", nil
	}
	if wf.State != StatePlan {
		return fmt.Sprintf("Workflow is in state %q, not %q. Call _workflow_analyze first.", wf.State, StatePlan), nil
	}

	// Parse the plan.
	var plan PlanResult
	if err := json.Unmarshal([]byte(toolInput), &plan); err != nil {
		return fmt.Sprintf("Invalid plan output: %v", err), nil
	}

	// Validate.
	errors := validatePlan(plan, memberRoster)
	if len(errors) > 0 {
		// Count previous attempts from the error field format "attempt:N|details".
		attempt := 1
		if wf.Error != "" {
			// Parse attempt count from stored error.
			if strings.HasPrefix(wf.Error, "attempt:") {
				parts := strings.SplitN(wf.Error, "|", 2)
				if len(parts) >= 1 {
					var n int
					if _, err := fmt.Sscanf(parts[0], "attempt:%d", &n); err == nil {
						attempt = n + 1
					}
				}
			} else {
				attempt = 2 // Legacy: non-empty error without prefix = 1 prior attempt.
			}
		}

		if attempt >= 3 {
			// Third failure — mark workflow failed.
			_ = w.store.UpdateWorkflowState(ctx, wf.ID, StateFailed, wf.Version, map[string]any{
				"error": fmt.Sprintf("attempt:%d|%s", attempt, strings.Join(errors, "; ")),
			})
			return fmt.Sprintf("Task plan validation failed %d times. Workflow cancelled:\n%s", attempt, strings.Join(errors, "\n")), nil
		}

		if attempt >= 2 {
			// Second failure — escalate to user.
			errRecord := fmt.Sprintf("attempt:%d|%s", attempt, strings.Join(errors, "; "))
			if err := w.store.UpdateWorkflowState(ctx, wf.ID, StatePlan, wf.Version, map[string]any{"error": errRecord}); err != nil {
				return "", fmt.Errorf("updating workflow error: %w", err)
			}
			return fmt.Sprintf("Task plan validation failed twice:\n%s\nAsk the user for guidance on how to structure the delegation.", strings.Join(errors, "\n")), nil
		}

		// First failure — retry with errors.
		errRecord := fmt.Sprintf("attempt:%d|%s", attempt, strings.Join(errors, "; "))
		if err := w.store.UpdateWorkflowState(ctx, wf.ID, StatePlan, wf.Version, map[string]any{"error": errRecord}); err != nil {
			return "", fmt.Errorf("updating workflow error: %w", err)
		}
		return fmt.Sprintf("Plan validation failed:\n%s\nFix these issues and call _workflow_plan again.", strings.Join(errors, "\n")), nil
	}

	// Persist plan and advance to create.
	planJSON, _ := json.Marshal(plan)
	if err := w.transition(ctx, wf, StateCreate, map[string]any{
		"plan_json":    string(planJSON),
		"announcement": plan.Announcement,
		"error":        "", // Clear any previous validation error.
	}); err != nil {
		return "", fmt.Errorf("transitioning to create: %w", err)
	}

	// Execute CREATE deterministically.
	result, err := w.executeCreate(ctx, wf, plan)
	if err != nil {
		// Mark workflow failed.
		_ = w.store.UpdateWorkflowState(ctx, wf.ID, StateFailed, wf.Version, map[string]any{
			"error": err.Error(),
		})
		return fmt.Sprintf("Task creation failed: %v", err), nil
	}

	return result, nil
}

// executeCreate deterministically creates tasks from the validated plan.
func (w *WorkflowEngine) executeCreate(ctx context.Context, wf *store.TeamWorkflow, plan PlanResult) (string, error) {
	taskIDMap := make(map[string]string) // "$TASK_0" → real task ID
	var createdIDs []string

	for i, pt := range plan.Tasks {
		// Resolve blocked_by references.
		var blockedBy []string
		for _, ref := range pt.BlockedBy {
			realID, ok := taskIDMap[ref]
			if !ok {
				// Rollback all created tasks.
				w.rollbackTasks(ctx, createdIDs)
				return "", fmt.Errorf("unresolvable blocked_by reference %q in task %d", ref, i)
			}
			blockedBy = append(blockedBy, realID)
		}

		task := store.TeamTask{
			TeamID:      wf.TeamID,
			Title:       pt.Subject,
			Description: pt.Description,
			AssignedTo:  pt.Assignee,
			CreatedBy:   wf.TeamID, // Lead's team ID as creator.
			BlockedBy:   strings.Join(blockedBy, ","),
			BatchID:     wf.ID,
			Status:      "pending",
		}
		if len(blockedBy) > 0 {
			task.Status = "blocked"
		}

		taskID, err := w.executor.Dispatch(ctx, task)
		if err != nil {
			// Rollback all already-created tasks.
			w.rollbackTasks(ctx, createdIDs)
			return "", fmt.Errorf("dispatching task %d (%s): %w", i, pt.Subject, err)
		}

		ref := fmt.Sprintf("$TASK_%d", i)
		taskIDMap[ref] = taskID
		createdIDs = append(createdIDs, taskID)
	}

	// Persist task IDs on the workflow.
	taskIDsStr := strings.Join(createdIDs, ",")
	if err := w.transition(ctx, wf, StateExecute, map[string]any{
		"task_ids": taskIDsStr,
	}); err != nil {
		return "", fmt.Errorf("transitioning to execute: %w", err)
	}

	// Start monitoring task events for this workflow.
	if w.monitor != nil {
		w.monitor.StartMonitoring(wf.ID, wf.TeamID, wf.SessionID, createdIDs)
	}

	// Register relay for progress forwarding to user's session.
	if w.relay != nil {
		// Look up the lead agent for pause/resume detection.
		team, _ := w.store.GetTeam(ctx, wf.TeamID)
		leadID := ""
		if team != nil {
			leadID = team.LeadID
		}
		w.relay.RegisterWorkflow(wf.ID, wf.SessionID, leadID, wf.TeamID)
		w.relay.EmitDelegating(wf.ID, wf.SessionID, len(createdIDs))
	}

	return fmt.Sprintf("%s\n\n[%d tasks created and dispatched. The system will monitor progress and deliver results when all tasks complete.]",
		plan.Announcement, len(createdIDs)), nil
}

// HandleLeadRunComplete is called after a lead agent's run completes.
// If a workflow is in SYNTHESIZE state, checks synthesis quality then
// transitions to DELIVER → COMPLETE. responseText is the lead's output.
func (w *WorkflowEngine) HandleLeadRunComplete(ctx context.Context, teamID, sessionID, responseText string) {
	// Look up workflow by team+session.
	if teamID == "" || sessionID == "" {
		return // Both required — caller must provide.
	}
	wf, err := w.store.GetActiveWorkflow(ctx, teamID, sessionID)
	if err != nil || wf == nil {
		return
	}
	if wf.State != StateSynthesize {
		return
	}

	// Synthesis quality gate: check that the response references at least one task subject.
	if responseText != "" && wf.ResultsJSON != "" {
		if !synthesisReferencesResults(responseText, wf.ResultsJSON) {
			// Track retry attempts via error field.
			if !strings.Contains(wf.Error, "synthesis_retry") {
				// First failure — retry with explicit instruction.
				_ = w.store.UpdateWorkflowState(ctx, wf.ID, StateSynthesize, wf.Version, map[string]any{
					"error": "synthesis_retry:1",
				})
				log.Printf("[workflow:%s] synthesis did not reference task outputs — retrying", wf.ID[:min(8, len(wf.ID))])
				// Re-wake with stronger instruction (handled by stale recovery ticker).
				return
			}
			// Already retried once — accept whatever we got.
			log.Printf("[workflow:%s] synthesis retry still incomplete — accepting", wf.ID[:min(8, len(wf.ID))])
		}
	}

	// Transition SYNTHESIZE → DELIVER → COMPLETE with retry for SQLITE_BUSY.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
			// Re-fetch workflow to get current version after retry.
			wf, err = w.store.GetActiveWorkflow(ctx, teamID, sessionID)
			if err != nil || wf == nil || wf.State == StateComplete || wf.State == StateDeliver {
				return // Already completed by another goroutine or gone.
			}
		}
		err = w.transition(ctx, wf, StateDeliver, nil)
		if err == nil {
			break
		}
		log.Printf("[workflow:%s] transition to deliver attempt %d: %v", wf.ID[:min(8, len(wf.ID))], attempt+1, err)
	}
	if err != nil {
		return // Stale recovery will pick this up.
	}
	if err := w.transition(ctx, wf, StateComplete, nil); err != nil {
		log.Printf("[workflow:%s] failed to transition to complete: %v", wf.ID[:min(8, len(wf.ID))], err)
		return
	}

	// Set unread indicator on the user's session.
	_ = w.store.UpdateSessionMetadata(ctx, sessionID, map[string]string{"has_unread": "true"})

	if w.eventHandler != nil {
		w.eventHandler(agent.Event{
			Type:    agent.EventWorkflowCompleted,
			AgentID: wf.TeamID,
			Text:    wf.ID,
			TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
		})
	}
}

// synthesisReferencesResults checks if the LLM response references any task subjects.
func synthesisReferencesResults(response, resultsJSON string) bool {
	var results []TaskResult
	if err := json.Unmarshal([]byte(resultsJSON), &results); err != nil {
		return true // Can't parse — assume OK.
	}
	responseLower := strings.ToLower(response)
	for _, r := range results {
		if r.Status != "completed" {
			continue
		}
		// Check if any significant word from the task title appears in the response.
		words := strings.Fields(strings.ToLower(r.Title))
		for _, word := range words {
			if len(word) >= 4 && strings.Contains(responseLower, word) {
				return true
			}
		}
	}
	return false
}

// rollbackTasks cancels all already-created tasks on partial failure.
func (w *WorkflowEngine) rollbackTasks(ctx context.Context, taskIDs []string) {
	for _, id := range taskIDs {
		if err := w.store.CancelTask(ctx, id); err != nil {
			log.Printf("[workflow] rollback: failed to cancel task %s: %v", id[:min(8, len(id))], err)
		}
	}
}

// validatePlan checks the plan against the spec's validation gates.
func validatePlan(plan PlanResult, memberRoster []string) []string {
	var errs []string

	if len(plan.Tasks) == 0 {
		errs = append(errs, "Plan must have at least one task.")
		return errs
	}

	if plan.Announcement == "" {
		errs = append(errs, "Plan must include an announcement message for the user.")
	}

	rosterSet := make(map[string]bool, len(memberRoster))
	for _, m := range memberRoster {
		rosterSet[m] = true
	}

	taskRefs := make(map[string]bool)
	for i, t := range plan.Tasks {
		ref := fmt.Sprintf("$TASK_%d", i)
		taskRefs[ref] = true

		if t.Subject == "" {
			errs = append(errs, fmt.Sprintf("Task %d: subject is empty.", i))
		}
		if t.Assignee == "" {
			errs = append(errs, fmt.Sprintf("Task %d: assignee is empty.", i))
		} else if !rosterSet[t.Assignee] {
			errs = append(errs, fmt.Sprintf("Task %d: assignee %q is not in the team roster. Available: %s",
				i, t.Assignee, strings.Join(memberRoster, ", ")))
		}
		if t.Description == "" {
			errs = append(errs, fmt.Sprintf("Task %d: description is empty.", i))
		}

		// Validate blocked_by references.
		for _, dep := range t.BlockedBy {
			if !strings.HasPrefix(dep, "$TASK_") {
				errs = append(errs, fmt.Sprintf("Task %d: blocked_by %q is not a valid $TASK_N reference.", i, dep))
				continue
			}
			if dep == ref {
				errs = append(errs, fmt.Sprintf("Task %d: cannot block on itself.", i))
				continue
			}
			if !taskRefs[dep] {
				// Reference to a later task = forward reference (not allowed).
				errs = append(errs, fmt.Sprintf("Task %d: blocked_by %q references a later or nonexistent task.", i, dep))
			}
		}

		// Soft warning: compound subjects.
		lower := strings.ToLower(t.Subject)
		if strings.Contains(lower, " and ") {
			// Check if it contains action verbs on both sides.
			parts := strings.SplitN(lower, " and ", 2)
			actionVerbs := []string{"create", "write", "build", "design", "implement", "research", "analyze", "review", "test", "fix", "update"}
			hasVerb0, hasVerb1 := false, false
			for _, v := range actionVerbs {
				if strings.Contains(parts[0], v) {
					hasVerb0 = true
				}
				if len(parts) > 1 && strings.Contains(parts[1], v) {
					hasVerb1 = true
				}
			}
			if hasVerb0 && hasVerb1 {
				errs = append(errs, fmt.Sprintf("Task %d: subject %q contains multiple actions — consider splitting into separate tasks.", i, t.Subject))
			}
		}
	}

	// Check for circular dependencies.
	if cycle := detectCycle(plan.Tasks); cycle != "" {
		errs = append(errs, fmt.Sprintf("Circular dependency detected: %s", cycle))
	}

	return errs
}

// detectCycle checks for circular dependencies in blocked_by references.
func detectCycle(tasks []PlanTask) string {
	// Build adjacency: task index → indices it depends on.
	n := len(tasks)
	refToIdx := make(map[string]int, n)
	for i := range tasks {
		refToIdx[fmt.Sprintf("$TASK_%d", i)] = i
	}

	// Topological sort via DFS.
	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	colors := make([]int, n)

	var dfs func(i int) bool
	dfs = func(i int) bool {
		colors[i] = visiting
		for _, dep := range tasks[i].BlockedBy {
			j, ok := refToIdx[dep]
			if !ok {
				continue
			}
			if colors[j] == visiting {
				return true // Cycle found.
			}
			if colors[j] == unvisited && dfs(j) {
				return true
			}
		}
		colors[i] = visited
		return false
	}

	for i := 0; i < n; i++ {
		if colors[i] == unvisited && dfs(i) {
			return fmt.Sprintf("cycle involving task %d", i)
		}
	}
	return ""
}
