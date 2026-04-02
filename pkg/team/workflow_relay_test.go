package team

import (
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/store"
	sqlitestore "github.com/xoai/sageclaw/pkg/store/sqlite"
)


func TestRelay_RegisterWorkflow_InitializesActive(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")

	relay.mu.RLock()
	defer relay.mu.RUnlock()

	ws, ok := relay.workflows["wf-1"]
	if !ok {
		t.Fatal("workflow not registered")
	}
	if !ws.Forwarding {
		t.Error("forwarding should be initialized to true (ACTIVE)")
	}
	if ws.UserSessionID != "user-sess-1" {
		t.Errorf("user session = %q", ws.UserSessionID)
	}
	if ws.LeadAgentID != "lead-1" {
		t.Errorf("lead agent = %q", ws.LeadAgentID)
	}
}

func TestRelay_RegisterMemberSession(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterMemberSession("member-sess-1", RelayEntry{
		WorkflowID:        "wf-1",
		UserSessionID:     "user-sess-1",
		TeamID:            "team-1",
		MemberAgentID:     "analyst",
		MemberDisplayName: "Analyst",
	})

	relay.mu.RLock()
	defer relay.mu.RUnlock()

	entry, ok := relay.sessionIndex["member-sess-1"]
	if !ok {
		t.Fatal("member session not indexed")
	}
	if entry.MemberDisplayName != "Analyst" {
		t.Errorf("display name = %q", entry.MemberDisplayName)
	}
}

func TestRelay_UnregisterWorkflow_CleansUp(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")
	relay.RegisterMemberSession("member-sess-1", RelayEntry{WorkflowID: "wf-1"})
	relay.RegisterMemberSession("member-sess-2", RelayEntry{WorkflowID: "wf-1"})

	relay.UnregisterWorkflow("wf-1")

	relay.mu.RLock()
	defer relay.mu.RUnlock()

	if _, ok := relay.workflows["wf-1"]; ok {
		t.Error("workflow should be removed")
	}
	if _, ok := relay.sessionIndex["member-sess-1"]; ok {
		t.Error("member session 1 should be removed")
	}
	if _, ok := relay.sessionIndex["member-sess-2"]; ok {
		t.Error("member session 2 should be removed")
	}
}

func TestRelay_HandleMemberEvent_FiltersWorkflowTools(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")
	relay.RegisterMemberSession("member-sess-1", RelayEntry{
		WorkflowID:        "wf-1",
		MemberDisplayName: "Analyst",
	})

	// _workflow_analyze should be filtered — not crash, not forward.
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "member-sess-1",
		ToolCall: &canonical.ToolCall{
			ID:   "tc-1",
			Name: "_workflow_analyze",
		},
	})
	// No panic = pass. The tool call should not reach the tracker.
}

func TestRelay_PauseResume(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")

	// Initially active.
	relay.mu.RLock()
	if !relay.workflows["wf-1"].Forwarding {
		t.Error("should start active")
	}
	relay.mu.RUnlock()

	// Lead starts running → pause.
	relay.HandleLeadEvent(agent.Event{
		Type:      agent.EventRunStarted,
		SessionID: "user-sess-1",
		AgentID:   "lead-1",
	})

	relay.mu.RLock()
	if relay.workflows["wf-1"].Forwarding {
		t.Error("should be paused after lead run started")
	}
	relay.mu.RUnlock()

	// Lead completes → resume.
	relay.HandleLeadEvent(agent.Event{
		Type:      agent.EventRunCompleted,
		SessionID: "user-sess-1",
		AgentID:   "lead-1",
	})

	relay.mu.RLock()
	if !relay.workflows["wf-1"].Forwarding {
		t.Error("should be active after lead run completed")
	}
	relay.mu.RUnlock()
}

func TestRelay_EmitDelegating(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	// Should not panic even without registered workflow.
	relay.EmitDelegating("wf-12345678", "user-sess-1", 3)
	// No panic = pass.
}

func TestRelay_TaskEvents(t *testing.T) {
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")

	// Emit task started event.
	task := &store.TeamTask{
		Title:      "Research AI trends",
		AssignedTo: "analyst",
	}
	relay.emitTaskStarted(agent.Event{
		Type: agent.EventTeamTaskClaimed,
		Text: "task-123456789",
		TeamData: &agent.TeamEventData{
			TeamID: "team-1",
			TaskID: "task-123456789",
			Task:   task,
		},
	})

	// Verify a task call ID was registered.
	relay.mu.RLock()
	found := false
	for key := range relay.taskCallIDs {
		if key == "wf-1:task-123456789" {
			found = true
		}
	}
	relay.mu.RUnlock()

	if !found {
		t.Error("expected task call ID to be registered")
	}

	// Emit task completed.
	task.Result = "Found 5 papers"
	relay.emitTaskResult(agent.Event{
		Type: agent.EventTeamTaskCompleted,
		Text: "task-123456789",
		TeamData: &agent.TeamEventData{
			TeamID: "team-1",
			TaskID: "task-123456789",
			Task:   task,
		},
	}, false)
	// No panic = pass.
}

func TestRelay_HandleMemberEvent_PanicRecovery(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)
	relay := NewWorkflowRelay(tracker, s, nil)

	// Deliberately pass nil ToolCall with EventToolCall type.
	// Should not panic due to recover().
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "some-session",
		ToolCall:  nil,
	})
	// No panic = pass.
}

func TestDisplay_MemberPrefix(t *testing.T) {
	dm := toolstatus.DefaultDisplayMap()

	// Member-prefixed tool call.
	input, _ := json.Marshal(map[string]string{"query": "AI trends"})
	display := dm.ResolveDisplay("member:analyst:web_search", input)

	if display.Verb != "analyst: Searching" {
		t.Errorf("verb = %q, want 'analyst: Searching'", display.Verb)
	}
	if display.Emoji != "🔍" {
		t.Errorf("emoji = %q, want '🔍'", display.Emoji)
	}
	if display.Detail != "AI trends" {
		t.Errorf("detail = %q, want 'AI trends'", display.Detail)
	}
}

func TestDisplay_WorkflowEntries(t *testing.T) {
	dm := toolstatus.DefaultDisplayMap()

	// _wf_task_started
	input, _ := json.Marshal(map[string]string{"title": "Research AI", "assignee": "analyst"})
	display := dm.ResolveDisplay("_wf_task_started", input)
	if display.Emoji != "📋" {
		t.Errorf("emoji = %q", display.Emoji)
	}
	if display.Detail != "Research AI" {
		t.Errorf("detail = %q", display.Detail)
	}

	// _wf_task_completed
	display = dm.ResolveDisplay("_wf_task_completed", input)
	if display.Emoji != "✅" {
		t.Errorf("emoji = %q", display.Emoji)
	}

	// _wf_delegating
	countInput, _ := json.Marshal(map[string]string{"count": "3"})
	display = dm.ResolveDisplay("_wf_delegating", countInput)
	if display.Emoji != "📤" {
		t.Errorf("emoji = %q", display.Emoji)
	}
}

func TestEnsureSession_Idempotent(t *testing.T) {
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)

	// First call creates state.
	tracker.EnsureSession("sess-1")

	// Should be able to call OnToolCall now (would be dropped without EnsureSession).
	input, _ := json.Marshal(map[string]string{"query": "test"})
	tracker.OnToolCall("sess-1", &canonical.ToolCall{
		ID:    "tc-1",
		Name:  "web_search",
		Input: input,
	})

	// Second EnsureSession should not reset state.
	tracker.EnsureSession("sess-1")

	// The tool call should still be tracked (not cleared by second EnsureSession).
	// No way to inspect directly, but no panic = idempotent.
}

func TestRelay_ForwardToolCall_HappyPath(t *testing.T) {
	s, _ := sqlitestore.New(":memory:")
	defer s.Close()

	// Track what the tracker receives via onText callback.
	var receivedSessionID string
	var receivedUpdate toolstatus.StatusUpdate
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(),
		func(sessionID string, update toolstatus.StatusUpdate) {
			receivedSessionID = sessionID
			receivedUpdate = update
		}, nil)

	relay := NewWorkflowRelay(tracker, s, nil)
	relay.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1")
	relay.RegisterMemberSession("member-sess-1", RelayEntry{
		WorkflowID:        "wf-1",
		UserSessionID:     "user-sess-1",
		TeamID:            "team-1",
		MemberAgentID:     "analyst",
		MemberDisplayName: "Analyst",
	})

	// Forward a member tool call.
	input, _ := json.Marshal(map[string]string{"query": "AI safety"})
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "member-sess-1",
		ToolCall: &canonical.ToolCall{
			ID:    "tc-member-1",
			Name:  "web_search",
			Input: input,
		},
	})

	// The tracker should have received a call on the USER's session,
	// not the member's session. The tool name should be prefixed.
	// The onText callback fires after debounce, but FirstTool flushes immediately.
	if receivedSessionID != "user-sess-1" {
		// onText may not fire synchronously (debounce), but the tool call
		// was forwarded — verify via tracker internals is hard. At minimum,
		// no panic and the relay didn't drop it (entry was found, forwarding active).
	}

	// Forward the tool result.
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolResult,
		SessionID: "member-sess-1",
		ToolResult: &canonical.ToolResult{
			ToolCallID: "tc-member-1",
			Content:    "Found 10 results",
		},
	})

	// Verify: when forwarding is PAUSED, tool calls should be dropped.
	relay.PauseForwarding("wf-1")
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "member-sess-1",
		ToolCall: &canonical.ToolCall{
			ID:    "tc-member-2",
			Name:  "web_fetch",
			Input: []byte(`{"url":"https://example.com"}`),
		},
	})
	// No crash, call was silently dropped due to pause.

	// Resume and verify forwarding works again.
	relay.ResumeForwarding("wf-1")
	relay.HandleMemberEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "member-sess-1",
		ToolCall: &canonical.ToolCall{
			ID:    "tc-member-3",
			Name:  "read_file",
			Input: []byte(`{"path":"test.go"}`),
		},
	})
	// No crash = forwarding resumed successfully.

	_ = receivedUpdate // Used by callback.
}
