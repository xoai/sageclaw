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

// TaskResult holds a completed task's result for SYNTHESIZE injection.
type TaskResult struct {
	TaskID  string `json:"task_id"`
	Title   string `json:"title"`
	Status  string `json:"status"` // "completed", "failed", "cancelled"
	Result  string `json:"result,omitempty"`
	Error   string `json:"error,omitempty"`
}

// monitoredWorkflow tracks an active workflow being monitored.
type monitoredWorkflow struct {
	workflowID string
	teamID     string
	sessionID  string
	taskIDs    map[string]bool // All task IDs in this workflow.
	results    []TaskResult    // Collected results.
	mu         sync.Mutex
}

// WorkflowMonitor watches task events and advances workflows through MONITOR state.
type WorkflowMonitor struct {
	store        store.Store
	engine       *WorkflowEngine
	eventHandler agent.EventHandler
	wakeLead     WakeLeadFunc // Wakes the lead agent for synthesis.

	mu        sync.Mutex
	workflows map[string]*monitoredWorkflow // workflowID → tracked state
	taskIndex map[string]string             // taskID → workflowID (reverse index)

	// Timeout ticker.
	stopTicker chan struct{}
	tickerOnce sync.Once
}

// NewWorkflowMonitor creates a monitor that tracks task events for active workflows.
func NewWorkflowMonitor(s store.Store, engine *WorkflowEngine, onEvent agent.EventHandler) *WorkflowMonitor {
	return &WorkflowMonitor{
		store:        s,
		engine:       engine,
		eventHandler: onEvent,
		workflows:    make(map[string]*monitoredWorkflow),
		taskIndex:    make(map[string]string),
		stopTicker:   make(chan struct{}),
	}
}

// SetWakeLead sets the function used to wake the lead agent for synthesis.
func (m *WorkflowMonitor) SetWakeLead(fn WakeLeadFunc) {
	m.wakeLead = fn
}

// StartMonitoring begins tracking a workflow that has entered EXECUTE state.
// Called after CREATE completes and tasks have been dispatched.
func (m *WorkflowMonitor) StartMonitoring(workflowID, teamID, sessionID string, taskIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskSet := make(map[string]bool, len(taskIDs))
	for _, id := range taskIDs {
		taskSet[id] = true
		m.taskIndex[id] = workflowID
	}

	m.workflows[workflowID] = &monitoredWorkflow{
		workflowID: workflowID,
		teamID:     teamID,
		sessionID:  sessionID,
		taskIDs:    taskSet,
	}

	log.Printf("[workflow:%s] monitoring %d tasks", workflowID[:min(8, len(workflowID))], len(taskIDs))
}

// HandleEvent processes a task event and checks if the workflow should advance.
// This is called from the event handler chain — it must be fast and non-blocking.
func (m *WorkflowMonitor) HandleEvent(e agent.Event) {
	// Only process task terminal events.
	switch e.Type {
	case agent.EventTeamTaskCompleted, agent.EventTeamTaskFailed, agent.EventTeamTaskCancelled:
		// e.Text carries the taskID for team events.
		taskID := e.Text
		if taskID == "" {
			return
		}

		m.mu.Lock()
		wfID, ok := m.taskIndex[taskID]
		if !ok {
			m.mu.Unlock()
			return // Not a workflow task.
		}
		mw := m.workflows[wfID]
		m.mu.Unlock()

		if mw == nil {
			return
		}

		// Collect the result.
		m.collectResult(taskID, e.Type, mw)

		// Check if all tasks are terminal.
		if m.allTasksTerminal(mw) {
			m.advanceToSynthesize(mw)
		}
	}
}

// collectResult fetches the task from DB and records its result.
func (m *WorkflowMonitor) collectResult(taskID string, eventType agent.EventType, mw *monitoredWorkflow) {
	task, err := m.store.GetTask(context.Background(), taskID)
	if err != nil || task == nil {
		log.Printf("[workflow:%s] failed to fetch task %s: %v", mw.workflowID[:min(8, len(mw.workflowID))], taskID[:min(8, len(taskID))], err)
		return
	}

	result := TaskResult{
		TaskID: taskID,
		Title:  task.Title,
	}

	switch eventType {
	case agent.EventTeamTaskCompleted:
		result.Status = "completed"
		result.Result = task.Result
	case agent.EventTeamTaskFailed:
		result.Status = "failed"
		result.Error = task.ErrorMessage
	case agent.EventTeamTaskCancelled:
		result.Status = "cancelled"
	}

	mw.mu.Lock()
	// Dedup: skip if this task was already collected (timeout + event can both fire).
	for _, r := range mw.results {
		if r.TaskID == taskID {
			mw.mu.Unlock()
			return
		}
	}
	mw.results = append(mw.results, result)
	mw.mu.Unlock()

	log.Printf("[workflow:%s] task %s %s (%d/%d)",
		mw.workflowID[:min(8, len(mw.workflowID))],
		taskID[:min(8, len(taskID))],
		result.Status,
		len(mw.results), len(mw.taskIDs))
}

// allTasksTerminal checks if all workflow tasks have reported a terminal result.
func (m *WorkflowMonitor) allTasksTerminal(mw *monitoredWorkflow) bool {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	return len(mw.results) >= len(mw.taskIDs)
}

// advanceToSynthesize transitions the workflow from MONITOR to SYNTHESIZE.
func (m *WorkflowMonitor) advanceToSynthesize(mw *monitoredWorkflow) {
	ctx := context.Background()

	wf, err := m.store.GetWorkflow(ctx, mw.workflowID)
	if err != nil || wf == nil {
		log.Printf("[workflow:%s] failed to fetch workflow for synthesize: %v", mw.workflowID[:min(8, len(mw.workflowID))], err)
		return
	}

	// Only advance from execute or monitor states.
	if wf.State != StateExecute && wf.State != StateMonitor {
		return
	}

	// First transition to MONITOR if still in EXECUTE.
	if wf.State == StateExecute {
		if err := m.engine.transition(ctx, wf, StateMonitor, nil); err != nil {
			log.Printf("[workflow:%s] failed to transition to monitor: %v", mw.workflowID[:min(8, len(mw.workflowID))], err)
			return
		}
	}

	// Serialize results.
	mw.mu.Lock()
	resultsJSON, _ := json.Marshal(mw.results)
	mw.mu.Unlock()

	// Check if ALL tasks failed.
	allFailed := true
	mw.mu.Lock()
	for _, r := range mw.results {
		if r.Status == "completed" {
			allFailed = false
			break
		}
	}
	mw.mu.Unlock()

	if allFailed {
		// All failed — skip SYNTHESIZE, go to failed.
		if err := m.engine.transition(ctx, wf, StateFailed, map[string]any{
			"results_json": string(resultsJSON),
			"error":        "all tasks failed",
		}); err != nil {
			log.Printf("[workflow:%s] failed to transition to failed: %v", mw.workflowID[:min(8, len(mw.workflowID))], err)
		}
		m.cleanup(mw.workflowID)

		if m.eventHandler != nil {
			m.eventHandler(agent.Event{
				Type:    agent.EventWorkflowCompleted,
				AgentID: mw.teamID,
				Text:    mw.workflowID,
				TeamData: &agent.TeamEventData{TeamID: mw.teamID},
			})
		}
		return
	}

	// Advance to SYNTHESIZE with collected results.
	if err := m.engine.transition(ctx, wf, StateSynthesize, map[string]any{
		"results_json": string(resultsJSON),
	}); err != nil {
		log.Printf("[workflow:%s] failed to transition to synthesize: %v", mw.workflowID[:min(8, len(mw.workflowID))], err)
		return
	}

	if m.eventHandler != nil {
		m.eventHandler(agent.Event{
			Type:    agent.EventWorkflowStateChanged,
			AgentID: mw.teamID,
			Text:    fmt.Sprintf("%s:monitor→synthesize", mw.workflowID[:min(8, len(mw.workflowID))]),
			TeamData: &agent.TeamEventData{TeamID: mw.teamID},
		})
	}

	// Capture sessionID before cleanup removes the tracked workflow.
	originalSessionID := mw.sessionID

	// Wake the lead agent to synthesize results in the user's ORIGINAL session.
	if m.wakeLead != nil {
		team, err := m.store.GetTeam(ctx, mw.teamID)
		if err != nil || team == nil {
			log.Printf("[workflow:%s] failed to find team for wake: %v", mw.workflowID[:min(8, len(mw.workflowID))], err)
		} else {
			wakeMsg := FormatWorkflowWakeMessage(string(resultsJSON))
			m.wakeLead(ctx, team.LeadID, mw.teamID, wakeMsg, originalSessionID)
			log.Printf("[workflow:%s] waking lead %s for synthesis (session %s)", mw.workflowID[:min(8, len(mw.workflowID))], team.LeadID, originalSessionID[:min(8, len(originalSessionID))])
		}
	}

	// Cleanup happens in HandleLeadRunComplete or when the workflow transitions
	// to a terminal state (after synthesis completes).
}

// cleanup removes a workflow from the monitor's tracking maps.
func (m *WorkflowMonitor) cleanup(workflowID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if mw, ok := m.workflows[workflowID]; ok {
		for taskID := range mw.taskIDs {
			delete(m.taskIndex, taskID)
		}
		delete(m.workflows, workflowID)
	}
}

// StartTimeoutTicker starts a background ticker that checks for timed-out tasks.
func (m *WorkflowMonitor) StartTimeoutTicker(interval time.Duration, taskTimeout time.Duration) {
	m.tickerOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					m.checkTimeouts(taskTimeout)
					m.recoverStaleWorkflows()
				case <-m.stopTicker:
					return
				}
			}
		}()
	})
}

// StopTimeoutTicker stops the background timeout checker.
func (m *WorkflowMonitor) StopTimeoutTicker() {
	select {
	case m.stopTicker <- struct{}{}:
	default:
	}
}

// checkTimeouts finds tasks that have exceeded the timeout and auto-fails them.
func (m *WorkflowMonitor) checkTimeouts(taskTimeout time.Duration) {
	m.mu.Lock()
	// Copy workflow IDs to avoid holding the lock during DB queries.
	var wfIDs []string
	for id := range m.workflows {
		wfIDs = append(wfIDs, id)
	}
	m.mu.Unlock()

	cutoff := time.Now().Add(-taskTimeout)

	for _, wfID := range wfIDs {
		m.mu.Lock()
		mw, ok := m.workflows[wfID]
		m.mu.Unlock()
		if !ok {
			continue
		}

		mw.mu.Lock()
		reportedTasks := make(map[string]bool, len(mw.results))
		for _, r := range mw.results {
			reportedTasks[r.TaskID] = true
		}
		var unreportedIDs []string
		for taskID := range mw.taskIDs {
			if !reportedTasks[taskID] {
				unreportedIDs = append(unreportedIDs, taskID)
			}
		}
		mw.mu.Unlock()

		for _, taskID := range unreportedIDs {
			task, err := m.store.GetTask(context.Background(), taskID)
			if err != nil || task == nil {
				continue
			}
			// Check if task is stuck: either claimed but not completed, or unclaimed and past timeout.
			isStuck := false
			if task.ClaimedAt != nil && task.ClaimedAt.Before(cutoff) {
				isStuck = true
			} else if task.ClaimedAt == nil && task.CreatedAt.Before(cutoff) {
				isStuck = true // Never claimed — stuck in pending/blocked.
			}
			if isStuck {
				stuckSince := task.CreatedAt
				if task.ClaimedAt != nil {
					stuckSince = *task.ClaimedAt
				}
				log.Printf("[workflow:%s] task %s timed out (stuck %v ago)",
					wfID[:min(8, len(wfID))], taskID[:min(8, len(taskID))],
					time.Since(stuckSince).Round(time.Second))

				// Auto-fail the task.
				m.store.UpdateTask(context.Background(), taskID, map[string]any{
					"status":        "failed",
					"error_message": fmt.Sprintf("timed out after %v", taskTimeout),
				})
				// Collect as failed result.
				m.collectResult(taskID, agent.EventTeamTaskFailed, mw)

				// Check if all done now.
				if m.allTasksTerminal(mw) {
					m.advanceToSynthesize(mw)
				}
			}
		}
	}
}

// recoverStaleWorkflows checks for workflows stuck in early states and fails them.
func (m *WorkflowMonitor) recoverStaleWorkflows() {
	ctx := context.Background()

	// Use different timeouts per state:
	// - ANALYZE/PLAN/SYNTHESIZE: 5 min (LLM-dependent, should be fast)
	// - EXECUTE/MONITOR: 15 min (must exceed DefaultTaskTimeout of 10 min)
	staleEarly, _ := m.store.ListStaleWorkflows(ctx, 5*time.Minute)
	staleExec, _ := m.store.ListStaleWorkflows(ctx, 15*time.Minute)

	// Merge: use early threshold for ANALYZE/PLAN/SYNTHESIZE, exec threshold for EXECUTE/MONITOR.
	seen := make(map[string]bool)
	var stale []store.TeamWorkflow
	for _, wf := range staleEarly {
		if wf.State == StateAnalyze || wf.State == StatePlan || wf.State == StateCreate || wf.State == StateSynthesize || wf.State == StateDeliver {
			stale = append(stale, wf)
			seen[wf.ID] = true
		}
	}
	for _, wf := range staleExec {
		if !seen[wf.ID] && (wf.State == StateExecute || wf.State == StateMonitor) {
			stale = append(stale, wf)
		}
	}

	for _, wf := range stale {
		switch wf.State {
		case StateAnalyze, StatePlan, StateCreate:
			// Stuck in early LLM-dependent or transient states → mark failed.
			log.Printf("[workflow:%s] stale in %s (>5m) — marking failed", wf.ID[:min(8, len(wf.ID))], wf.State)
			if err := m.store.UpdateWorkflowState(ctx, wf.ID, StateFailed, wf.Version, map[string]any{
				"error": fmt.Sprintf("stale: stuck in %s for >5 minutes", wf.State),
			}); err != nil {
				log.Printf("[workflow:%s] stale recovery: %v", wf.ID[:min(8, len(wf.ID))], err)
			} else if m.eventHandler != nil {
				m.eventHandler(agent.Event{
					Type: agent.EventWorkflowCompleted, AgentID: wf.TeamID, Text: wf.ID,
					TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
				})
			}

		case StateDeliver:
			// Stuck between DELIVER and COMPLETE (process crash). Auto-complete.
			log.Printf("[workflow:%s] stale in deliver (>5m) — auto-completing", wf.ID[:min(8, len(wf.ID))])
			if err := m.store.UpdateWorkflowState(ctx, wf.ID, StateComplete, wf.Version, nil); err != nil {
				log.Printf("[workflow:%s] stale recovery: %v", wf.ID[:min(8, len(wf.ID))], err)
			} else if m.eventHandler != nil {
				m.eventHandler(agent.Event{
					Type: agent.EventWorkflowCompleted, AgentID: wf.TeamID, Text: wf.ID,
					TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
				})
			}

		case StateExecute, StateMonitor:
			// Stuck in execution/monitoring → auto-fail remaining tasks, advance to synthesize.
			log.Printf("[workflow:%s] stale in %s (>5m) — auto-failing remaining tasks", wf.ID[:min(8, len(wf.ID))], wf.State)
			if wf.TaskIDs == "" {
				if err := m.store.UpdateWorkflowState(ctx, wf.ID, StateFailed, wf.Version, map[string]any{
					"error": "stale: no tasks to recover",
				}); err != nil {
					log.Printf("[workflow:%s] stale recovery: %v", wf.ID[:min(8, len(wf.ID))], err)
				} else if m.eventHandler != nil {
					m.eventHandler(agent.Event{
						Type: agent.EventWorkflowCompleted, AgentID: wf.TeamID, Text: wf.ID,
						TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
					})
				}
				continue
			}
			// Try to re-register for monitoring and let the timeout system handle it.
			taskIDs := strings.Split(wf.TaskIDs, ",")
			m.mu.Lock()
			_, alreadyTracked := m.workflows[wf.ID]
			m.mu.Unlock()
			if !alreadyTracked {
				m.StartMonitoring(wf.ID, wf.TeamID, wf.SessionID, taskIDs)
				// Collect already-terminal tasks.
				m.mu.Lock()
				mw := m.workflows[wf.ID]
				m.mu.Unlock()
				if mw != nil {
					for _, taskID := range taskIDs {
						task, err := m.store.GetTask(ctx, taskID)
						if err != nil || task == nil {
							continue
						}
						switch task.Status {
						case "completed":
							m.collectResult(taskID, agent.EventTeamTaskCompleted, mw)
						case "failed":
							m.collectResult(taskID, agent.EventTeamTaskFailed, mw)
						case "cancelled":
							m.collectResult(taskID, agent.EventTeamTaskCancelled, mw)
						}
					}
					if m.allTasksTerminal(mw) {
						m.advanceToSynthesize(mw)
					}
				}
			}

		case StateSynthesize:
			// Stuck in synthesize → the lead may have crashed. Re-wake with retry limit.
			wakeCount := 0
			if strings.Contains(wf.Error, "synth_wake:") {
				fmt.Sscanf(wf.Error[strings.Index(wf.Error, "synth_wake:")+len("synth_wake:"):], "%d", &wakeCount)
			}
			wakeCount++

			if wakeCount > 3 {
				// Exceeded retry limit — give up.
				log.Printf("[workflow:%s] stale in synthesize — exceeded 3 re-wake attempts, failing", wf.ID[:min(8, len(wf.ID))])
				if err := m.store.UpdateWorkflowState(ctx, wf.ID, StateFailed, wf.Version, map[string]any{
					"error": fmt.Sprintf("synthesis failed after %d re-wake attempts", wakeCount-1),
				}); err != nil {
					log.Printf("[workflow:%s] stale recovery: %v", wf.ID[:min(8, len(wf.ID))], err)
				}
				if m.eventHandler != nil {
					m.eventHandler(agent.Event{
						Type:    agent.EventWorkflowCompleted,
						AgentID: wf.TeamID,
						Text:    wf.ID,
						TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
					})
				}
				continue
			}

			// Record attempt count and re-wake.
			newError := fmt.Sprintf("synth_wake:%d", wakeCount)
			if wf.Error != "" && !strings.Contains(wf.Error, "synth_wake:") {
				newError = wf.Error + "; " + newError
			}
			_ = m.store.UpdateWorkflowState(ctx, wf.ID, StateSynthesize, wf.Version, map[string]any{
				"error": newError,
			})

			log.Printf("[workflow:%s] stale in synthesize (>5m) — re-waking lead (attempt %d/3)", wf.ID[:min(8, len(wf.ID))], wakeCount)
			if m.wakeLead != nil {
				team, err := m.store.GetTeam(ctx, wf.TeamID)
				if err != nil || team == nil {
					continue
				}
				wakeMsg := FormatWorkflowWakeMessage(wf.ResultsJSON)
				m.wakeLead(ctx, team.LeadID, wf.TeamID, wakeMsg, wf.SessionID)
			}
		}
	}
}

// CancelWorkflow cancels a workflow and all its in-flight tasks.
func (m *WorkflowMonitor) CancelWorkflow(ctx context.Context, workflowID string) error {
	wf, err := m.store.GetWorkflow(ctx, workflowID)
	if err != nil || wf == nil {
		return fmt.Errorf("workflow not found: %s", workflowID)
	}
	if IsTerminal(wf.State) {
		return fmt.Errorf("workflow %s is already in terminal state %s", workflowID[:min(8, len(workflowID))], wf.State)
	}

	// Cancel all in-flight tasks if we have task IDs.
	if wf.TaskIDs != "" {
		taskIDs := strings.Split(wf.TaskIDs, ",")
		for _, taskID := range taskIDs {
			task, err := m.store.GetTask(ctx, taskID)
			if err != nil || task == nil {
				continue
			}
			if task.Status == "pending" || task.Status == "in_progress" || task.Status == "blocked" {
				_ = m.store.CancelTask(ctx, taskID)
			}
		}
	}

	// Cancel the workflow itself.
	if err := m.store.CancelWorkflow(ctx, workflowID); err != nil {
		return err
	}

	m.cleanup(workflowID)

	log.Printf("[workflow:%s] cancelled", workflowID[:min(8, len(workflowID))])

	if m.eventHandler != nil {
		m.eventHandler(agent.Event{
			Type:    agent.EventWorkflowCompleted,
			AgentID: wf.TeamID,
			Text:    workflowID,
			TeamData: &agent.TeamEventData{TeamID: wf.TeamID},
		})
	}

	return nil
}

// RecoverActiveWorkflows re-registers any non-terminal workflows for monitoring.
// Called at startup to resume monitoring after a process restart.
func (m *WorkflowMonitor) RecoverActiveWorkflows(ctx context.Context) {
	// Find all non-terminal workflows.
	workflows, err := m.store.ListNonTerminalWorkflows(ctx)
	if err != nil {
		log.Printf("[workflow-monitor] recovery error: %v", err)
		return
	}

	for _, wf := range workflows {
		if wf.State != StateExecute && wf.State != StateMonitor {
			continue
		}
		if wf.TaskIDs == "" {
			continue
		}
		taskIDs := strings.Split(wf.TaskIDs, ",")
		m.StartMonitoring(wf.ID, wf.TeamID, wf.SessionID, taskIDs)

		// Get the tracked workflow under lock.
		m.mu.Lock()
		mw := m.workflows[wf.ID]
		m.mu.Unlock()
		if mw == nil {
			continue
		}

		// Check current task statuses — some may have completed while we were down.
		for _, taskID := range taskIDs {
			task, err := m.store.GetTask(ctx, taskID)
			if err != nil || task == nil {
				continue
			}
			switch task.Status {
			case "completed":
				m.collectResult(taskID, agent.EventTeamTaskCompleted, mw)
			case "failed":
				m.collectResult(taskID, agent.EventTeamTaskFailed, mw)
			case "cancelled":
				m.collectResult(taskID, agent.EventTeamTaskCancelled, mw)
			}
		}

		// Check if all tasks are already terminal.
		if m.allTasksTerminal(mw) {
			m.advanceToSynthesize(mw)
		}
	}
}
