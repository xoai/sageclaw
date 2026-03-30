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

// TeamExecutor is the orchestration engine that dispatches tasks to member agents.
type TeamExecutor struct {
	store        store.Store
	loopFactory  LoopFactory
	eventHandler agent.EventHandler
	notifier     *TeamProgressNotifier

	mu       sync.Mutex
	inboxes  map[string]*TeamInbox    // teamID → completion inbox
	limiters map[string]chan struct{}  // teamID → concurrency semaphore
	wg       sync.WaitGroup           // tracks in-flight execute goroutines
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
		_ = role // lead or member — both valid assignees
	}

	// Cycle detection for blocked_by.
	if task.BlockedBy != "" {
		if err := e.detectCycle(ctx, task.TeamID, task.BlockedBy); err != nil {
			return "", err
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
		e.launchExecute(task.TeamID, taskID, task.AssignedTo)
	}

	return taskID, nil
}

// launchExecute starts a goroutine for task execution.
func (e *TeamExecutor) launchExecute(teamID, taskID, agentID string) {
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

	// Acquire concurrency slot.
	if err := e.acquireConcurrency(ctx, teamID); err != nil {
		log.Printf("[team] task %s: failed to acquire concurrency: %v", taskID[:8], err)
		e.failTask(ctx, taskID, teamID, "concurrency_timeout", err.Error())
		return
	}
	defer e.releaseConcurrency(teamID)

	// Claim the task (pending → in_progress).
	if err := e.store.ClaimTask(ctx, taskID, agentID); err != nil {
		log.Printf("[team] task %s: failed to claim: %v", taskID[:8], err)
		return
	}
	e.emitEvent(agent.EventTeamTaskClaimed, teamID, taskID, nil)

	// Load task details.
	task, err := e.store.GetTask(ctx, taskID)
	if err != nil || task == nil {
		log.Printf("[team] task %s: failed to load: %v", taskID[:8], err)
		e.failTask(ctx, taskID, teamID, "load_error", "failed to load task details")
		return
	}

	// Build task prompt.
	prompt := e.buildTaskPrompt(ctx, task)

	// Create fresh session for this task.
	session, err := e.store.CreateSessionWithKind(ctx, "internal", taskID[:8], agentID, "team_task")
	if err != nil {
		log.Printf("[team] task %s: failed to create session: %v", taskID[:8], err)
		e.failTask(ctx, taskID, teamID, "session_error", err.Error())
		return
	}

	// Link session to task.
	e.store.UpdateTask(ctx, taskID, map[string]any{"session_id": session.ID})

	// Create ephemeral Loop for this task.
	loop := e.loopFactory.NewTaskLoop(agentID)
	if loop == nil {
		log.Printf("[team] task %s: no loop factory output for agent %s", taskID[:8], agentID)
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
		log.Printf("[team] task %s: failed to complete: %v", taskID[:8], err)
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
				e.launchExecute(teamID, t.ID, t.AssignedTo)
			}
		}
	}

	log.Printf("[team] task %s (%s) completed by %s", taskID[:8], task.Title, agentID)
}

// handleFailure classifies errors and either retries or fails permanently.
func (e *TeamExecutor) handleFailure(ctx context.Context, task *store.TeamTask, teamID string, err error) {
	errMsg := err.Error()
	isTransient := isTransientError(errMsg)

	if isTransient && task.RetryCount < task.MaxRetries {
		log.Printf("[team] task %s: transient failure, retrying (%d/%d): %v",
			task.ID[:8], task.RetryCount+1, task.MaxRetries, err)
		if retryErr := e.store.RetryTask(ctx, task.ID); retryErr == nil {
			e.launchExecute(teamID, task.ID, task.AssignedTo)
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

// Shutdown waits for in-flight tasks to complete within the given context.
func (e *TeamExecutor) Shutdown(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("shutdown timed out: %d tasks still in-flight", e.inFlightCount())
	}
}

// inFlightCount is an approximation (for logging only).
func (e *TeamExecutor) inFlightCount() int {
	// WaitGroup doesn't expose its counter, so this is best-effort.
	return 0
}

// detectCycle checks for circular dependencies using DFS.
func (e *TeamExecutor) detectCycle(ctx context.Context, teamID, blockedBy string) error {
	blockers := strings.Split(blockedBy, ",")
	visited := make(map[string]bool)

	var dfs func(taskID string) error
	dfs = func(taskID string) error {
		taskID = strings.TrimSpace(taskID)
		if taskID == "" {
			return nil
		}
		if visited[taskID] {
			return fmt.Errorf("circular dependency detected involving task %s", taskID[:min(8, len(taskID))])
		}
		visited[taskID] = true

		task, err := e.store.GetTask(ctx, taskID)
		if err != nil || task == nil {
			return nil // Task doesn't exist yet, no cycle possible.
		}
		if task.BlockedBy == "" {
			return nil
		}
		for _, dep := range strings.Split(task.BlockedBy, ",") {
			if err := dfs(dep); err != nil {
				return err
			}
		}
		return nil
	}

	for _, b := range blockers {
		if err := dfs(b); err != nil {
			return err
		}
	}
	return nil
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
