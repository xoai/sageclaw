package team

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/store"
)

// RelayEntry maps a member session to the user's workflow session.
type RelayEntry struct {
	WorkflowID        string
	UserSessionID     string
	TeamID            string
	MemberAgentID     string
	MemberDisplayName string
}

// relayState tracks forwarding state per workflow.
type relayState struct {
	Forwarding    bool   // true = ACTIVE, false = PAUSED
	UserSessionID string
	LeadAgentID   string
	TeamID        string
}

// WorkflowRelay forwards team member activity to the user's chat session
// via the toolstatus.Tracker. Sits between the event stream and the tracker,
// translating workflow events into tracker-compatible calls.
type WorkflowRelay struct {
	toolTracker *toolstatus.Tracker
	store       store.Store
	onEvent     agent.EventHandler // Emits synthetic events to SSE broadcast.

	mu           sync.RWMutex
	sessionIndex map[string]*RelayEntry // memberSessionID → relay info
	workflows    map[string]*relayState // workflowID → forwarding state

	// Synthetic tool call IDs for tracking (workflowID:taskID → callID).
	taskCallIDs map[string]string
}

// NewWorkflowRelay creates a relay that forwards workflow activity to user sessions.
func NewWorkflowRelay(tracker *toolstatus.Tracker, s store.Store, onEvent agent.EventHandler) *WorkflowRelay {
	return &WorkflowRelay{
		toolTracker:  tracker,
		onEvent:      onEvent,
		store:        s,
		sessionIndex: make(map[string]*RelayEntry),
		workflows:    make(map[string]*relayState),
		taskCallIDs:  make(map[string]string),
	}
}

// RegisterWorkflow sets up forwarding for a workflow. Called when workflow enters EXECUTE.
func (r *WorkflowRelay) RegisterWorkflow(wfID, userSessionID, leadAgentID, teamID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.workflows[wfID] = &relayState{
		Forwarding:    true, // Explicitly ACTIVE — Go zero-value is false.
		UserSessionID: userSessionID,
		LeadAgentID:   leadAgentID,
		TeamID:        teamID,
	}

	// Ensure session state exists in the tracker so tool calls aren't dropped.
	r.toolTracker.EnsureSession(userSessionID)

	log.Printf("[relay:%s] registered workflow (user session %s)", wfID[:min(8, len(wfID))], userSessionID[:min(8, len(userSessionID))])
}

// RegisterMemberSession maps a member's session to a workflow for forwarding.
func (r *WorkflowRelay) RegisterMemberSession(memberSessionID string, entry RelayEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionIndex[memberSessionID] = &entry
}

// UnregisterWorkflow removes all relay state for a completed/cancelled workflow.
func (r *WorkflowRelay) UnregisterWorkflow(wfID string) {
	r.mu.Lock()
	// Capture user session ID before deleting.
	var userSessionID string
	if ws, ok := r.workflows[wfID]; ok {
		userSessionID = ws.UserSessionID
	}
	delete(r.workflows, wfID)

	// Remove all member session entries for this workflow.
	for sessID, entry := range r.sessionIndex {
		if entry.WorkflowID == wfID {
			delete(r.sessionIndex, sessID)
		}
	}

	// Remove task call IDs.
	prefix := wfID + ":"
	for key := range r.taskCallIDs {
		if strings.HasPrefix(key, prefix) {
			delete(r.taskCallIDs, key)
		}
	}

	// Check if user session is still referenced by other workflows (under same lock).
	shouldClear := false
	if userSessionID != "" {
		shouldClear = true
		for _, ws := range r.workflows {
			if ws.UserSessionID == userSessionID {
				shouldClear = false
				break
			}
		}
	}
	r.mu.Unlock()

	// Clear tracker session state outside the lock (tracker has its own lock).
	if shouldClear {
		r.toolTracker.Clear(userSessionID)
	}
}

// PauseForwarding pauses tool-level forwarding (task events still pass).
func (r *WorkflowRelay) PauseForwarding(wfID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ws, ok := r.workflows[wfID]; ok {
		ws.Forwarding = false
	}
}

// ResumeForwarding resumes tool-level forwarding.
func (r *WorkflowRelay) ResumeForwarding(wfID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ws, ok := r.workflows[wfID]; ok {
		ws.Forwarding = true
	}
}

// HandleMemberEvent processes events from the executor's event handler chain.
// Called for ALL team member events — filters to workflow-relevant ones.
// Must be safe to call from any goroutine; uses recover() for hot-path safety.
func (r *WorkflowRelay) HandleMemberEvent(e agent.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[relay] panic in HandleMemberEvent: %v", rec)
		}
	}()

	switch e.Type {
	case agent.EventToolCall:
		if e.ToolCall == nil || e.SessionID == "" {
			return
		}
		// Filter internal workflow tools.
		if strings.HasPrefix(e.ToolCall.Name, "_workflow_") {
			return
		}
		r.forwardToolCall(e.SessionID, e.ToolCall)

	case agent.EventToolResult:
		if e.ToolResult == nil || e.SessionID == "" {
			return
		}
		r.forwardToolResult(e.SessionID, e.ToolResult)

	case agent.EventTeamTaskClaimed:
		r.emitTaskStarted(e)

	case agent.EventTeamTaskCompleted:
		r.emitTaskCompleted(e)

	case agent.EventTeamTaskFailed:
		r.emitTaskFailed(e)
	}
}

// HandleLeadEvent processes events from the loopPool's event handler chain.
// Detects lead RunStarted/RunCompleted for pause/resume.
func (r *WorkflowRelay) HandleLeadEvent(e agent.Event) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[relay] panic in HandleLeadEvent: %v", rec)
		}
	}()

	if e.SessionID == "" {
		return
	}

	switch e.Type {
	case agent.EventRunStarted:
		r.mu.RLock()
		for wfID, ws := range r.workflows {
			if ws.UserSessionID == e.SessionID && ws.LeadAgentID == e.AgentID {
				r.mu.RUnlock()
				r.PauseForwarding(wfID)
				return
			}
		}
		r.mu.RUnlock()

	case agent.EventRunCompleted:
		r.mu.RLock()
		for wfID, ws := range r.workflows {
			if ws.UserSessionID == e.SessionID && ws.LeadAgentID == e.AgentID {
				r.mu.RUnlock()
				r.ResumeForwarding(wfID)
				return
			}
		}
		r.mu.RUnlock()
	}
}

// forwardToolCall forwards a member's tool call to the user's session.
func (r *WorkflowRelay) forwardToolCall(memberSessionID string, tc *canonical.ToolCall) {
	r.mu.RLock()
	entry := r.sessionIndex[memberSessionID]
	if entry == nil {
		r.mu.RUnlock()
		// DB fallback: try to resolve member session → workflow.
		entry = r.resolveSessionFromDB(memberSessionID)
		if entry == nil {
			return
		}
		r.mu.RLock()
	}
	ws := r.workflows[entry.WorkflowID]
	if ws == nil || !ws.Forwarding {
		r.mu.RUnlock()
		return
	}
	userSessionID := ws.UserSessionID
	displayName := entry.MemberDisplayName
	r.mu.RUnlock()

	// Create synthetic tool call with member prefix.
	syntheticTC := &canonical.ToolCall{
		ID:    tc.ID,
		Name:  fmt.Sprintf("member:%s:%s", displayName, tc.Name),
		Input: tc.Input,
	}
	r.toolTracker.OnToolCall(userSessionID, syntheticTC)

	// Emit to SSE so web chat sees the tool call.
	if r.onEvent != nil {
		r.onEvent(agent.Event{
			Type:      agent.EventToolCall,
			SessionID: userSessionID,
			ToolCall:  syntheticTC,
		})
	}
}

// forwardToolResult forwards a member's tool result to the user's session.
func (r *WorkflowRelay) forwardToolResult(memberSessionID string, tr *canonical.ToolResult) {
	r.mu.RLock()
	entry := r.sessionIndex[memberSessionID]
	if entry == nil {
		r.mu.RUnlock()
		// DB fallback (same as forwardToolCall).
		entry = r.resolveSessionFromDB(memberSessionID)
		if entry == nil {
			return
		}
		r.mu.RLock()
	}
	ws := r.workflows[entry.WorkflowID]
	if ws == nil || !ws.Forwarding {
		r.mu.RUnlock()
		return
	}
	userSessionID := ws.UserSessionID
	r.mu.RUnlock()

	r.toolTracker.OnToolResult(userSessionID, tr)

	// Emit to SSE so web chat sees the tool result.
	if r.onEvent != nil {
		r.onEvent(agent.Event{
			Type:       agent.EventToolResult,
			SessionID:  userSessionID,
			ToolResult: tr,
		})
	}
}

// emitTaskStarted emits a synthetic _wf_task_started tool call on the user's session.
func (r *WorkflowRelay) emitTaskStarted(e agent.Event) {
	taskID := e.Text
	if taskID == "" || e.TeamData == nil {
		return
	}

	// Find the workflow for this team by matching TeamID.
	teamID := ""
	if e.TeamData != nil {
		teamID = e.TeamData.TeamID
	}
	if teamID == "" {
		return
	}

	r.mu.RLock()
	var ws *relayState
	var wfID string
	for id, w := range r.workflows {
		if w.TeamID == teamID {
			wfID = id
			ws = w
			break
		}
	}
	r.mu.RUnlock()
	if ws == nil {
		return
	}

	// Extract task info from TeamData.
	var title, assignee string
	if e.TeamData.Task != nil {
		if task, ok := e.TeamData.Task.(*store.TeamTask); ok {
			title = task.Title
			assignee = task.AssignedTo
		}
	}
	if title == "" {
		title = taskID[:min(8, len(taskID))]
	}

	callID := fmt.Sprintf("wf_%s_%s", wfID[:min(8, len(wfID))], taskID[:min(8, len(taskID))])
	r.mu.Lock()
	r.taskCallIDs[wfID+":"+taskID] = callID
	r.mu.Unlock()

	input, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"title":    title,
		"assignee": assignee,
	})
	syntheticTC := &canonical.ToolCall{
		ID:    callID,
		Name:  "_wf_task_started",
		Input: input,
	}
	r.toolTracker.OnToolCall(ws.UserSessionID, syntheticTC)
	if r.onEvent != nil {
		r.onEvent(agent.Event{
			Type:      agent.EventToolCall,
			SessionID: ws.UserSessionID,
			ToolCall:  syntheticTC,
		})
	}
}

// emitTaskCompleted emits a synthetic tool result for a completed task.
func (r *WorkflowRelay) emitTaskCompleted(e agent.Event) {
	r.emitTaskResult(e, false)
}

// emitTaskFailed emits a synthetic error tool result for a failed task.
func (r *WorkflowRelay) emitTaskFailed(e agent.Event) {
	r.emitTaskResult(e, true)
}

func (r *WorkflowRelay) emitTaskResult(e agent.Event, isError bool) {
	taskID := e.Text
	if taskID == "" {
		return
	}

	// Find the call ID for this task.
	r.mu.RLock()
	var callID string
	var userSessionID string
	for wfID, ws := range r.workflows {
		key := wfID + ":" + taskID
		if cid, ok := r.taskCallIDs[key]; ok {
			callID = cid
			userSessionID = ws.UserSessionID
			break
		}
	}
	r.mu.RUnlock()

	if callID == "" || userSessionID == "" {
		return
	}

	var content string
	if e.TeamData != nil && e.TeamData.Task != nil {
		if task, ok := e.TeamData.Task.(*store.TeamTask); ok {
			if isError {
				content = task.ErrorMessage
			} else {
				content = "Completed"
				if task.Result != "" && len(task.Result) < 100 {
					content = task.Result
				}
			}
		}
	}
	if content == "" {
		if isError {
			content = "Task failed"
		} else {
			content = "Task completed"
		}
	}

	syntheticTR := &canonical.ToolResult{
		ToolCallID: callID,
		Content:    content,
		IsError:    isError,
	}
	r.toolTracker.OnToolResult(userSessionID, syntheticTR)
	if r.onEvent != nil {
		r.onEvent(agent.Event{
			Type:       agent.EventToolResult,
			SessionID:  userSessionID,
			ToolResult: syntheticTR,
		})
	}
}

// resolveSessionFromDB attempts to resolve a member session to a workflow via DB lookup.
// Used as a fallback when the in-memory index doesn't have the session (e.g., after restart).
// Returns nil if the session is not associated with any active workflow.
func (r *WorkflowRelay) resolveSessionFromDB(memberSessionID string) *RelayEntry {
	ctx := context.Background()

	// Look up the session to find which agent it belongs to.
	sess, err := r.store.GetSession(ctx, memberSessionID)
	if err != nil || sess == nil {
		return nil
	}

	// Find the agent's team.
	teamInfo, role, err := r.store.GetTeamByAgent(ctx, sess.AgentID)
	if err != nil || teamInfo == nil || role != "member" {
		return nil
	}

	// Find an active workflow for this team.
	r.mu.RLock()
	var wfID string
	for id, ws := range r.workflows {
		if ws.TeamID == teamInfo.ID {
			wfID = id
			break
		}
	}
	r.mu.RUnlock()

	if wfID == "" {
		return nil
	}

	// Build and cache the entry.
	entry := &RelayEntry{
		WorkflowID:        wfID,
		UserSessionID:     "", // Will be filled from workflow state.
		TeamID:            teamInfo.ID,
		MemberAgentID:     sess.AgentID,
		MemberDisplayName: sess.AgentID, // Fallback: use agent ID as name.
	}

	r.mu.RLock()
	if ws, ok := r.workflows[wfID]; ok {
		entry.UserSessionID = ws.UserSessionID
	}
	r.mu.RUnlock()

	if entry.UserSessionID == "" {
		return nil
	}

	// Cache for future lookups.
	r.RegisterMemberSession(memberSessionID, *entry)

	return entry
}

// EmitDelegating emits a _wf_delegating synthetic tool call.
func (r *WorkflowRelay) EmitDelegating(wfID, userSessionID string, taskCount int) {
	r.toolTracker.EnsureSession(userSessionID)

	callID := fmt.Sprintf("wf_deleg_%s", wfID[:min(8, len(wfID))])
	input, _ := json.Marshal(map[string]string{
		"count": fmt.Sprintf("%d", taskCount),
	})
	syntheticTC := &canonical.ToolCall{
		ID:    callID,
		Name:  "_wf_delegating",
		Input: input,
	}
	r.toolTracker.OnToolCall(userSessionID, syntheticTC)
	if r.onEvent != nil {
		r.onEvent(agent.Event{Type: agent.EventToolCall, SessionID: userSessionID, ToolCall: syntheticTC})
	}

	// Immediately complete it (delegation is instant).
	syntheticTR := &canonical.ToolResult{
		ToolCallID: callID,
		Content:    fmt.Sprintf("%d tasks created", taskCount),
	}
	r.toolTracker.OnToolResult(userSessionID, syntheticTR)
	if r.onEvent != nil {
		r.onEvent(agent.Event{Type: agent.EventToolResult, SessionID: userSessionID, ToolResult: syntheticTR})
	}
}
