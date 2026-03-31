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
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// DefaultMaxConcurrent is the default concurrency limit per team.
const DefaultMaxConcurrent = 3

// DefaultTaskTimeout is the default timeout per task execution.
const DefaultTaskTimeout = 10 * time.Minute

// LoopFactory creates ephemeral Loops for task execution.
// Decouples executor from LoopPool internals.
type LoopFactory interface {
	// NewTaskLoop creates a fresh, ephemeral Loop for a member agent.
	NewTaskLoop(agentID string) *agent.Loop
}

// MaxDispatchAttempts is the maximum number of times a task can be dispatched
// before being permanently failed (circuit breaker).
const MaxDispatchAttempts = 3

// DefaultStaleTimeout is the default timeout in seconds for stale task recovery.
const DefaultStaleTimeout = 600

// TeamExecutor is the orchestration engine that dispatches tasks to member agents.
type TeamExecutor struct {
	store        store.Store
	loopFactory  LoopFactory
	eventHandler agent.EventHandler
	notifier     *TeamProgressNotifier

	mu          sync.Mutex
	inboxes     map[string]*TeamInbox           // teamID → completion inbox
	limiters    map[string]chan struct{}         // teamID → concurrency semaphore
	cancelFuncs map[string]context.CancelFunc   // taskID → cancel function
	wg          sync.WaitGroup                  // tracks in-flight execute goroutines

	// Recovery ticker lifecycle.
	recoveryStop   func()
}

// NewTeamExecutor creates an executor.
func NewTeamExecutor(
	s store.Store,
	factory LoopFactory,
	eventHandler agent.EventHandler,
) *TeamExecutor {
	return &TeamExecutor{
		store:        s,
		loopFactory:  factory,
		eventHandler: eventHandler,
		inboxes:      make(map[string]*TeamInbox),
		limiters:     make(map[string]chan struct{}),
		cancelFuncs:  make(map[string]context.CancelFunc),
	}
}

// Dispatch inserts a task and launches async execution if runnable.
// Returns the task ID immediately (non-blocking).
func (e *TeamExecutor) Dispatch(ctx context.Context, task store.TeamTask) (string, error) {
	// Validate assignee is a team member.
	if task.AssignedTo != "" {
		team, role, err := e.store.GetTeamByAgent(ctx, task.AssignedTo)
		if err != nil {
			return "", fmt.Errorf("validating assignee: %w", err)
		}
		if team == nil {
			return "", fmt.Errorf("assignee %q is not a member of any team", task.AssignedTo)
		}
		if team.ID != task.TeamID {
			return "", fmt.Errorf("assignee %q belongs to team %q, not %q", task.AssignedTo, team.ID, task.TeamID)
		}
		// Block dispatch to lead agent — tasks must go to members.
		if role == "lead" {
			return "", fmt.Errorf("cannot dispatch task to team lead %q — assign to a member instead", task.AssignedTo)
		}
	}

	// Insert task into DB.
	taskID, err := e.store.CreateTask(ctx, task)
	if err != nil {
		return "", fmt.Errorf("creating task: %w", err)
	}

	// Emit created event.
	e.emitEvent(agent.EventTeamTaskCreated, task.TeamID, taskID, nil)

	// If not blocked and has an assignee, launch execution.
	if task.BlockedBy == "" && task.AssignedTo != "" {
		e.launchExecute(ctx, task.TeamID, taskID, task.AssignedTo)
	}

	return taskID, nil
}

// launchExecute increments dispatch attempts, checks the circuit breaker,
// and starts a goroutine for task execution. All dispatch paths go through
// this method so the circuit breaker cannot be bypassed.
func (e *TeamExecutor) launchExecute(_ context.Context, teamID, taskID, agentID string) {
	// Use a fresh context for dispatch operations — the caller's context may
	// be near expiry (e.g., when unblocking dependent tasks after completion).
	dispatchCtx, dispatchCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer dispatchCancel()

	// Circuit breaker: increment and check.
	attempts, err := e.store.IncrementDispatchAttempt(dispatchCtx, taskID)
	if err != nil {
		log.Printf("[team] task %s: failed to increment dispatch attempt: %v", taskID[:min(8, len(taskID))], err)
	}
	if attempts >= MaxDispatchAttempts {
		log.Printf("[team] task %s: circuit breaker tripped after %d dispatch attempts", taskID[:min(8, len(taskID))], attempts)
		e.failTask(dispatchCtx, taskID, teamID, "max_dispatch_attempts_exceeded",
			fmt.Sprintf("task failed after %d dispatch attempts", attempts))
		return
	}

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		e.execute(teamID, taskID, agentID)
	}()
}

// execute runs a single task on a member agent. Always runs as a goroutine.
func (e *TeamExecutor) execute(teamID, taskID, agentID string) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTaskTimeout)
	defer cancel()

	// Store cancel func for cascade stop.
	e.mu.Lock()
	e.cancelFuncs[taskID] = cancel
	e.mu.Unlock()
	defer func() {
		e.mu.Lock()
		delete(e.cancelFuncs, taskID)
		e.mu.Unlock()
	}()

	// Acquire concurrency slot.
	if err := e.acquireConcurrency(ctx, teamID); err != nil {
		log.Printf("[team] task %s: failed to acquire concurrency: %v", taskID[:min(8, len(taskID))], err)
		e.failTask(ctx, taskID, teamID, "concurrency_timeout", err.Error())
		return
	}
	defer e.releaseConcurrency(teamID)

	// Claim the task (pending → in_progress).
	if err := e.store.ClaimTask(ctx, taskID, agentID); err != nil {
		log.Printf("[team] task %s: failed to claim: %v", taskID[:min(8, len(taskID))], err)
		return
	}
	e.emitEvent(agent.EventTeamTaskClaimed, teamID, taskID, nil)

	// Load task details.
	task, err := e.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		log.Printf("[team] task %s: failed to load: %v", taskID[:min(8, len(taskID))], err)
		e.failTask(ctx, taskID, teamID, "load_error", "failed to load task details")
		return
	}

	// Build task prompt.
	prompt := e.buildTaskPrompt(ctx, task)

	// Create fresh session for this task.
	session, err := e.store.CreateSessionWithKind(ctx, "internal", taskID[:min(8, len(taskID))], agentID, "team_task")
	if err != nil {
		log.Printf("[team] task %s: failed to create session: %v", taskID[:min(8, len(taskID))], err)
		e.failTask(ctx, taskID, teamID, "session_error", err.Error())
		return
	}

	// Link session to task.
	e.store.UpdateTask(ctx, taskID, map[string]any{"session_id": session.ID})

	// Create ephemeral Loop for this task.
	loop := e.loopFactory.NewTaskLoop(agentID)
	if loop == nil {
		log.Printf("[team] task %s: no loop factory output for agent %s", taskID[:min(8, len(taskID))], agentID)
		e.failTask(ctx, taskID, teamID, "agent_not_found", fmt.Sprintf("agent %q not configured", agentID))
		return
	}

	// Run the loop.
	messages := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: prompt}}},
	}
	result := loop.Run(ctx, session.ID, messages)

	// Process result.
	if result.Error != nil {
		e.handleFailure(ctx, task, teamID, result.Error)
		return
	}

	// Extract assistant response text.
	responseText := extractResponse(result.Messages)
	responseText = TruncateResult(responseText)

	// Complete the task.
	if err := e.store.CompleteTask(ctx, taskID, responseText); err != nil {
		log.Printf("[team] task %s: failed to complete: %v", taskID[:min(8, len(taskID))], err)
		e.failTask(ctx, taskID, teamID, "complete_error", err.Error())
		return
	}

	// Determine event type based on whether task went to review.
	updatedTask, _ := e.store.GetTask(ctx, taskID)
	if updatedTask != nil && updatedTask.Status == "in_review" {
		e.emitEvent(agent.EventTeamTaskReview, teamID, taskID, nil)
	} else {
		e.emitEvent(agent.EventTeamTaskCompleted, teamID, taskID, nil)
	}

	// Push to inbox.
	inbox := e.getInbox(teamID)
	inbox.Push(TaskCompletion{
		TaskID:    taskID,
		AgentKey:  agentID,
		Subject:   task.Title,
		Result:    responseText,
		Status:    "completed",
		BatchID:   task.BatchID,
	})

	// Check for tasks blocked by this one.
	unblocked, err := e.store.UnblockTasks(ctx, taskID)
	if err == nil {
		for _, t := range unblocked {
			e.emitEvent(agent.EventTeamTaskUnblocked, teamID, t.ID, nil)
			if t.AssignedTo != "" {
				e.launchExecute(ctx, teamID, t.ID, t.AssignedTo)
			}
		}
	}

	log.Printf("[team] task %s (%s) completed by %s", taskID[:min(8, len(taskID))], task.Title, agentID)
}

// handleFailure classifies errors and either retries or fails permanently.
func (e *TeamExecutor) handleFailure(ctx context.Context, task *store.TeamTask, teamID string, err error) {
	errMsg := err.Error()
	isTransient := isTransientError(errMsg)

	if isTransient && task.RetryCount < task.MaxRetries {
		log.Printf("[team] task %s: transient failure, retrying (%d/%d): %v",
			task.ID[:8], task.RetryCount+1, task.MaxRetries, err)
		if retryErr := e.store.RetryTask(ctx, task.ID); retryErr == nil {
			e.launchExecute(ctx, teamID, task.ID, task.AssignedTo)
			return
		}
	}

	e.failTask(ctx, task.ID, teamID, "execution_error", errMsg)
}

// failTask marks a task as failed and emits event.
func (e *TeamExecutor) failTask(ctx context.Context, taskID, teamID, errorType, errorMsg string) {
	e.store.UpdateTask(ctx, taskID, map[string]any{
		"status":        "failed",
		"error_message": fmt.Sprintf("[%s] %s", errorType, errorMsg),
	})
	e.emitEvent(agent.EventTeamTaskFailed, teamID, taskID, nil)

	// Push failure to inbox.
	task, _ := e.store.GetTask(ctx, taskID)
	inbox := e.getInbox(teamID)
	if task != nil {
		inbox.Push(TaskCompletion{
			TaskID:  taskID,
			Subject: task.Title,
			Status:  "failed",
			Error:   errorMsg,
			BatchID: task.BatchID,
		})
	}
}

// buildTaskPrompt constructs the prompt for a member agent's task.
func (e *TeamExecutor) buildTaskPrompt(ctx context.Context, task *store.TeamTask) string {
	var b strings.Builder
	b.WriteString(task.Title)
	if task.Description != "" {
		b.WriteString("\n\n")
		b.WriteString(task.Description)
	}

	// If task has dependencies, include their results wrapped in boundary tags.
	if task.BlockedBy != "" {
		blockers := strings.Split(task.BlockedBy, ",")
		for _, bid := range blockers {
			bid = strings.TrimSpace(bid)
			if bid == "" {
				continue
			}
			blocker, err := e.store.GetTask(ctx, bid)
			if err != nil || blocker == nil || blocker.Result == "" {
				continue
			}
			b.WriteString("\n\n")
			b.WriteString(fmt.Sprintf("<team-task-result task-id=%q title=%q>\n", bid, blocker.Title))
			b.WriteString(TruncateResult(blocker.Result))
			b.WriteString("\n</team-task-result>")
		}
	}

	return b.String()
}

// acquireConcurrency waits for a concurrency slot, respecting context.
func (e *TeamExecutor) acquireConcurrency(ctx context.Context, teamID string) error {
	e.mu.Lock()
	sem, ok := e.limiters[teamID]
	if !ok {
		sem = make(chan struct{}, e.getMaxConcurrent(teamID))
		e.limiters[teamID] = sem
	}
	e.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("cancelled while waiting for concurrency slot: %w", ctx.Err())
	}
}

// releaseConcurrency frees a concurrency slot.
func (e *TeamExecutor) releaseConcurrency(teamID string) {
	e.mu.Lock()
	sem, ok := e.limiters[teamID]
	e.mu.Unlock()
	if ok {
		<-sem
	}
}

// getMaxConcurrent reads the team's concurrency limit from settings.
func (e *TeamExecutor) getMaxConcurrent(teamID string) int {
	team, err := e.store.GetTeam(context.Background(), teamID)
	if err != nil || team == nil {
		return DefaultMaxConcurrent
	}
	if team.Settings == "" {
		return DefaultMaxConcurrent
	}
	var settings map[string]any
	if err := json.Unmarshal([]byte(team.Settings), &settings); err != nil {
		return DefaultMaxConcurrent
	}
	if v, ok := settings["max_concurrent"].(float64); ok && int(v) > 0 {
		return int(v)
	}
	return DefaultMaxConcurrent
}

// getInbox returns or creates the inbox for a team.
func (e *TeamExecutor) getInbox(teamID string) *TeamInbox {
	e.mu.Lock()
	defer e.mu.Unlock()
	inbox, ok := e.inboxes[teamID]
	if !ok {
		inbox = &TeamInbox{}
		e.inboxes[teamID] = inbox
	}
	return inbox
}

// GetInbox returns the inbox for a team (for external use by tool/wakeup).
func (e *TeamExecutor) GetInbox(teamID string) *TeamInbox {
	return e.getInbox(teamID)
}

// StartRecoveryTicker launches a goroutine that periodically recovers stale tasks.
// Returns a stop function. Call it (or Shutdown) to stop the ticker.
func (e *TeamExecutor) StartRecoveryTicker(interval time.Duration) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				e.recoverStaleTasks()
			}
		}
	}()

	stop := func() {
		select {
		case <-done:
			// Already stopped.
		default:
			close(done)
		}
		<-stopped
	}

	e.mu.Lock()
	e.recoveryStop = stop
	e.mu.Unlock()

	return stop
}

// recoverStaleTasks finds and fails tasks that have been in_progress too long.
func (e *TeamExecutor) recoverStaleTasks() {
	ctx := context.Background()
	recovered, err := e.store.RecoverStaleTasks(ctx, e.getStaleTimeout())
	if err != nil {
		log.Printf("[team] stale recovery error: %v", err)
		return
	}
	for _, task := range recovered {
		log.Printf("[team] recovered stale task %s (%s)", task.ID[:min(8, len(task.ID))], task.Title)
		e.emitEvent(agent.EventTeamTaskFailed, task.TeamID, task.ID, nil)

		// Push failure to inbox for lead notification.
		inbox := e.getInbox(task.TeamID)
		inbox.Push(TaskCompletion{
			TaskID:  task.ID,
			Subject: task.Title,
			Status:  "failed",
			Error:   "stale_timeout: task not completed within configured timeout",
			BatchID: task.BatchID,
		})
	}
}

// getStaleTimeout reads the stale timeout from the first available team's settings.
// Falls back to DefaultStaleTimeout. Uses global default since RecoverStaleTasks
// queries across all teams.
func (e *TeamExecutor) getStaleTimeout() int {
	return DefaultStaleTimeout
}

// CancelTask cancels a specific in-flight task by ID.
func (e *TeamExecutor) CancelTask(ctx context.Context, taskID string) error {
	// Cancel the context if the task is running.
	e.mu.Lock()
	cancelFn, running := e.cancelFuncs[taskID]
	e.mu.Unlock()
	if running {
		cancelFn()
	}

	// Mark as cancelled in DB.
	if err := e.store.CancelTask(ctx, taskID); err != nil {
		return fmt.Errorf("cancelling task: %w", err)
	}

	// Cascade cancel dependent tasks.
	dependents, err := e.store.CancelDependentTasks(ctx, taskID)
	if err != nil {
		log.Printf("[team] cascade cancel for %s error: %v", taskID[:min(8, len(taskID))], err)
	}

	// Emit events for cancelled dependents.
	task, _ := e.store.GetTask(ctx, taskID)
	teamID := ""
	if task != nil {
		teamID = task.TeamID
	}
	for _, dep := range dependents {
		log.Printf("[team] cascade cancelled dependent %s (%s)", dep.ID[:min(8, len(dep.ID))], dep.Title)
		if teamID != "" {
			e.emitEvent(agent.EventTeamTaskCancelled, teamID, dep.ID, nil)
		}
	}

	return nil
}

// CancelTeam cancels all in-flight tasks for a team.
func (e *TeamExecutor) CancelTeam(ctx context.Context, teamID string) error {
	// Get all in-progress tasks for the team.
	tasks, err := e.store.ListTasks(ctx, teamID, "in_progress")
	if err != nil {
		return fmt.Errorf("listing in-progress tasks: %w", err)
	}

	var errs []error
	for _, task := range tasks {
		if err := e.CancelTask(ctx, task.ID); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cancelled %d/%d tasks, %d errors", len(tasks)-len(errs), len(tasks), len(errs))
	}
	return nil
}

// LaunchIfReady checks if a task can be dispatched and launches execution if so.
// Used by the post-turn dispatch queue to launch tasks after the lead's turn.
func (e *TeamExecutor) LaunchIfReady(ctx context.Context, task store.TeamTask) {
	if task.AssignedTo == "" {
		return
	}

	// Re-read the task — cycle detection may have already failed it.
	current, err := e.store.GetTask(ctx, task.ID)
	if err != nil || current == nil || current.Status == "failed" || current.Status == "cancelled" || current.Status == "completed" {
		return
	}

	if current.BlockedBy != "" {
		// Check if all blockers are resolved.
		blockers := strings.Split(current.BlockedBy, ",")
		for _, bid := range blockers {
			bid = strings.TrimSpace(bid)
			if bid == "" {
				continue
			}
			bt, err := e.store.GetTask(ctx, bid)
			if err != nil || bt == nil || bt.Status != "completed" {
				return // Still blocked.
			}
		}
	}

	// Circuit breaker is inside launchExecute — no need to check here.
	e.launchExecute(ctx, current.TeamID, current.ID, current.AssignedTo)
}

// EmitTaskFailed emits a task failed event for external callers (e.g. blocker escalation).
func (e *TeamExecutor) EmitTaskFailed(ctx context.Context, teamID, taskID string) {
	e.emitEvent(agent.EventTeamTaskFailed, teamID, taskID, nil)

	// Push to inbox for lead notification.
	task, _ := e.store.GetTask(ctx, taskID)
	if task != nil {
		inbox := e.getInbox(teamID)
		inbox.Push(TaskCompletion{
			TaskID:  taskID,
			Subject: task.Title,
			Status:  "failed",
			Error:   task.ErrorMessage,
			BatchID: task.BatchID,
		})
	}
}

// SetNotifier attaches a progress notifier to the executor.
func (e *TeamExecutor) SetNotifier(n *TeamProgressNotifier) {
	e.notifier = n
}

// emitEvent sends a team event via the event handler and notifier.
func (e *TeamExecutor) emitEvent(eventType agent.EventType, teamID, taskID string, data any) {
	evt := agent.Event{
		Type:    eventType,
		AgentID: teamID,
		Text:    taskID,
	}
	// Fetch task snapshot for SSE broadcast so the web dashboard can update in real time.
	if task, err := e.store.GetTask(context.Background(), taskID); err == nil && task != nil {
		evt.TeamData = &agent.TeamEventData{
			TeamID: teamID,
			TaskID: taskID,
			Seq:    task.UpdatedAt.UnixMilli(),
			Task:   task,
		}
	}
	if e.eventHandler != nil {
		e.eventHandler(evt)
	}
	if e.notifier != nil {
		e.notifier.HandleEvent(evt)
	}
}

// Shutdown stops the recovery ticker, cancels all in-flight tasks, and waits
// for goroutines to finish within the given context.
func (e *TeamExecutor) Shutdown(ctx context.Context) error {
	// 1. Stop recovery ticker.
	e.mu.Lock()
	stopFn := e.recoveryStop
	e.mu.Unlock()
	if stopFn != nil {
		stopFn()
	}

	// 2. Cancel all in-flight tasks.
	e.mu.Lock()
	for taskID, cancel := range e.cancelFuncs {
		cancel()
		log.Printf("[team] shutdown: cancelled task %s", taskID[:min(8, len(taskID))])
	}
	e.mu.Unlock()

	// 3. Wait for goroutines to finish.
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown timed out: tasks still in-flight")
	}
}

// detectCycles uses Kahn's algorithm to find ALL circular dependencies
// among non-terminal tasks in a team. Returns IDs of tasks in cycles.
func (e *TeamExecutor) detectCycles(ctx context.Context, teamID string) []string {
	tasks, err := e.store.ListTasks(ctx, teamID, "")
	if err != nil {
		return nil
	}

	// Build adjacency: task → tasks it blocks (reverse of blocked_by).
	// Only consider non-terminal tasks.
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // blocker → tasks that depend on it

	for _, t := range tasks {
		if t.Status == "completed" || t.Status == "cancelled" || t.Status == "failed" {
			continue
		}
		if _, ok := inDegree[t.ID]; !ok {
			inDegree[t.ID] = 0
		}
		if t.BlockedBy == "" {
			continue
		}
		for _, dep := range strings.Split(t.BlockedBy, ",") {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			inDegree[t.ID]++
			dependents[dep] = append(dependents[dep], t.ID)
			if _, ok := inDegree[dep]; !ok {
				inDegree[dep] = 0
			}
		}
	}

	// Kahn's: start with nodes that have no incoming edges.
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	processed := 0
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		processed++
		for _, dep := range dependents[node] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	// Nodes not processed are in cycles.
	if processed == len(inDegree) {
		return nil
	}

	var cycleIDs []string
	for id, deg := range inDegree {
		if deg > 0 {
			cycleIDs = append(cycleIDs, id)
		}
	}
	return cycleIDs
}

// FailCycleTasks detects and fails any tasks involved in circular dependencies.
func (e *TeamExecutor) FailCycleTasks(ctx context.Context, teamID string) {
	cycleIDs := e.detectCycles(ctx, teamID)
	for _, id := range cycleIDs {
		e.store.UpdateTask(ctx, id, map[string]any{
			"status":        "failed",
			"error_message": "circular dependency detected",
		})
		e.emitEvent(agent.EventTeamTaskFailed, teamID, id, nil)
	}
}

// extractResponse extracts the assistant's text from run result messages.
func extractResponse(messages []canonical.Message) string {
	var parts []string
	for _, msg := range messages {
		if msg.Role != "assistant" {
			continue
		}
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, c.Text)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// isTransientError determines if an error is retryable.
func isTransientError(errMsg string) bool {
	transientPatterns := []string{
		"timeout", "context deadline exceeded",
		"overloaded", "rate limit", "429",
		"503", "502", "504",
		"temporary", "connection reset",
	}
	lower := strings.ToLower(errMsg)
	for _, p := range transientPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
