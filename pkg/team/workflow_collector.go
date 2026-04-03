package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/store"
)

// WorkflowEventCollector provides persist-first workflow event collection.
// It stores workflow activity as workflow_activity content blocks in the user's
// session, making the tool timeline reconstructable from stored messages.
// SSE provides real-time overlay; DB is the source of truth.
type WorkflowEventCollector struct {
	store       store.Store
	onSSE       agent.EventHandler     // Real-time SSE broadcast.
	tracker     *toolstatus.Tracker    // Channel adapter display (Telegram, Discord).

	mu          sync.RWMutex
	workflows   map[string]*activeWorkflow  // workflowID → state
	agentIndex  map[string]string           // memberAgentID → workflowID

	// Batching: accumulate events per workflow, flush on timer/capacity/state-change.
	batchMu     sync.Mutex
	batches     map[string]*eventBatch      // workflowID → pending batch
}

// activeWorkflow tracks a registered workflow for event matching.
type activeWorkflow struct {
	WorkflowID    string
	UserSessionID string
	TeamID        string
	LeadAgentID   string
	Members       map[string]string // agentID → displayName
}

// eventBatch accumulates workflow_activity content blocks before flushing to DB.
type eventBatch struct {
	userSessionID string
	activities    []canonical.Content
	timer         *time.Timer
	failures      int // Consecutive flush failure count.
}

const (
	batchFlushInterval = 1500 * time.Millisecond
	batchMaxCapacity   = 10
	batchMaxRetries    = 3
)

// NewWorkflowEventCollector creates a collector.
func NewWorkflowEventCollector(s store.Store, onSSE agent.EventHandler, tracker *toolstatus.Tracker) *WorkflowEventCollector {
	return &WorkflowEventCollector{
		store:      s,
		onSSE:      onSSE,
		tracker:    tracker,
		workflows:  make(map[string]*activeWorkflow),
		agentIndex: make(map[string]string),
		batches:    make(map[string]*eventBatch),
	}
}

// RegisterWorkflow eagerly registers a workflow with all member agent IDs.
// Called from executeCreate with data available at creation time — no DB lookups.
func (c *WorkflowEventCollector) RegisterWorkflow(wfID, userSessionID, leadAgentID, teamID string, members map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.workflows[wfID] = &activeWorkflow{
		WorkflowID:    wfID,
		UserSessionID: userSessionID,
		TeamID:        teamID,
		LeadAgentID:   leadAgentID,
		Members:       members,
	}

	// Index all member agents for O(1) matching.
	// Invariant: one agent maps to at most one active workflow.
	// If a collision occurs (shared agent across teams), the newer workflow wins.
	for agentID := range members {
		if existing, ok := c.agentIndex[agentID]; ok && existing != wfID {
			log.Printf("[collector] agent %s already indexed to workflow %s, overwriting with %s",
				agentID, existing[:min(8, len(existing))], wfID[:min(8, len(wfID))])
		}
		c.agentIndex[agentID] = wfID
	}

	// Ensure tracker session exists for channel adapter display.
	if c.tracker != nil {
		c.tracker.EnsureSession(userSessionID)
	}

	log.Printf("[collector:%s] registered workflow (session %s, %d members)",
		wfID[:min(8, len(wfID))], userSessionID[:min(8, len(userSessionID))], len(members))
}

// UnregisterWorkflow flushes pending batch and removes all state.
// Called from HandleLeadRunComplete after workflow reaches COMPLETE.
func (c *WorkflowEventCollector) UnregisterWorkflow(wfID string) {
	// Flush any pending batch first.
	c.flushBatch(wfID)

	c.mu.Lock()
	defer c.mu.Unlock()

	wf, ok := c.workflows[wfID]
	if !ok {
		return
	}

	// Remove agent index entries.
	for agentID := range wf.Members {
		delete(c.agentIndex, agentID)
	}
	delete(c.workflows, wfID)

	// Clean up batch.
	c.batchMu.Lock()
	if b, ok := c.batches[wfID]; ok {
		if b.timer != nil {
			b.timer.Stop()
		}
		delete(c.batches, wfID)
	}
	c.batchMu.Unlock()

	log.Printf("[collector:%s] unregistered workflow", wfID[:min(8, len(wfID))])
}

// HandleEvent is the single entry point for ALL workflow-relevant events.
// Called from BOTH the loopPool handler AND the executor handler.
func (c *WorkflowEventCollector) HandleEvent(e agent.Event) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[collector] panic in HandleEvent: %v", r)
		}
	}()

	switch e.Type {
	case agent.EventToolCall:
		if e.ToolCall == nil || e.AgentID == "" {
			return
		}
		// Skip internal workflow tools.
		if strings.HasPrefix(e.ToolCall.Name, "_workflow_") {
			return
		}
		c.handleMemberToolCall(e)

	case agent.EventToolResult:
		if e.ToolResult == nil || e.AgentID == "" {
			return
		}
		c.handleMemberToolResult(e)

	case agent.EventTeamTaskClaimed:
		c.handleTaskStarted(e)

	case agent.EventTeamTaskCompleted:
		c.handleTaskCompleted(e, false)

	case agent.EventTeamTaskFailed:
		c.handleTaskCompleted(e, true)

	case agent.EventConsentNeeded:
		c.handleConsentNeeded(e)
	}
}

// --- Tool call/result forwarding ---

func (c *WorkflowEventCollector) handleMemberToolCall(e agent.Event) {
	c.mu.RLock()
	wfID, ok := c.agentIndex[e.AgentID]
	if !ok {
		c.mu.RUnlock()
		return // Not a workflow member.
	}
	wf := c.workflows[wfID]
	if wf == nil {
		c.mu.RUnlock()
		return
	}
	displayName := wf.Members[e.AgentID]
	userSessionID := wf.UserSessionID
	c.mu.RUnlock()

	if displayName == "" {
		displayName = e.AgentID
	}

	detail := extractToolDetail(e.ToolCall.Name, e.ToolCall.Input)

	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": "tool_call",
			"workflow_id":   wfID,
			"agent_name":    displayName,
			"tool_name":     e.ToolCall.Name,
			"tool_call_id":  e.ToolCall.ID,
			"detail":        detail,
			"status":        "running",
		},
	}

	c.addToBatch(wfID, activity)

	// Real-time SSE + tracker.
	if c.onSSE != nil {
		c.onSSE(agent.Event{
			Type:      agent.EventToolCall,
			SessionID: userSessionID,
			AgentID:   e.AgentID,
			ToolCall: &canonical.ToolCall{
				ID:    e.ToolCall.ID,
				Name:  fmt.Sprintf("member:%s:%s", displayName, e.ToolCall.Name),
				Input: e.ToolCall.Input,
			},
		})
	}
	if c.tracker != nil {
		c.tracker.OnToolCall(userSessionID, &canonical.ToolCall{
			ID:    e.ToolCall.ID,
			Name:  fmt.Sprintf("member:%s:%s", displayName, e.ToolCall.Name),
			Input: e.ToolCall.Input,
		})
	}
}

func (c *WorkflowEventCollector) handleMemberToolResult(e agent.Event) {
	c.mu.RLock()
	wfID, ok := c.agentIndex[e.AgentID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	wf := c.workflows[wfID]
	if wf == nil {
		c.mu.RUnlock()
		return
	}
	userSessionID := wf.UserSessionID
	c.mu.RUnlock()

	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": "tool_result",
			"workflow_id":   wfID,
			"tool_call_id":  e.ToolResult.ToolCallID,
			"status":        "done",
		},
	}

	c.addToBatch(wfID, activity)

	// Real-time SSE + tracker.
	if c.onSSE != nil {
		c.onSSE(agent.Event{
			Type:       agent.EventToolResult,
			SessionID:  userSessionID,
			ToolResult: e.ToolResult,
		})
	}
	if c.tracker != nil {
		c.tracker.OnToolResult(userSessionID, e.ToolResult)
	}
}

// --- Task lifecycle ---

func (c *WorkflowEventCollector) handleTaskStarted(e agent.Event) {
	taskID := e.Text
	if taskID == "" || e.TeamData == nil {
		return
	}

	teamID := e.TeamData.TeamID
	c.mu.RLock()
	var wf *activeWorkflow
	for _, w := range c.workflows {
		if w.TeamID == teamID {
			wf = w
			break
		}
	}
	c.mu.RUnlock()
	if wf == nil {
		return
	}

	var title, assignee string
	if e.TeamData.Task != nil {
		if task, ok := e.TeamData.Task.(*store.TeamTask); ok {
			title = task.Title
			assignee = task.AssignedTo
		}
	}

	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": "task_started",
			"workflow_id":   wf.WorkflowID,
			"task_title":    title,
			"agent_name":    assignee,
			"tool_call_id":  "task_" + taskID[:min(8, len(taskID))],
			"status":        "running",
		},
	}

	c.addToBatch(wf.WorkflowID, activity)

	// Immediate SSE for task events.
	if c.onSSE != nil {
		syntheticTC := &canonical.ToolCall{
			ID:   "task_" + taskID[:min(8, len(taskID))],
			Name: "_wf_task_started",
		}
		if title != "" {
			input, _ := json.Marshal(map[string]string{"title": title, "assignee": assignee})
			syntheticTC.Input = input
		}
		c.onSSE(agent.Event{
			Type:      agent.EventToolCall,
			SessionID: wf.UserSessionID,
			ToolCall:  syntheticTC,
		})
	}
}

func (c *WorkflowEventCollector) handleTaskCompleted(e agent.Event, isError bool) {
	taskID := e.Text
	if taskID == "" || e.TeamData == nil {
		return
	}

	teamID := e.TeamData.TeamID
	c.mu.RLock()
	var wf *activeWorkflow
	for _, w := range c.workflows {
		if w.TeamID == teamID {
			wf = w
			break
		}
	}
	c.mu.RUnlock()
	if wf == nil {
		return
	}

	status := "done"
	actType := "task_completed"
	if isError {
		actType = "task_failed"
	}

	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": actType,
			"workflow_id":   wf.WorkflowID,
			"tool_call_id":  "task_" + taskID[:min(8, len(taskID))],
			"status":        status,
		},
	}

	// State change → flush immediately.
	c.addToBatch(wf.WorkflowID, activity)
	c.flushBatch(wf.WorkflowID)

	// SSE result for the task.
	if c.onSSE != nil {
		c.onSSE(agent.Event{
			Type:      agent.EventToolResult,
			SessionID: wf.UserSessionID,
			ToolResult: &canonical.ToolResult{
				ToolCallID: "task_" + taskID[:min(8, len(taskID))],
				Content:    "Task " + actType,
				IsError:    isError,
			},
		})
	}
}

// --- Consent forwarding ---

func (c *WorkflowEventCollector) handleConsentNeeded(e agent.Event) {
	if e.SessionID == "" || e.Consent == nil {
		return
	}

	c.mu.RLock()
	wfID, ok := c.agentIndex[e.AgentID]
	if !ok {
		c.mu.RUnlock()
		return
	}
	wf := c.workflows[wfID]
	if wf == nil {
		c.mu.RUnlock()
		return
	}
	displayName := wf.Members[e.AgentID]
	if displayName == "" {
		displayName = e.AgentID
	}
	userSessionID := wf.UserSessionID
	c.mu.RUnlock()

	// Persist consent activity for timeline reconstruction.
	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": "consent_needed",
			"workflow_id":   wfID,
			"agent_name":    displayName,
			"tool_name":     e.Consent.ToolName,
			"detail":        fmt.Sprintf("%s wants to run: %s", displayName, e.Consent.ToolName),
			"status":        "running",
		},
	}
	c.addToBatch(wfID, activity)

	// Re-emit consent on user's session so the web chat shows the popup.
	if c.onSSE != nil {
		forwarded := e
		forwarded.SessionID = userSessionID
		if forwarded.Consent != nil {
			forwarded.Consent.Explanation = fmt.Sprintf("%s wants to run: %s", displayName, e.Consent.ToolName)
		}
		c.onSSE(forwarded)
	}
}

// --- Delegating event ---

// EmitDelegating stores and broadcasts the initial delegation event.
func (c *WorkflowEventCollector) EmitDelegating(wfID, userSessionID string, taskCount int) {
	activity := canonical.Content{
		Type: "workflow_activity",
		Meta: map[string]string{
			"activity_type": "delegating",
			"workflow_id":   wfID,
			"task_count":    fmt.Sprintf("%d", taskCount),
			"status":        "done",
		},
	}

	c.addToBatch(wfID, activity)
	c.flushBatch(wfID) // Flush immediately — delegation is a milestone.

	// SSE.
	if c.onSSE != nil {
		input, _ := json.Marshal(map[string]string{"count": fmt.Sprintf("%d", taskCount)})
		tc := &canonical.ToolCall{ID: "wf_deleg_" + wfID[:min(8, len(wfID))], Name: "_wf_delegating", Input: input}
		c.onSSE(agent.Event{Type: agent.EventToolCall, SessionID: userSessionID, ToolCall: tc})
		tr := &canonical.ToolResult{ToolCallID: tc.ID, Content: fmt.Sprintf("%d tasks created", taskCount)}
		c.onSSE(agent.Event{Type: agent.EventToolResult, SessionID: userSessionID, ToolResult: tr})
	}
	if c.tracker != nil {
		c.tracker.EnsureSession(userSessionID)
	}
}

// --- Batching ---

func (c *WorkflowEventCollector) addToBatch(wfID string, activity canonical.Content) {
	c.batchMu.Lock()
	defer c.batchMu.Unlock()

	b, ok := c.batches[wfID]
	if !ok {
		c.mu.RLock()
		wf := c.workflows[wfID]
		sessID := ""
		if wf != nil {
			sessID = wf.UserSessionID
		}
		c.mu.RUnlock()

		b = &eventBatch{userSessionID: sessID}
		c.batches[wfID] = b
	}

	b.activities = append(b.activities, activity)

	// Start timer on first event in batch.
	if len(b.activities) == 1 && b.timer == nil {
		b.timer = time.AfterFunc(batchFlushInterval, func() {
			c.flushBatch(wfID)
		})
	}

	// Flush on capacity — stop timer to prevent double-flush race.
	if len(b.activities) >= batchMaxCapacity {
		if b.timer != nil {
			b.timer.Stop()
			b.timer = nil
		}
		go c.flushBatch(wfID)
	}
}

func (c *WorkflowEventCollector) flushBatch(wfID string) {
	c.batchMu.Lock()
	b, ok := c.batches[wfID]
	if !ok || len(b.activities) == 0 {
		c.batchMu.Unlock()
		return
	}

	// Take the activities and reset the batch.
	activities := b.activities
	b.activities = nil
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	sessID := b.userSessionID
	c.batchMu.Unlock()

	if sessID == "" {
		return
	}

	// Write to DB.
	msg := canonical.Message{
		Role:    "assistant",
		Content: activities,
	}
	err := c.store.AppendMessages(context.Background(), sessID, []canonical.Message{msg})
	if err != nil {
		c.batchMu.Lock()
		b.failures++
		if b.failures >= batchMaxRetries {
			log.Printf("[collector:%s] batch flush failed %d times, dropping %d activities",
				wfID[:min(8, len(wfID))], b.failures, len(activities))
			b.failures = 0
		} else {
			// Keep activities for retry on next flush.
			b.activities = append(activities, b.activities...)
			log.Printf("[collector:%s] batch flush failed (attempt %d), will retry: %v",
				wfID[:min(8, len(wfID))], b.failures, err)
		}
		c.batchMu.Unlock()
		return
	}

	// Success — reset failure count.
	c.batchMu.Lock()
	if b, ok := c.batches[wfID]; ok {
		b.failures = 0
	}
	c.batchMu.Unlock()
}

// --- Detail extraction ---

// extractToolDetail extracts a meaningful detail string from tool input.
func extractToolDetail(toolName string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return ""
	}

	switch toolName {
	case "web_search":
		return stringField(args, "query", 60)
	case "web_fetch":
		if u, ok := args["url"].(string); ok {
			if parsed, err := url.Parse(u); err == nil {
				return parsed.Host
			}
			return truncate(u, 40)
		}
	case "read_file", "write_file", "edit":
		if p, ok := args["path"].(string); ok {
			return filepath.Base(p)
		}
	case "execute_command":
		return stringField(args, "command", 50)
	case "memory_search":
		return stringField(args, "query", 60)
	case "browser":
		if u, ok := args["url"].(string); ok {
			if parsed, err := url.Parse(u); err == nil {
				return parsed.Host
			}
		}
	case "team_tasks":
		action := stringField(args, "action", 20)
		subject := stringField(args, "subject", 30)
		if subject != "" {
			return action + ": " + subject
		}
		return action
	}
	return ""
}

func stringField(args map[string]any, key string, maxLen int) string {
	if v, ok := args[key].(string); ok {
		return truncate(v, maxLen)
	}
	return ""
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
