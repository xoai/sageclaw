package team

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/store"
	sqlitestore "github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestWorkflowEngine(t *testing.T) (*WorkflowEngine, store.Store) {
	t.Helper()
	s, err := sqlitestore.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	exec := NewTeamExecutor(s, nil, func(e agent.Event) {})
	engine := NewWorkflowEngine(s, exec, func(e agent.Event) {})
	return engine, s
}

// --- validatePlan tests ---

func TestValidatePlan_Valid(t *testing.T) {
	plan := PlanResult{
		Tasks: []PlanTask{
			{Subject: "Research AI", Assignee: "researcher", Description: "Find papers"},
			{Subject: "Write report", Assignee: "writer", Description: "Write based on research", BlockedBy: []string{"$TASK_0"}},
		},
		Announcement: "Delegating research and writing",
	}
	errs := validatePlan(plan, []string{"researcher", "writer"})
	if len(errs) != 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

func TestValidatePlan_EmptyTasks(t *testing.T) {
	plan := PlanResult{Tasks: nil, Announcement: "test"}
	errs := validatePlan(plan, []string{"researcher"})
	if len(errs) == 0 {
		t.Error("expected error for empty tasks")
	}
}

func TestValidatePlan_EmptyAnnouncement(t *testing.T) {
	plan := PlanResult{
		Tasks: []PlanTask{{Subject: "A", Assignee: "researcher", Description: "Do A"}},
	}
	errs := validatePlan(plan, []string{"researcher"})
	found := false
	for _, e := range errs {
		if e == "Plan must include an announcement message for the user." {
			found = true
		}
	}
	if !found {
		t.Error("expected announcement error")
	}
}

func TestValidatePlan_InvalidAssignee(t *testing.T) {
	plan := PlanResult{
		Tasks:        []PlanTask{{Subject: "A", Assignee: "unknown", Description: "Do A"}},
		Announcement: "test",
	}
	errs := validatePlan(plan, []string{"researcher"})
	if len(errs) == 0 {
		t.Error("expected error for unknown assignee")
	}
}

func TestValidatePlan_EmptyFields(t *testing.T) {
	plan := PlanResult{
		Tasks:        []PlanTask{{Subject: "", Assignee: "", Description: ""}},
		Announcement: "test",
	}
	errs := validatePlan(plan, []string{"researcher"})
	if len(errs) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errs), errs)
	}
}

func TestValidatePlan_ForwardReference(t *testing.T) {
	plan := PlanResult{
		Tasks: []PlanTask{
			{Subject: "A", Assignee: "researcher", Description: "Do A", BlockedBy: []string{"$TASK_1"}},
			{Subject: "B", Assignee: "writer", Description: "Do B"},
		},
		Announcement: "test",
	}
	errs := validatePlan(plan, []string{"researcher", "writer"})
	if len(errs) == 0 {
		t.Error("expected error for forward reference")
	}
}

func TestValidatePlan_SelfBlock(t *testing.T) {
	plan := PlanResult{
		Tasks: []PlanTask{
			{Subject: "A", Assignee: "researcher", Description: "Do A", BlockedBy: []string{"$TASK_0"}},
		},
		Announcement: "test",
	}
	errs := validatePlan(plan, []string{"researcher"})
	if len(errs) == 0 {
		t.Error("expected error for self-block")
	}
}

func TestValidatePlan_CompoundSubject(t *testing.T) {
	plan := PlanResult{
		Tasks: []PlanTask{
			{Subject: "Research trends and write a summary", Assignee: "researcher", Description: "Do both"},
		},
		Announcement: "test",
	}
	errs := validatePlan(plan, []string{"researcher"})
	if len(errs) == 0 {
		t.Error("expected soft warning for compound subject")
	}
}

// --- detectCycle tests ---

func TestDetectCycle_NoCycle(t *testing.T) {
	tasks := []PlanTask{
		{BlockedBy: nil},
		{BlockedBy: []string{"$TASK_0"}},
		{BlockedBy: []string{"$TASK_1"}},
	}
	if cycle := detectCycle(tasks); cycle != "" {
		t.Errorf("unexpected cycle: %s", cycle)
	}
}

func TestDetectCycle_WithCycle(t *testing.T) {
	tasks := []PlanTask{
		{BlockedBy: []string{"$TASK_1"}},
		{BlockedBy: []string{"$TASK_0"}},
	}
	if cycle := detectCycle(tasks); cycle == "" {
		t.Error("expected cycle detection")
	}
}

// --- IsTerminal tests ---

func TestIsTerminal(t *testing.T) {
	for _, s := range []string{StateComplete, StateCancelled, StateFailed} {
		if !IsTerminal(s) {
			t.Errorf("%q should be terminal", s)
		}
	}
	for _, s := range []string{StateAnalyze, StatePlan, StateCreate, StateExecute, StateMonitor} {
		if IsTerminal(s) {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

// --- HandleAnalyze tests ---

func TestHandleAnalyze_NoDelegation(t *testing.T) {
	engine, _ := newTestWorkflowEngine(t)
	ctx := context.Background()

	input, _ := json.Marshal(AnalyzeResult{Delegate: false, Confidence: 0.9, Reason: "Simple question"})
	text, started, err := engine.HandleAnalyze(ctx, "team-1", "sess-1", "Hello", string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started {
		t.Error("should not start workflow for no-delegation")
	}
	if text == "" {
		t.Error("expected non-empty text")
	}
}

func TestHandleAnalyze_LowConfidence(t *testing.T) {
	engine, _ := newTestWorkflowEngine(t)
	ctx := context.Background()

	input, _ := json.Marshal(AnalyzeResult{Delegate: true, Confidence: 0.3, Reason: "Maybe"})
	text, started, err := engine.HandleAnalyze(ctx, "team-1", "sess-1", "Hello", string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started {
		t.Error("should not start workflow with low confidence")
	}
	if text == "" {
		t.Error("expected guidance text")
	}
}

func TestHandleAnalyze_StartWorkflow(t *testing.T) {
	engine, s := newTestWorkflowEngine(t)
	ctx := context.Background()

	input, _ := json.Marshal(AnalyzeResult{Delegate: true, Confidence: 0.85, Reason: "Needs research"})
	text, started, err := engine.HandleAnalyze(ctx, "team-1", "sess-1", "Research AI", string(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !started {
		t.Error("expected workflow to start")
	}
	if text == "" {
		t.Error("expected non-empty text")
	}

	// Verify workflow was created and advanced to plan.
	active, _ := s.GetActiveWorkflow(ctx, "team-1", "sess-1")
	if active == nil {
		t.Fatal("expected active workflow")
	}
	if active.State != StatePlan {
		t.Errorf("state = %q, want plan", active.State)
	}
}

func TestHandleAnalyze_ConcurrentGuard(t *testing.T) {
	engine, _ := newTestWorkflowEngine(t)
	ctx := context.Background()

	// Start first workflow.
	input, _ := json.Marshal(AnalyzeResult{Delegate: true, Confidence: 0.9, Reason: "First"})
	engine.HandleAnalyze(ctx, "team-1", "sess-1", "Task 1", string(input))

	// Try second workflow — should be blocked.
	input2, _ := json.Marshal(AnalyzeResult{Delegate: true, Confidence: 0.9, Reason: "Second"})
	text, started, err := engine.HandleAnalyze(ctx, "team-1", "sess-1", "Task 2", string(input2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if started {
		t.Error("should not start concurrent workflow")
	}
	if text == "" {
		t.Error("expected guard message")
	}
}

// --- HandlePlan tests ---

func TestHandlePlan_NoActiveWorkflow(t *testing.T) {
	engine, _ := newTestWorkflowEngine(t)
	ctx := context.Background()

	input, _ := json.Marshal(PlanResult{
		Tasks:        []PlanTask{{Subject: "A", Assignee: "researcher", Description: "Do A"}},
		Announcement: "test",
	})
	text, err := engine.HandlePlan(ctx, "team-1", "sess-1", string(input), []string{"researcher"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Error("expected guidance text")
	}
}

func TestHandlePlan_WrongState(t *testing.T) {
	engine, s := newTestWorkflowEngine(t)
	ctx := context.Background()

	// Create workflow in analyze state.
	s.CreateWorkflow(ctx, store.TeamWorkflow{TeamID: "team-1", SessionID: "sess-1"})

	input, _ := json.Marshal(PlanResult{
		Tasks:        []PlanTask{{Subject: "A", Assignee: "researcher", Description: "Do A"}},
		Announcement: "test",
	})
	text, err := engine.HandlePlan(ctx, "team-1", "sess-1", string(input), []string{"researcher"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Error("expected wrong-state message")
	}
}

func TestHandlePlan_ValidationFailure(t *testing.T) {
	engine, s := newTestWorkflowEngine(t)
	ctx := context.Background()

	// Create workflow and advance to plan state.
	id, _ := s.CreateWorkflow(ctx, store.TeamWorkflow{TeamID: "team-1", SessionID: "sess-1"})
	s.UpdateWorkflowState(ctx, id, "plan", 0, nil)

	// Invalid plan — unknown assignee.
	input, _ := json.Marshal(PlanResult{
		Tasks:        []PlanTask{{Subject: "A", Assignee: "unknown", Description: "Do A"}},
		Announcement: "test",
	})
	text, err := engine.HandlePlan(ctx, "team-1", "sess-1", string(input), []string{"researcher"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text == "" {
		t.Error("expected validation error text")
	}

	// Workflow should still be in plan state (retry allowed).
	wf, _ := s.GetWorkflow(ctx, id)
	if wf.State != "plan" {
		t.Errorf("state = %q, want plan", wf.State)
	}
	if wf.Error == "" {
		t.Error("expected error to be recorded")
	}
}

// --- WorkflowToolDefs tests ---

func TestWorkflowToolDefs(t *testing.T) {
	defs := WorkflowToolDefs()
	if len(defs) != 2 {
		t.Fatalf("expected 2 tool defs, got %d", len(defs))
	}
	if defs[0].Name != ToolWorkflowAnalyze {
		t.Errorf("first tool = %q", defs[0].Name)
	}
	if defs[1].Name != ToolWorkflowPlan {
		t.Errorf("second tool = %q", defs[1].Name)
	}

	// Verify schemas are valid JSON.
	for _, d := range defs {
		var schema map[string]any
		if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
			t.Errorf("invalid schema for %s: %v", d.Name, err)
		}
	}
}

func TestIsWorkflowTool(t *testing.T) {
	if !IsWorkflowTool("_workflow_analyze") {
		t.Error("expected true for _workflow_analyze")
	}
	if !IsWorkflowTool("_workflow_plan") {
		t.Error("expected true for _workflow_plan")
	}
	if IsWorkflowTool("team_tasks") {
		t.Error("expected false for team_tasks")
	}
}
