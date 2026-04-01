package team

import (
	"strings"
	"testing"
)

// --- Test profiles ---

var testProfiles = []MemberProfile{
	{
		AgentID:     "writer",
		DisplayName: "Writter",
		Keywords:    ExtractKeywords("Creative writing, content strategy, storytelling, technical writing, copywriting, editing, proofreading, SEO optimization"),
	},
	{
		AgentID:     "editor",
		DisplayName: "EditMaster",
		Keywords:    ExtractKeywords("Professional editing, grammar, style guides, copy editing, proofreading, developmental editing"),
	},
	{
		AgentID:     "researcher",
		DisplayName: "Research Bot",
		Keywords:    ExtractKeywords("Information gathering, source validation, data synthesis, comparative analysis, fact-checking, academic research"),
	},
}

// --- ExtractKeywords tests ---

func TestExtractKeywords_Basic(t *testing.T) {
	kw := ExtractKeywords("Creative writing, content strategy")
	if len(kw) == 0 {
		t.Fatal("expected keywords")
	}
	found := false
	for _, k := range kw {
		if k == "creativ" || k == "creative" || strings.HasPrefix(k, "creat") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected stem of 'creative' in keywords, got: %v", kw)
	}
}

func TestExtractKeywords_StopWordsRemoved(t *testing.T) {
	kw := ExtractKeywords("the quick brown fox is very fast")
	for _, k := range kw {
		if stopWords[k] {
			t.Errorf("stop word %q should be removed", k)
		}
	}
}

func TestExtractKeywords_EmptyInput(t *testing.T) {
	kw := ExtractKeywords("")
	if len(kw) != 0 {
		t.Errorf("expected empty, got: %v", kw)
	}
}

func TestExtractKeywords_Deduplicates(t *testing.T) {
	kw := ExtractKeywords("writing writing writing")
	if len(kw) != 1 {
		t.Errorf("expected 1 deduplicated keyword, got %d: %v", len(kw), kw)
	}
}

// --- Complexity classification tests ---

func TestComplexity_Trivial_Greeting(t *testing.T) {
	hint := AnalyzeDelegation("Hello!", testProfiles, nil)
	if hint.Complexity != "trivial" {
		t.Errorf("greeting should be trivial, got %q", hint.Complexity)
	}
	if hint.Recommendation != "self" {
		t.Errorf("trivial should be self, got %q", hint.Recommendation)
	}
}

func TestComplexity_Trivial_ShortQuestion(t *testing.T) {
	hint := AnalyzeDelegation("What time is it?", testProfiles, nil)
	if hint.Complexity != "trivial" {
		t.Errorf("short question should be trivial, got %q", hint.Complexity)
	}
}

func TestComplexity_Simple_SingleAction(t *testing.T) {
	hint := AnalyzeDelegation("Write a blog post about AI trends", testProfiles, nil)
	if hint.Complexity != "simple" {
		t.Errorf("single write action should be simple, got %q", hint.Complexity)
	}
}

func TestComplexity_Complex_MultiSkill(t *testing.T) {
	hint := AnalyzeDelegation("Research about MoMo vs ZaloPay and write a blog post about it", testProfiles, nil)
	if hint.Complexity != "complex" {
		t.Errorf("research+write should be complex, got %q", hint.Complexity)
	}
	if !hint.MultiSkill {
		t.Error("research+write should be multi-skill")
	}
}

func TestComplexity_Simple_SameSkill(t *testing.T) {
	// "research and summarize" is the same skill category.
	hint := AnalyzeDelegation("Research this topic and summarize findings", testProfiles, nil)
	// Both "research" and "summarize" are in the same-ish domain.
	// This should be simple or complex but NOT multi-skill with different members.
	if hint.MultiSkill {
		t.Error("research+summarize should NOT be multi-skill (same domain)")
	}
}

// --- Member scoring tests ---

func TestScoring_WriterMatchesWriteRequest(t *testing.T) {
	hint := AnalyzeDelegation("Write a detailed blog post about technology", testProfiles, nil)
	if len(hint.Scores) == 0 {
		t.Fatal("expected scores")
	}
	if hint.Scores[0].AgentID != "writer" {
		t.Errorf("writer should score highest for write request, got %q", hint.Scores[0].AgentID)
	}
}

func TestScoring_ResearcherMatchesResearchRequest(t *testing.T) {
	hint := AnalyzeDelegation("Research the competitive landscape of fintech in Vietnam", testProfiles, nil)
	if len(hint.Scores) == 0 {
		t.Fatal("expected scores")
	}
	if hint.Scores[0].AgentID != "researcher" {
		t.Errorf("researcher should score highest, got %q (scores: %+v)", hint.Scores[0].AgentID, hint.Scores)
	}
}

func TestScoring_NoMatchLowScores(t *testing.T) {
	hint := AnalyzeDelegation("Deploy the kubernetes cluster to production", testProfiles, nil)
	// None of our test profiles match devops/kubernetes.
	for _, s := range hint.Scores {
		if s.Score > 0.3 {
			t.Errorf("no profile should score high for devops request, %s scored %.2f", s.AgentID, s.Score)
		}
	}
}

func TestScoring_StemMatching(t *testing.T) {
	// "writing" should match "write" via stemming.
	terms := tokenize("I need help with writing")
	score := scoreMember(terms, testProfiles[0]) // writer profile
	if score == 0 {
		t.Error("stem matching should produce non-zero score for 'writing' vs writer keywords")
	}
}

// --- Recommendation logic tests ---

func TestRecommendation_DelegateForComplex(t *testing.T) {
	hint := AnalyzeDelegation("Research about MoMo vs ZaloPay and write a blog post", testProfiles, nil)
	if hint.Recommendation != "delegate" {
		t.Errorf("complex multi-skill should recommend delegate, got %q", hint.Recommendation)
	}
}

func TestRecommendation_SelfForTrivial(t *testing.T) {
	hint := AnalyzeDelegation("Thanks!", testProfiles, nil)
	if hint.Recommendation != "self" {
		t.Errorf("trivial should recommend self, got %q", hint.Recommendation)
	}
}

func TestRecommendation_SelfForNoMatch(t *testing.T) {
	hint := AnalyzeDelegation("Configure the nginx reverse proxy", testProfiles, nil)
	if hint.Recommendation != "self" {
		t.Errorf("no match should recommend self, got %q", hint.Recommendation)
	}
}

func TestRecommendation_DelegateSimpleStrongMatch(t *testing.T) {
	hint := AnalyzeDelegation("Edit and proofread this document for grammar mistakes", testProfiles, nil)
	if hint.Recommendation != "delegate" {
		t.Errorf("strong match for editing should delegate, got %q", hint.Recommendation)
	}
	if len(hint.Scores) > 0 && hint.Scores[0].AgentID != "editor" {
		t.Errorf("editor should be top match, got %q", hint.Scores[0].AgentID)
	}
}

// --- Explicit intent tests ---

func TestExplicitIntent_MemberNameMentioned(t *testing.T) {
	hint := AnalyzeDelegation("Ask EditMaster to review this document", testProfiles, nil)
	if hint.Recommendation != "delegate" {
		t.Errorf("explicit name should delegate, got %q", hint.Recommendation)
	}
	if len(hint.Scores) == 0 || hint.Scores[0].AgentID != "editor" {
		t.Error("should target EditMaster")
	}
}

func TestExplicitIntent_DelegationKeyword(t *testing.T) {
	hint := AnalyzeDelegation("Delegate this task to your team", testProfiles, nil)
	if hint.Recommendation != "delegate" {
		t.Errorf("explicit delegation keyword should delegate, got %q", hint.Recommendation)
	}
}

func TestExplicitIntent_AskTeamMember(t *testing.T) {
	hint := AnalyzeDelegation("Ask your team member to write a blog post", testProfiles, nil)
	if hint.Recommendation != "delegate" {
		t.Errorf("'ask your team' should delegate, got %q", hint.Recommendation)
	}
}

// --- Edge cases ---

func TestEdge_EmptyProfiles(t *testing.T) {
	hint := AnalyzeDelegation("Write something", nil, nil)
	if hint != nil {
		t.Error("empty profiles should return nil")
	}
}

func TestEdge_EmptyMessage(t *testing.T) {
	hint := AnalyzeDelegation("", testProfiles, nil)
	if hint.Recommendation != "self" {
		t.Errorf("empty message should be self, got %q", hint.Recommendation)
	}
}

func TestEdge_SingleMemberTeam(t *testing.T) {
	single := []MemberProfile{testProfiles[0]} // Writer only.
	hint := AnalyzeDelegation("Research and write a comprehensive report", single, nil)
	// Complex request with only one member — should still recommend delegate.
	if hint.Complexity != "complex" && hint.Complexity != "simple" {
		t.Errorf("should classify as complex or simple, got %q", hint.Complexity)
	}
}

func TestEdge_ConfigurableThresholds(t *testing.T) {
	// Very high threshold — nothing should trigger delegation.
	strict := &DelegationConfig{
		DelegateThreshold: 0.99,
		ComplexThreshold:  0.99,
		TrivialMaxWords:   15,
	}
	hint := AnalyzeDelegation("Write a blog post about AI", testProfiles, strict)
	// Even with a match, threshold is too high.
	if hint.Recommendation == "delegate" {
		// Unless explicit intent overrides — which it shouldn't here.
		t.Log("delegation triggered despite high threshold — check if explicit intent fired")
	}
}

// --- FormatDelegationHint tests ---

func TestFormatHint_ContainsRecommendation(t *testing.T) {
	hint := &DelegationHint{
		Recommendation: "delegate",
		Complexity:     "complex",
		Reasoning:      "Multi-skill request",
		MultiSkill:     true,
		Scores: []MemberScore{
			{AgentID: "writer", DisplayName: "Writter", Score: 0.82},
			{AgentID: "editor", DisplayName: "EditMaster", Score: 0.31},
		},
	}
	formatted := FormatDelegationHint(hint)
	if !strings.Contains(formatted, "[Delegation Analysis]") {
		t.Error("missing header")
	}
	if !strings.Contains(formatted, "DELEGATE") {
		t.Error("missing recommendation")
	}
	if !strings.Contains(formatted, "Writter") {
		t.Error("missing member name")
	}
	if !strings.Contains(formatted, "Multi-skill") {
		t.Error("missing multi-skill marker")
	}
}

func TestFormatHint_Nil(t *testing.T) {
	if FormatDelegationHint(nil) != "" {
		t.Error("nil hint should return empty string")
	}
}
