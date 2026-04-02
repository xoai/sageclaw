package team

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
	sqlitestore "github.com/xoai/sageclaw/pkg/store/sqlite"
)

func TestFormatWorkflowResults_AllCompleted(t *testing.T) {
	results := []TaskResult{
		{TaskID: "t1", Title: "Research AI", Status: "completed", Result: "Found 5 papers on safety."},
		{TaskID: "t2", Title: "Write report", Status: "completed", Result: "Report covers recent trends."},
	}
	data, _ := json.Marshal(results)
	formatted := FormatWorkflowResults(string(data))

	if !strings.Contains(formatted, "[Workflow Results]") {
		t.Error("missing [Workflow Results] header")
	}
	if !strings.Contains(formatted, "Research AI") {
		t.Error("missing task title")
	}
	if !strings.Contains(formatted, "Found 5 papers") {
		t.Error("missing result content")
	}
	if !strings.Contains(formatted, "Synthesize") {
		t.Error("missing synthesis instruction")
	}
}

func TestFormatWorkflowResults_MixedStatuses(t *testing.T) {
	results := []TaskResult{
		{TaskID: "t1", Title: "Research", Status: "completed", Result: "Good data."},
		{TaskID: "t2", Title: "Design", Status: "failed", Error: "timed out"},
		{TaskID: "t3", Title: "Review", Status: "cancelled"},
	}
	data, _ := json.Marshal(results)
	formatted := FormatWorkflowResults(string(data))

	if !strings.Contains(formatted, "<result>") {
		t.Error("missing result tag")
	}
	if !strings.Contains(formatted, "<error>") {
		t.Error("missing error tag")
	}
	if !strings.Contains(formatted, "<cancelled/>") {
		t.Error("missing cancelled tag")
	}
}

func TestFormatWorkflowResults_Truncation(t *testing.T) {
	// Create a result that exceeds budget.
	bigResult := strings.Repeat("x", MaxResultChars+1000)
	results := []TaskResult{
		{TaskID: "t1", Title: "Big task", Status: "completed", Result: bigResult},
	}
	data, _ := json.Marshal(results)
	formatted := FormatWorkflowResults(string(data))

	if !strings.Contains(formatted, "[truncated") {
		t.Error("expected truncation marker")
	}
	if len(formatted) > MaxResultChars+5000 { // Some overhead for tags and instructions.
		t.Errorf("formatted too large: %d chars", len(formatted))
	}
}

func TestFormatWorkflowResults_EmptyJSON(t *testing.T) {
	formatted := FormatWorkflowResults("")
	if formatted != "" {
		t.Error("expected empty for invalid JSON")
	}
	formatted = FormatWorkflowResults("[]")
	if formatted != "" {
		t.Error("expected empty for empty array")
	}
}

func TestHandleLeadRunComplete_AdvancesToComplete(t *testing.T) {
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	var completedEvents []agent.Event
	exec := NewTeamExecutor(s, nil, func(e agent.Event) {})
	engine := NewWorkflowEngine(s, exec, func(e agent.Event) {
		completedEvents = append(completedEvents, e)
	})

	ctx := context.Background()

	// Create workflow and advance to synthesize state.
	teamID := createTestTeamInDB(t, s, "lead-1", "member-1")
	wfID, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: teamID, SessionID: "sess-1",
	})
	s.UpdateWorkflowState(ctx, wfID, StatePlan, 0, nil)
	s.UpdateWorkflowState(ctx, wfID, StateCreate, 1, nil)
	s.UpdateWorkflowState(ctx, wfID, StateExecute, 2, nil)
	s.UpdateWorkflowState(ctx, wfID, StateMonitor, 3, nil)
	s.UpdateWorkflowState(ctx, wfID, StateSynthesize, 4, map[string]any{
		"results_json": `[{"task_id":"t1","title":"Research","status":"completed","result":"data"}]`,
	})

	// Simulate lead run completing after synthesis.
	engine.HandleLeadRunComplete(ctx, teamID, "sess-1", "The research found important AI safety data and trends.")

	// Workflow should be complete.
	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateComplete {
		t.Errorf("state = %q, want complete", wf.State)
	}
	if wf.CompletedAt == nil {
		t.Error("expected completed_at to be set")
	}

	// Should have emitted workflow.completed event.
	found := false
	for _, e := range completedEvents {
		if e.Type == agent.EventWorkflowCompleted {
			found = true
		}
	}
	if !found {
		t.Error("expected workflow.completed event")
	}
}

func TestHandleLeadRunComplete_IgnoresNonSynthesizeState(t *testing.T) {
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	exec := NewTeamExecutor(s, nil, func(e agent.Event) {})
	engine := NewWorkflowEngine(s, exec, func(e agent.Event) {})

	ctx := context.Background()
	teamID := createTestTeamInDB(t, s, "lead-1", "member-1")

	// Create workflow in execute state.
	wfID, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: teamID, SessionID: "sess-1",
	})
	s.UpdateWorkflowState(ctx, wfID, StatePlan, 0, nil)
	s.UpdateWorkflowState(ctx, wfID, StateCreate, 1, nil)
	s.UpdateWorkflowState(ctx, wfID, StateExecute, 2, nil)

	// Lead run completes — should NOT advance (not in synthesize).
	engine.HandleLeadRunComplete(ctx, teamID, "sess-1", "The research found important AI safety data and trends.")

	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateExecute {
		t.Errorf("state = %q, want execute (should not advance)", wf.State)
	}
}

func TestRecoverStaleWorkflows_FailsStuckAnalyze(t *testing.T) {
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	exec := NewTeamExecutor(s, nil, func(e agent.Event) {})
	engine := NewWorkflowEngine(s, exec, func(e agent.Event) {})
	monitor := engine.Monitor()

	ctx := context.Background()

	// Create a workflow stuck in analyze for >5 min.
	wfID, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{
		TeamID: "team-1", SessionID: "sess-1",
	})
	// Force state_entered_at to be old.
	s.DB().ExecContext(ctx, `UPDATE team_workflows SET state_entered_at = datetime('now', '-10 minutes') WHERE id = ?`, wfID)

	monitor.recoverStaleWorkflows()

	wf, _ := s.GetWorkflow(ctx, wfID)
	if wf.State != StateFailed {
		t.Errorf("state = %q, want failed", wf.State)
	}
}
