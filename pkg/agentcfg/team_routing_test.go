package agentcfg

import (
	"strings"
	"testing"
)

// TestTeamLeadPrompt_IncludesRoutingGuidance verifies the lead's system prompt
// contains the 3-layer routing table that directs delegation by member expertise.
func TestTeamLeadPrompt_IncludesRoutingGuidance(t *testing.T) {
	cfg := &AgentConfig{
		ID: "lead-agent",
		Identity: Identity{
			Name:  "Team Lead",
			Role:  "Coordinates research and writing tasks",
			Model: "strong",
		},
		TeamInfo: &TeamInfo{
			TeamID:   "team-alpha",
			TeamName: "Alpha Squad",
			Role:     "lead",
			Members: []TeamMemberInfo{
				{AgentID: "lead-agent", DisplayName: "Team Lead", Role: "lead", Description: "Coordinates the team"},
				{AgentID: "researcher", DisplayName: "Researcher", Role: "member", Description: "Deep research, fact-checking, data analysis"},
				{AgentID: "writer", DisplayName: "Writer", Role: "member", Description: "Technical writing, blog posts, documentation"},
				{AgentID: "coder", DisplayName: "Coder", Role: "member", Description: "Code implementation, debugging, testing"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	// Layer 1: keyword routing should include member descriptions.
	if !strings.Contains(prompt, "researcher") {
		t.Error("prompt should contain researcher agent ID for routing")
	}
	if !strings.Contains(prompt, "Deep research") {
		t.Error("prompt should contain researcher's description for keyword matching")
	}

	// Delegation guidance (replaced 3-layer routing with [Delegation Analysis] directive).
	if !strings.Contains(prompt, "Delegation Guidance") {
		t.Error("prompt should contain Delegation Guidance section")
	}
	if !strings.Contains(prompt, "[Delegation Analysis]") {
		t.Error("prompt should reference [Delegation Analysis] block")
	}
	if !strings.Contains(prompt, "DELEGATE") {
		t.Error("prompt should contain DELEGATE instruction")
	}

	// Member roster.
	if !strings.Contains(prompt, "Your Team Members") {
		t.Error("prompt should contain member roster section")
	}

	// Lead should NOT appear as assignable member.
	lines := strings.Split(prompt, "\n")
	for _, line := range lines {
		if strings.Contains(line, "Your Team Members") {
			continue
		}
		if strings.Contains(line, "`lead-agent`") && strings.Contains(line, "- **") {
			t.Error("lead agent should NOT appear in assignable member roster")
		}
	}
}

// TestTeamLeadPrompt_MemberSkillsInRoster verifies each member's specialty
// description appears in the roster for skill-based task assignment.
func TestTeamLeadPrompt_MemberSkillsInRoster(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "lead",
		Identity: Identity{Name: "Lead", Model: "strong"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Dev Team",
			Role:     "lead",
			Members: []TeamMemberInfo{
				{AgentID: "lead", DisplayName: "Lead", Role: "lead"},
				{AgentID: "frontend-dev", DisplayName: "Frontend Dev", Role: "member", Description: "React, CSS, UI components, accessibility"},
				{AgentID: "backend-dev", DisplayName: "Backend Dev", Role: "member", Description: "Go, APIs, database, infrastructure"},
				{AgentID: "qa-engineer", DisplayName: "QA Engineer", Role: "member", Description: "Testing, test automation, bug reproduction"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	// Each member's skills should be in the prompt for the LLM to match against.
	expectations := map[string]string{
		"frontend-dev": "React, CSS, UI components",
		"backend-dev":  "Go, APIs, database",
		"qa-engineer":  "Testing, test automation",
	}
	for agentID, skill := range expectations {
		if !strings.Contains(prompt, agentID) {
			t.Errorf("prompt missing agent ID %q", agentID)
		}
		if !strings.Contains(prompt, skill) {
			t.Errorf("prompt missing skill description %q for %s", skill, agentID)
		}
	}
}

// TestTeamLeadPrompt_BatchCreationGuidance verifies the prompt instructs
// the lead to create all tasks upfront rather than create-wait-create.
func TestTeamLeadPrompt_BatchCreationGuidance(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "lead",
		Identity: Identity{Name: "Lead", Model: "strong"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Team",
			Role:     "lead",
			Members: []TeamMemberInfo{
				{AgentID: "lead", DisplayName: "Lead", Role: "lead"},
				{AgentID: "worker", DisplayName: "Worker", Role: "member", Description: "General tasks"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	if !strings.Contains(prompt, "Create ALL tasks upfront") || !strings.Contains(prompt, "batch") {
		t.Error("prompt should instruct batch task creation")
	}
	if !strings.Contains(prompt, "Anti-pattern") || !strings.Contains(prompt, "WRONG") {
		t.Error("prompt should warn against create-wait-create anti-pattern")
	}
}

// TestTeamLeadPrompt_NoMembersNoTeamSection verifies that a lead with
// no members gets no team section (nothing to delegate to).
func TestTeamLeadPrompt_NoMembersNoTeamSection(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "solo-lead",
		Identity: Identity{Name: "Solo", Model: "strong"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-solo",
			TeamName: "Solo Team",
			Role:     "lead",
			Members: []TeamMemberInfo{
				{AgentID: "solo-lead", DisplayName: "Solo", Role: "lead"},
			},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	if strings.Contains(prompt, "Your Team Members") {
		t.Error("solo lead should NOT have a team members section")
	}
	if strings.Contains(prompt, "Layer 1") {
		t.Error("solo lead should NOT have routing guidance")
	}
}

// TestTeamMemberPrompt_NoRoutingGuidance verifies members don't get
// the delegation/routing prompt — only leads decide task assignment.
func TestTeamMemberPrompt_NoRoutingGuidance(t *testing.T) {
	cfg := &AgentConfig{
		ID:       "researcher",
		Identity: Identity{Name: "Researcher", Model: "fast"},
		TeamInfo: &TeamInfo{
			TeamID:   "team-1",
			TeamName: "Team",
			Role:     "member",
			LeadName: "Lead",
			Members:  []TeamMemberInfo{},
		},
	}

	prompt := AssembleSystemPrompt(cfg)

	if strings.Contains(prompt, "Layer 1") {
		t.Error("member should NOT have routing guidance")
	}
	if strings.Contains(prompt, "When to Delegate") {
		t.Error("member should NOT have delegation guidance")
	}
	if !strings.Contains(prompt, "member of") {
		t.Error("member should have member role context")
	}
}

// TestExtractRoutingKeywords verifies keyword extraction from descriptions.
func TestExtractRoutingKeywords(t *testing.T) {
	tests := []struct {
		desc     string
		name     string
		wantNon  bool   // expect non-empty result
		contains string // substring expected in result
	}{
		{"Deep research and analysis", "Researcher", true, "Deep research and analysis"},
		{"", "Writer", true, "writer-related tasks"}, // fallback to name
		{"Code, debugging, testing", "Dev", true, "Code, debugging, testing"},
	}

	for _, tt := range tests {
		result := extractRoutingKeywords(tt.desc, tt.name)
		if tt.wantNon && result == "" {
			t.Errorf("extractRoutingKeywords(%q, %q) = empty, want non-empty", tt.desc, tt.name)
		}
		if tt.contains != "" && !strings.Contains(result, tt.contains) {
			t.Errorf("extractRoutingKeywords(%q, %q) = %q, missing %q", tt.desc, tt.name, result, tt.contains)
		}
	}
}
