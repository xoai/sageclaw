package team

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/agentcfg"
	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

const liveDBPath = "/home/xoai/.sageclaw/sageclaw.db"
const liveAgentsDir = "/mnt/e/Codes/SageClaw/bin/agents"

// TestLive_TeamExistsInDB verifies the real DB has the expected team.
func TestLive_TeamExistsInDB(t *testing.T) {
	skipIfNoDB(t)
	s := openLiveDB(t)
	defer s.Close()

	teams, err := listTeamsFromDB(s)
	if err != nil {
		t.Fatalf("ListTeams: %v", err)
	}
	if len(teams) == 0 {
		t.Fatal("no teams in database")
	}

	var found *store.Team
	for i := range teams {
		if teams[i].LeadID == "research-familiar" {
			found = &teams[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected team with lead research-familiar")
	}

	t.Logf("Team: %s (lead: %s)", found.Name, found.LeadID)

	// Verify members.
	if !strings.Contains(found.Config, "editmaster") {
		t.Error("team config should contain editmaster")
	}
	if !strings.Contains(found.Config, "writter") {
		t.Error("team config should contain writter")
	}
}

// TestLive_LeadPromptContainsMemberRouting loads the real agent configs
// and verifies the lead's system prompt routes tasks to the right members.
func TestLive_LeadPromptContainsMemberRouting(t *testing.T) {
	skipIfNoDB(t)
	skipIfNoAgents(t)
	s := openLiveDB(t)
	defer s.Close()

	// Load agent configs from disk.
	configs, err := agentcfg.LoadAll(liveAgentsDir)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}

	leadCfg, ok := configs["research-familiar"]
	if !ok {
		t.Fatal("research-familiar config not found")
	}

	// Simulate team info injection (as main.go does at startup).
	leadCfg.TeamInfo = &agentcfg.TeamInfo{
		TeamID:   "team_research_&_writting",
		TeamName: "Research & Writting",
		Role:     "lead",
		Members: []agentcfg.TeamMemberInfo{
			{AgentID: "research-familiar", DisplayName: "Research Familiar", Role: "lead", Description: "Research Specialist"},
			{AgentID: "editmaster", DisplayName: "EditMaster", Role: "member", Description: "Professional Editor"},
			{AgentID: "writter", DisplayName: "Writter", Role: "member", Description: "Creative Writer & Content Specialist"},
		},
	}

	prompt := agentcfg.AssembleSystemPrompt(leadCfg)

	// Verify member roster is in the prompt.
	if !strings.Contains(prompt, "editmaster") {
		t.Error("lead prompt should contain editmaster agent ID")
	}
	if !strings.Contains(prompt, "writter") {
		t.Error("lead prompt should contain writter agent ID")
	}

	// Verify skills are exposed for routing.
	if !strings.Contains(prompt, "Professional Editor") {
		t.Error("lead prompt should contain EditMaster's specialty")
	}
	if !strings.Contains(prompt, "Creative Writer") {
		t.Error("lead prompt should contain Writter's specialty")
	}

	// Verify delegation guidance (v2: [Delegation Analysis] directive).
	if !strings.Contains(prompt, "Delegation Guidance") {
		t.Error("lead prompt should contain Delegation Guidance section")
	}
	if !strings.Contains(prompt, "DELEGATE") {
		t.Error("lead prompt should contain DELEGATE instruction")
	}

	// Lead should NOT be in the assignable roster.
	if strings.Contains(prompt, "`research-familiar`)") && strings.Contains(prompt, "- **Research Familiar**") {
		idx := strings.Index(prompt, "Your Team Members")
		if idx >= 0 {
			rosterSection := prompt[idx : idx+500]
			if strings.Contains(rosterSection, "`research-familiar`") {
				t.Error("lead should NOT appear as assignable member in roster")
			}
		}
	}

	t.Logf("Lead prompt length: %d chars", len(prompt))
	t.Logf("Delegation Guidance present: %v", strings.Contains(prompt, "Delegation Guidance"))
}

// TestLive_DispatchToMembersSucceeds verifies task dispatch to real team
// members works against the live database.
func TestLive_DispatchToMembersSucceeds(t *testing.T) {
	skipIfNoDB(t)
	s := openLiveDB(t)
	defer s.Close()

	// Find the team.
	teams, _ := listTeamsFromDB(s)
	var teamID string
	for _, tm := range teams {
		if tm.LeadID == "research-familiar" {
			teamID = tm.ID
			break
		}
	}
	if teamID == "" {
		t.Fatal("team not found")
	}

	exec := NewTeamExecutor(s, &mockLoopFactory{}, func(e agent.Event) {})
	ctx := context.Background()

	// Dispatch editing task to editmaster — should succeed.
	editTaskID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:      teamID,
		Title:       "Edit the draft blog post for clarity and grammar",
		Description: "Focus on readability, fix passive voice, ensure consistent tone",
		AssignedTo:  "editmaster",
		CreatedBy:   "research-familiar",
		Priority:    5,
	})
	if err != nil {
		t.Fatalf("dispatch to editmaster failed: %v", err)
	}
	t.Logf("Edit task created: %s", editTaskID)

	// Dispatch writing task to writter — should succeed.
	writeTaskID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:      teamID,
		Title:       "Write a 500-word blog intro about AI agents",
		Description: "Target audience: developers. Tone: conversational but technical.",
		AssignedTo:  "writter",
		CreatedBy:   "research-familiar",
		Priority:    5,
	})
	if err != nil {
		t.Fatalf("dispatch to writter failed: %v", err)
	}
	t.Logf("Write task created: %s", writeTaskID)

	// Dispatch to lead should FAIL.
	_, err = exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Should not work",
		AssignedTo: "research-familiar",
		CreatedBy:  "research-familiar",
	})
	if err == nil {
		t.Fatal("dispatch to lead should fail")
	}
	t.Logf("Lead self-assign correctly rejected: %v", err)

	// Dispatch to unknown agent should FAIL.
	_, err = exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Should not work",
		AssignedTo: "nonexistent-agent",
		CreatedBy:  "research-familiar",
	})
	if err == nil {
		t.Fatal("dispatch to unknown agent should fail")
	}
	t.Logf("Unknown agent correctly rejected: %v", err)

	// Verify tasks in DB.
	editTask, _ := s.GetTask(ctx, editTaskID)
	writeTask, _ := s.GetTask(ctx, writeTaskID)

	if editTask.AssignedTo != "editmaster" {
		t.Errorf("edit task assigned to %q, want editmaster", editTask.AssignedTo)
	}
	if writeTask.AssignedTo != "writter" {
		t.Errorf("write task assigned to %q, want writter", writeTask.AssignedTo)
	}

	// Cleanup: remove test tasks.
	s.DB().ExecContext(ctx, `DELETE FROM team_tasks WHERE id IN (?, ?)`, editTaskID, writeTaskID)
	t.Log("Test tasks cleaned up")
}

// TestLive_DependencyChainWiring verifies blocked_by works against the live DB:
// research → write (blocked by research) → edit (blocked by write).
func TestLive_DependencyChainWiring(t *testing.T) {
	skipIfNoDB(t)
	s := openLiveDB(t)
	defer s.Close()

	teams, _ := listTeamsFromDB(s)
	var teamID string
	for _, tm := range teams {
		if tm.LeadID == "research-familiar" {
			teamID = tm.ID
			break
		}
	}
	if teamID == "" {
		t.Fatal("team not found")
	}

	exec := NewTeamExecutor(s, &mockLoopFactory{}, func(e agent.Event) {})
	ctx := context.Background()

	// Step 1: Research (unblocked, no assignee — lead created it as a plan).
	researchID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:    teamID,
		Title:     "Research fintech trends 2026",
		CreatedBy: "research-familiar",
		BatchID:   "test-chain",
	})
	if err != nil {
		t.Fatalf("research dispatch: %v", err)
	}

	// Step 2: Write, blocked by research.
	writeID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Write article based on research",
		AssignedTo: "writter",
		CreatedBy:  "research-familiar",
		BlockedBy:  researchID,
		BatchID:    "test-chain",
	})
	if err != nil {
		t.Fatalf("write dispatch: %v", err)
	}

	// Step 3: Edit, blocked by write.
	editID, err := exec.Dispatch(ctx, store.TeamTask{
		TeamID:     teamID,
		Title:      "Final edit and polish",
		AssignedTo: "editmaster",
		CreatedBy:  "research-familiar",
		BlockedBy:  writeID,
		BatchID:    "test-chain",
	})
	if err != nil {
		t.Fatalf("edit dispatch: %v", err)
	}

	// Verify statuses.
	researchTask, _ := s.GetTask(ctx, researchID)
	writeTask, _ := s.GetTask(ctx, writeID)
	editTask, _ := s.GetTask(ctx, editID)

	if researchTask.Status != "pending" {
		t.Errorf("research should be pending (no assignee), got %q", researchTask.Status)
	}
	if writeTask.Status != "blocked" {
		t.Errorf("write should be blocked, got %q", writeTask.Status)
	}
	if editTask.Status != "blocked" {
		t.Errorf("edit should be blocked, got %q", editTask.Status)
	}

	t.Logf("Chain: research(%s) → write(%s, blocked) → edit(%s, blocked)", researchID[:8], writeID[:8], editID[:8])

	// Cleanup.
	s.DB().ExecContext(ctx, `DELETE FROM team_tasks WHERE id IN (?, ?, ?)`, researchID, writeID, editID)
	t.Log("Test tasks cleaned up")
}

// --- Helpers ---

func skipIfNoDB(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(liveDBPath); os.IsNotExist(err) {
		t.Skipf("live DB not found at %s", liveDBPath)
	}
}

func skipIfNoAgents(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(liveAgentsDir); os.IsNotExist(err) {
		t.Skipf("agents dir not found at %s", liveAgentsDir)
	}
}

func listTeamsFromDB(s *sqlite.Store) ([]store.Team, error) {
	rows, err := s.DB().Query(`SELECT id, name, lead_id, config, description, settings FROM teams`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var teams []store.Team
	for rows.Next() {
		var t store.Team
		var desc, settings string
		rows.Scan(&t.ID, &t.Name, &t.LeadID, &t.Config, &desc, &settings)
		t.Settings = settings
		teams = append(teams, t)
	}
	return teams, nil
}

func openLiveDB(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.New(liveDBPath)
	if err != nil {
		t.Fatalf("opening live DB: %v", err)
	}
	return s
}

