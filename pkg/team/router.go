package team

import (
	"fmt"
	"strings"
)

// MemberProfile holds pre-processed member data for delegation scoring.
type MemberProfile struct {
	AgentID     string
	DisplayName string
	Keywords    []string // Extracted from description + role + expertise.
}

// DelegationHint is the output of AnalyzeDelegation.
type DelegationHint struct {
	Recommendation string        // "delegate", "self", "ask_user"
	Scores         []MemberScore // Sorted by relevance, highest first.
	Reasoning      string        // One-line explanation.
	MultiSkill     bool          // True if request spans multiple member skills.
	Complexity     string        // "trivial", "simple", "complex"
}

// MemberScore is a scored member for a given user message.
type MemberScore struct {
	AgentID     string
	DisplayName string
	Score       float64 // 0.0 - 1.0 normalized.
}

// DelegationConfig holds tunable thresholds.
type DelegationConfig struct {
	DelegateThreshold float64 // Min score to recommend delegation (default: 0.3).
	ComplexThreshold  float64 // Min score for complex tasks (default: 0.2).
	TrivialMaxWords   int     // Max words for trivial classification (default: 15).
}

// DefaultDelegationConfig returns sensible defaults.
func DefaultDelegationConfig() DelegationConfig {
	return DelegationConfig{
		DelegateThreshold: 0.3,
		ComplexThreshold:  0.2,
		TrivialMaxWords:   15,
	}
}

// --- Action verb categories ---

// Each category maps to a skill domain. Used for multi-skill detection.
var actionCategories = map[string][]string{
	"research": {"research", "find", "search", "investigate", "analyze", "compare", "study", "explore", "discover", "look up"},
	"write":    {"write", "draft", "compose", "blog", "article", "story", "essay", "content", "copy", "post"},
	"edit":     {"edit", "proofread", "review", "revise", "fix grammar", "polish", "correct", "rewrite"},
	"code":     {"code", "implement", "build", "develop", "program", "debug", "deploy", "script", "function"},
	"design":   {"design", "wireframe", "mockup", "prototype", "layout", "sketch", "ui", "ux"},
}

// Explicit delegation keywords — user is asking the leader to delegate.
var delegationKeywords = []string{
	"delegate", "assign", "ask them", "have them", "tell them",
	"ask your team", "have your team", "tell your team",
	"let them", "get them to", "pass it to", "hand it to",
}

// --- Stop words ---

var stopWords = map[string]bool{
	"a": true, "an": true, "the": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "shall": true, "can": true, "need": true,
	"to": true, "of": true, "in": true, "for": true, "on": true,
	"with": true, "at": true, "by": true, "from": true, "as": true,
	"into": true, "about": true, "up": true, "out": true, "if": true,
	"or": true, "and": true, "but": true, "not": true, "no": true,
	"so": true, "than": true, "too": true, "very": true, "just": true,
	"that": true, "this": true, "it": true, "its": true, "i": true,
	"me": true, "my": true, "we": true, "our": true, "you": true,
	"your": true, "he": true, "she": true, "they": true, "them": true,
	"what": true, "which": true, "who": true, "when": true, "where": true,
	"how": true, "all": true, "each": true, "some": true, "any": true,
	"there": true, "here": true, "then": true, "also": true, "more": true,
}

// --- Public API ---

// AnalyzeDelegation scores team members against a user message and returns
// a delegation recommendation. Returns nil if profiles is empty.
func AnalyzeDelegation(message string, profiles []MemberProfile, config *DelegationConfig) *DelegationHint {
	if len(profiles) == 0 {
		return nil
	}
	if config == nil {
		cfg := DefaultDelegationConfig()
		config = &cfg
	}

	messageTerms := tokenize(message)
	messageLower := strings.ToLower(message)

	// Check for explicit member name mentions.
	if named := detectNamedMember(messageLower, profiles); named != "" {
		return &DelegationHint{
			Recommendation: "delegate",
			Scores:         []MemberScore{{AgentID: named, DisplayName: displayNameFor(named, profiles), Score: 1.0}},
			Reasoning:      fmt.Sprintf("User explicitly mentioned %s", displayNameFor(named, profiles)),
			Complexity:     "complex",
		}
	}

	// Check for explicit delegation intent.
	if hasExplicitDelegationIntent(messageLower) {
		scores := scoreAllMembers(messageTerms, profiles)
		hint := &DelegationHint{
			Recommendation: "delegate",
			Scores:         scores,
			Reasoning:      "User explicitly requested delegation",
			Complexity:     "complex",
		}
		if len(scores) > 0 && scores[0].Score > 0 {
			hint.Reasoning = fmt.Sprintf("User requested delegation — best match: %s", scores[0].DisplayName)
		}
		return hint
	}

	// Classify complexity.
	complexity, matchedCategories := classifyComplexity(messageTerms, messageLower, profiles, config.TrivialMaxWords)

	// Trivial → always self.
	if complexity == "trivial" {
		return &DelegationHint{
			Recommendation: "self",
			Complexity:     "trivial",
			Reasoning:      "Simple request — handle directly",
		}
	}

	// Score members.
	scores := scoreAllMembers(messageTerms, profiles)
	multiSkill := len(matchedCategories) >= 2

	// Build hint.
	hint := &DelegationHint{
		Scores:     scores,
		MultiSkill: multiSkill,
		Complexity: complexity,
	}

	if len(scores) == 0 {
		hint.Recommendation = "self"
		hint.Reasoning = "No team members matched"
		return hint
	}

	topScore := scores[0].Score
	var secondScore float64
	if len(scores) > 1 {
		secondScore = scores[1].Score
	}

	switch complexity {
	case "simple":
		if topScore < config.DelegateThreshold {
			hint.Recommendation = "self"
			hint.Reasoning = "No strong member match for this request"
		} else if secondScore > 0 && topScore < secondScore*2 {
			// Close scores — ambiguous.
			hint.Recommendation = "self"
			hint.Reasoning = fmt.Sprintf("Ambiguous match — %s and %s score similarly", scores[0].DisplayName, scores[1].DisplayName)
		} else {
			hint.Recommendation = "delegate"
			hint.Reasoning = fmt.Sprintf("Best match: %s (%.0f%%)", scores[0].DisplayName, topScore*100)
		}

	case "complex":
		if multiSkill {
			// Multi-skill always delegates — the request spans multiple member specialties.
			hint.Recommendation = "delegate"
			hint.Reasoning = "Multi-skill request — consider splitting across members"
		} else if topScore < config.ComplexThreshold {
			hint.Recommendation = "self"
			hint.Reasoning = "Complex request but no member has relevant expertise"
		} else {
			hint.Recommendation = "delegate"
			hint.Reasoning = fmt.Sprintf("Complex task — delegate to %s (%.0f%%)", scores[0].DisplayName, topScore*100)
		}
	}

	return hint
}

// FormatDelegationHint produces the [Delegation Analysis] block for
// injection into the team lead's system prompt.
func FormatDelegationHint(hint *DelegationHint) string {
	if hint == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Delegation Analysis]\n")
	sb.WriteString(fmt.Sprintf("Complexity: %s\n", hint.Complexity))

	if len(hint.Scores) > 0 {
		sb.WriteString("Member fitness:\n")
		for _, s := range hint.Scores {
			sb.WriteString(fmt.Sprintf("  - %s (%.0f%%)\n", s.DisplayName, s.Score*100))
		}
	}

	rec := strings.ToUpper(hint.Recommendation)
	sb.WriteString(fmt.Sprintf("Recommendation: %s", rec))
	if hint.Reasoning != "" {
		sb.WriteString(" — " + hint.Reasoning)
	}
	sb.WriteString("\n")

	if hint.MultiSkill {
		sb.WriteString("Multi-skill: yes — consider creating separate tasks per skill.\n")
	}

	return sb.String()
}

// --- Internals ---

// ExtractKeywords tokenizes a description into searchable keywords.
// Applies lowercasing, stop word removal, and simple stemming.
func ExtractKeywords(texts ...string) []string {
	seen := make(map[string]bool)
	var keywords []string
	for _, text := range texts {
		for _, term := range tokenize(text) {
			stemmed := simpleStem(term)
			if !seen[stemmed] {
				seen[stemmed] = true
				keywords = append(keywords, stemmed)
			}
		}
	}
	return keywords
}

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	// Replace punctuation with spaces.
	replacer := strings.NewReplacer(
		",", " ", ".", " ", "!", " ", "?", " ", ";", " ", ":", " ",
		"(", " ", ")", " ", "[", " ", "]", " ", "{", " ", "}", " ",
		"\"", " ", "'", " ", "/", " ", "-", " ", "_", " ",
	)
	cleaned := replacer.Replace(lower)
	words := strings.Fields(cleaned)

	var terms []string
	for _, w := range words {
		if len(w) < 2 {
			continue
		}
		if stopWords[w] {
			continue
		}
		terms = append(terms, w)
	}
	return terms
}

func simpleStem(word string) string {
	// Strip common English suffixes for rough matching.
	for _, suffix := range []string{"tion", "sion", "ing", "ment", "ness", "able", "ible", "ally", "ful", "less", "ous", "ive", "ed", "er", "ly", "es", "s"} {
		if len(word) > len(suffix)+2 && strings.HasSuffix(word, suffix) {
			return word[:len(word)-len(suffix)]
		}
	}
	return word
}

func detectNamedMember(messageLower string, profiles []MemberProfile) string {
	for _, p := range profiles {
		// Check display name (case-insensitive).
		if strings.Contains(messageLower, strings.ToLower(p.DisplayName)) {
			return p.AgentID
		}
		// Check agent ID.
		if strings.Contains(messageLower, strings.ToLower(p.AgentID)) {
			return p.AgentID
		}
	}
	return ""
}

func hasExplicitDelegationIntent(messageLower string) bool {
	for _, kw := range delegationKeywords {
		if strings.Contains(messageLower, kw) {
			return true
		}
	}
	return false
}

// classifyComplexity returns "trivial", "simple", or "complex" and the
// set of matched action categories (for multi-skill detection).
func classifyComplexity(terms []string, messageLower string, profiles []MemberProfile, trivialMaxWords int) (string, map[string]string) {
	// Trivial: short message with no action verbs.
	if len(terms) <= trivialMaxWords {
		hasAction := false
		for _, t := range terms {
			if _, cat := matchActionCategory(t); cat != "" {
				hasAction = true
				break
			}
		}
		if !hasAction {
			return "trivial", nil
		}
	}

	// Detect action categories present in the message.
	// matchedCategories maps category → the member whose keywords best match it.
	matched := make(map[string]string) // category → best member agentID
	for _, t := range terms {
		stemmed := simpleStem(t)
		if _, cat := matchActionCategory(t); cat != "" {
			if matched[cat] == "" {
				// Find which member this category aligns with.
				matched[cat] = bestMemberForCategory(cat, profiles)
			}
		}
		if _, cat := matchActionCategory(stemmed); cat != "" {
			if matched[cat] == "" {
				matched[cat] = bestMemberForCategory(cat, profiles)
			}
		}
	}

	// Multi-skill: 2+ categories matched to DIFFERENT members.
	uniqueMembers := make(map[string]bool)
	for _, memberID := range matched {
		if memberID != "" {
			uniqueMembers[memberID] = true
		}
	}

	if len(matched) >= 2 && len(uniqueMembers) >= 2 {
		return "complex", matched
	}

	// Long messages tend to be complex.
	if len(terms) > 50 {
		return "complex", matched
	}

	if len(matched) >= 2 {
		return "complex", matched
	}

	if len(matched) == 1 {
		return "simple", matched
	}

	// No action verbs but non-trivial length.
	return "simple", nil
}

func matchActionCategory(term string) (string, string) {
	for category, verbs := range actionCategories {
		for _, v := range verbs {
			if term == v || simpleStem(term) == simpleStem(v) {
				return v, category
			}
		}
	}
	return "", ""
}

func bestMemberForCategory(category string, profiles []MemberProfile) string {
	verbs := actionCategories[category]
	if len(verbs) == 0 {
		return ""
	}

	bestID := ""
	bestCount := 0
	for _, p := range profiles {
		count := 0
		for _, kw := range p.Keywords {
			for _, v := range verbs {
				if kw == v || kw == simpleStem(v) || simpleStem(kw) == simpleStem(v) {
					count++
				}
			}
		}
		if count > bestCount {
			bestCount = count
			bestID = p.AgentID
		}
	}
	return bestID
}

func scoreAllMembers(messageTerms []string, profiles []MemberProfile) []MemberScore {
	scores := make([]MemberScore, 0, len(profiles))
	for _, p := range profiles {
		score := scoreMember(messageTerms, p)
		scores = append(scores, MemberScore{
			AgentID:     p.AgentID,
			DisplayName: p.DisplayName,
			Score:       score,
		})
	}
	// Sort descending by score.
	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].Score > scores[i].Score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}
	return scores
}

func scoreMember(messageTerms []string, profile MemberProfile) float64 {
	if len(profile.Keywords) == 0 {
		return 0
	}

	var exactMatches, stemMatches float64
	memberStemmed := make(map[string]bool)
	memberExact := make(map[string]bool)
	for _, kw := range profile.Keywords {
		memberExact[kw] = true
		memberStemmed[simpleStem(kw)] = true
	}

	counted := make(map[string]bool) // Prevent double-counting.
	for _, term := range messageTerms {
		if counted[term] {
			continue
		}
		counted[term] = true
		if memberExact[term] {
			exactMatches++
		} else if memberStemmed[simpleStem(term)] {
			stemMatches++
		}
	}

	// Weighted: exact × 2 + stem × 0.5, normalized by keyword count.
	raw := (exactMatches*2 + stemMatches*0.5) / float64(len(profile.Keywords))
	if raw > 1.0 {
		raw = 1.0
	}
	return raw
}

func displayNameFor(agentID string, profiles []MemberProfile) string {
	for _, p := range profiles {
		if p.AgentID == agentID {
			return p.DisplayName
		}
	}
	return agentID
}
