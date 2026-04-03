package team

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/store"
	sqlitestore "github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestCollector(t *testing.T) (*WorkflowEventCollector, *sqlitestore.Store, []agent.Event) {
	t.Helper()
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	var sseEvents []agent.Event
	tracker := toolstatus.NewTracker(toolstatus.DefaultDisplayMap(), nil, nil)

	collector := NewWorkflowEventCollector(s, func(e agent.Event) {
		sseEvents = append(sseEvents, e)
	}, tracker)

	return collector, s, sseEvents
}

func TestCollector_RegisterWorkflow(t *testing.T) {
	collector, _, _ := newTestCollector(t)

	members := map[string]string{"analyst": "Analyst", "writer": "Writer"}
	collector.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1", members)

	collector.mu.RLock()
	defer collector.mu.RUnlock()

	wf, ok := collector.workflows["wf-1"]
	if !ok {
		t.Fatal("workflow not registered")
	}
	if wf.UserSessionID != "user-sess-1" {
		t.Errorf("session = %q", wf.UserSessionID)
	}
	if len(wf.Members) != 2 {
		t.Errorf("members count = %d", len(wf.Members))
	}

	// Agent index should have both members.
	if collector.agentIndex["analyst"] != "wf-1" {
		t.Error("analyst not indexed")
	}
	if collector.agentIndex["writer"] != "wf-1" {
		t.Error("writer not indexed")
	}
}

func TestCollector_AgentIDMatching(t *testing.T) {
	collector, s, _ := newTestCollector(t)

	// Create a session for the member to write activity into.
	teamID := createTestTeamInDB(t, s, "lead-1", "analyst")
	_ = teamID

	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1", members)

	// Simulate a member tool call event.
	input, _ := json.Marshal(map[string]string{"query": "AI frameworks"})
	collector.HandleEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "member-sess-1", // Different from user session.
		AgentID:   "analyst",       // Matches registered member.
		ToolCall: &canonical.ToolCall{
			ID:    "tc-1",
			Name:  "web_search",
			Input: input,
		},
	})

	// Verify the batch has the activity.
	collector.batchMu.Lock()
	b, ok := collector.batches["wf-1"]
	collector.batchMu.Unlock()
	if !ok {
		t.Fatal("no batch created")
	}
	if len(b.activities) != 1 {
		t.Fatalf("expected 1 activity, got %d", len(b.activities))
	}
	if b.activities[0].Meta["agent_name"] != "Analyst" {
		t.Errorf("agent_name = %q", b.activities[0].Meta["agent_name"])
	}
	if b.activities[0].Meta["detail"] != "AI frameworks" {
		t.Errorf("detail = %q", b.activities[0].Meta["detail"])
	}
}

func TestCollector_IgnoresNonMembers(t *testing.T) {
	collector, _, _ := newTestCollector(t)

	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1", members)

	// Event from unknown agent.
	collector.HandleEvent(agent.Event{
		Type:      agent.EventToolCall,
		SessionID: "other-sess",
		AgentID:   "unknown-agent",
		ToolCall:  &canonical.ToolCall{ID: "tc-1", Name: "web_search"},
	})

	// No batch should be created.
	collector.batchMu.Lock()
	_, ok := collector.batches["wf-1"]
	collector.batchMu.Unlock()
	if ok {
		t.Error("batch created for non-member agent")
	}
}

func TestCollector_FiltersWorkflowTools(t *testing.T) {
	collector, _, _ := newTestCollector(t)

	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", "user-sess-1", "lead-1", "team-1", members)

	collector.HandleEvent(agent.Event{
		Type:      agent.EventToolCall,
		AgentID:   "analyst",
		ToolCall:  &canonical.ToolCall{ID: "tc-1", Name: "_workflow_analyze"},
	})

	// No batch.
	collector.batchMu.Lock()
	_, ok := collector.batches["wf-1"]
	collector.batchMu.Unlock()
	if ok {
		t.Error("should not batch _workflow_* tools")
	}
}

func TestCollector_BatchFlushOnCapacity(t *testing.T) {
	collector, s, _ := newTestCollector(t)

	// Create a user session so AppendMessages works.
	s.CreateSessionWithKind(context.Background(), "web", "test-chat", "researcher", "dm")
	// Get the session ID.
	sessions, _ := s.ListSessions(context.Background(), 1)
	if len(sessions) == 0 {
		t.Fatal("no session created")
	}
	userSessionID := sessions[0].ID

	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", userSessionID, "lead-1", "team-1", members)

	// Add 10 events to trigger capacity flush.
	for i := 0; i < batchMaxCapacity; i++ {
		collector.HandleEvent(agent.Event{
			Type:    agent.EventToolCall,
			AgentID: "analyst",
			ToolCall: &canonical.ToolCall{
				ID:   fmt.Sprintf("tc-%d", i),
				Name: "web_search",
			},
		})
	}

	// Wait for async flush.
	time.Sleep(100 * time.Millisecond)

	// Verify messages were persisted.
	msgs, err := s.GetMessages(context.Background(), userSessionID, 100)
	if err != nil {
		t.Fatalf("get messages: %v", err)
	}

	// Find workflow_activity content.
	found := 0
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "workflow_activity" {
				found++
			}
		}
	}
	if found < batchMaxCapacity {
		t.Errorf("expected %d activity blocks persisted, got %d", batchMaxCapacity, found)
	}
}

func TestCollector_UnregisterFlushes(t *testing.T) {
	collector, s, _ := newTestCollector(t)

	sessions, _ := s.ListSessions(context.Background(), 1)
	userSessionID := ""
	if len(sessions) == 0 {
		sess, _ := s.CreateSessionWithKind(context.Background(), "web", "chat", "researcher", "dm")
		userSessionID = sess.ID
	} else {
		userSessionID = sessions[0].ID
	}

	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", userSessionID, "lead-1", "team-1", members)

	// Add one event (won't auto-flush — below capacity, timer not expired).
	collector.HandleEvent(agent.Event{
		Type:    agent.EventToolCall,
		AgentID: "analyst",
		ToolCall: &canonical.ToolCall{ID: "tc-1", Name: "web_search"},
	})

	// Unregister flushes the pending batch.
	collector.UnregisterWorkflow("wf-1")

	// Agent should be unindexed.
	collector.mu.RLock()
	_, indexed := collector.agentIndex["analyst"]
	collector.mu.RUnlock()
	if indexed {
		t.Error("analyst should be unindexed after unregister")
	}

	// Message should be persisted.
	msgs, _ := s.GetMessages(context.Background(), userSessionID, 100)
	found := false
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "workflow_activity" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected flush on unregister")
	}
}

func TestCollector_DetailExtraction(t *testing.T) {
	tests := []struct {
		tool   string
		input  map[string]any
		expect string
	}{
		{"web_search", map[string]any{"query": "AI frameworks"}, "AI frameworks"},
		{"web_fetch", map[string]any{"url": "https://example.com/page"}, "example.com"},
		{"read_file", map[string]any{"path": "/home/user/test.go"}, "test.go"},
		{"execute_command", map[string]any{"command": "echo hello world"}, "echo hello world"},
		{"memory_search", map[string]any{"query": "project architecture"}, "project architecture"},
		{"team_tasks", map[string]any{"action": "create", "subject": "Research"}, "create: Research"},
	}

	for _, tt := range tests {
		input, _ := json.Marshal(tt.input)
		detail := extractToolDetail(tt.tool, input)
		if detail != tt.expect {
			t.Errorf("%s: detail = %q, want %q", tt.tool, detail, tt.expect)
		}
	}
}

func TestCollector_TimerFlush(t *testing.T) {
	collector, s, _ := newTestCollector(t)

	sess, _ := s.CreateSessionWithKind(context.Background(), "web", "chat", "researcher", "dm")
	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", sess.ID, "lead-1", "team-1", members)

	// Add one event (below capacity).
	collector.HandleEvent(agent.Event{
		Type:    agent.EventToolCall,
		AgentID: "analyst",
		ToolCall: &canonical.ToolCall{ID: "tc-1", Name: "web_search"},
	})

	// Wait for timer flush (1.5s + margin).
	time.Sleep(2 * time.Second)

	msgs, _ := s.GetMessages(context.Background(), sess.ID, 100)
	found := 0
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "workflow_activity" {
				found++
			}
		}
	}
	if found == 0 {
		t.Error("expected timer-flushed activity in DB")
	}
}

func TestCollector_TaskLifecycle(t *testing.T) {
	collector, s, _ := newTestCollector(t)

	teamID := createTestTeamInDB(t, s, "lead-1", "analyst")
	sess, _ := s.CreateSessionWithKind(context.Background(), "web", "chat", "researcher", "dm")
	members := map[string]string{"analyst": "Analyst"}
	collector.RegisterWorkflow("wf-1", sess.ID, "lead-1", teamID, members)

	// Task claimed event.
	collector.HandleEvent(agent.Event{
		Type: agent.EventTeamTaskClaimed,
		Text: "task-12345678",
		TeamData: &agent.TeamEventData{
			TeamID: teamID,
			TaskID: "task-12345678",
			Task: &store.TeamTask{
				Title:      "Research AI",
				AssignedTo: "analyst",
			},
		},
	})

	// Task completed — triggers state-change flush.
	collector.HandleEvent(agent.Event{
		Type: agent.EventTeamTaskCompleted,
		Text: "task-12345678",
		TeamData: &agent.TeamEventData{
			TeamID: teamID,
			TaskID: "task-12345678",
			Task:   &store.TeamTask{Title: "Research AI"},
		},
	})

	// Verify persisted.
	msgs, _ := s.GetMessages(context.Background(), sess.ID, 100)
	types := map[string]bool{}
	for _, m := range msgs {
		for _, c := range m.Content {
			if c.Type == "workflow_activity" && c.Meta != nil {
				types[c.Meta["activity_type"]] = true
			}
		}
	}
	if !types["task_started"] {
		t.Error("missing task_started activity")
	}
	if !types["task_completed"] {
		t.Error("missing task_completed activity")
	}
}

func TestCollector_PanicRecovery(t *testing.T) {
	collector, _, _ := newTestCollector(t)

	// Should not panic with nil ToolCall.
	collector.HandleEvent(agent.Event{
		Type:    agent.EventToolCall,
		AgentID: "analyst",
	})
}
