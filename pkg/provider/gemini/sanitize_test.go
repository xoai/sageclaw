package gemini

import (
	"strings"
	"testing"
)

// TestSanitize_ExactBugScenario reproduces the exact error from production:
// content[0] role=model parts=[functionCall(memory_search)]
// content[1] role=user parts=[functionResponse(memory_search)]
// content[2] role=model parts=[functionCall(web_fetch)]
// content[3] role=user parts=[functionResponse(web_fetch) text(user message)]
//
// Two violations: (1) starts with model, (2) mixed functionResponse + text.
func TestSanitize_ExactBugScenario(t *testing.T) {
	contents := []geminiContent{
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "memory_search", Args: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "memory_search", Response: map[string]any{"result": "no memories"}}},
		}},
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "web_fetch", Args: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "web_fetch", Response: map[string]any{"result": "page content"}}},
			{Text: "Nghiên cứu và so sánh mom..."},
		}},
	}

	result := sanitizeGeminiContents(contents)

	// Must start with user turn.
	if result[0].Role != "user" {
		t.Fatalf("first turn must be user, got %s", result[0].Role)
	}

	// No user turn should mix functionResponse with text.
	for i, turn := range result {
		if turn.Role != "user" {
			continue
		}
		hasFR := false
		hasText := false
		for _, p := range turn.Parts {
			if p.FunctionResponse != nil {
				hasFR = true
			}
			if p.Text != "" {
				hasText = true
			}
		}
		if hasFR && hasText {
			t.Errorf("content[%d]: user turn mixes functionResponse with text", i)
		}
	}

	// Every functionCall must be immediately followed by functionResponse.
	for i, turn := range result {
		if !hasFuncCall(turn) {
			continue
		}
		if i+1 >= len(result) {
			t.Errorf("content[%d]: functionCall at end with no response", i)
			continue
		}
		if !hasFuncResponse(result[i+1]) {
			t.Errorf("content[%d]: functionCall not followed by functionResponse (next is %s with parts %d)", i, result[i+1].Role, len(result[i+1].Parts))
		}
	}

	// The user's text message should still be present somewhere.
	found := false
	for _, turn := range result {
		for _, p := range turn.Parts {
			if strings.Contains(p.Text, "Nghiên cứu") {
				found = true
			}
		}
	}
	if !found {
		t.Error("user's text message was lost")
	}
}

func TestSanitize_StartsWithModel(t *testing.T) {
	contents := []geminiContent{
		{Role: "model", Parts: []geminiPart{{Text: "hello"}}},
		{Role: "user", Parts: []geminiPart{{Text: "hi"}}},
	}

	result := sanitizeGeminiContents(contents)

	if result[0].Role != "user" {
		t.Fatalf("must start with user, got %s", result[0].Role)
	}
}

func TestSanitize_MixedFuncResponseAndText(t *testing.T) {
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "search for X"}}},
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "search", Args: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "search", Response: map[string]any{"result": "data"}}},
			{Text: "also tell me about Y"},
		}},
	}

	result := sanitizeGeminiContents(contents)

	// The functionResponse and text should be in separate turns.
	for i, turn := range result {
		if turn.Role != "user" {
			continue
		}
		hasFR := hasFuncResponse(turn)
		hasText := false
		for _, p := range turn.Parts {
			if p.Text != "" {
				hasText = true
			}
		}
		if hasFR && hasText {
			t.Errorf("content[%d]: mixed functionResponse + text", i)
		}
	}
}

func TestSanitize_NonThinkingModelToolCalls_Stripped(t *testing.T) {
	// Gemini requires thought_signature on all functionCall parts.
	// Tool calls from non-thinking models (no signature) must be stripped
	// along with their paired functionResponse, preserving text.
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hello"}}},
		{Role: "model", Parts: []geminiPart{
			{Text: "Let me check."},
			{FunctionCall: &geminiFunctionCall{Name: "web_fetch", Args: map[string]any{"url": "https://example.com"}}},
			// No ThoughtSignature — must be stripped.
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "web_fetch", Response: map[string]any{"result": "page content"}}},
		}},
		{Role: "model", Parts: []geminiPart{{Text: "Here's what I found."}}},
		{Role: "user", Parts: []geminiPart{{Text: "thanks"}}},
	}

	result := sanitizeGeminiContents(contents)

	// functionCall + functionResponse should be stripped.
	for _, turn := range result {
		if hasFuncCall(turn) {
			t.Error("functionCall without thoughtSignature should be stripped")
		}
		if hasFuncResponse(turn) {
			t.Error("paired functionResponse should be stripped")
		}
	}

	// Text should be preserved: "hello", "Let me check.", "Here's what I found.", "thanks"
	allText := ""
	for _, turn := range result {
		for _, p := range turn.Parts {
			allText += p.Text + "|"
		}
	}
	if !strings.Contains(allText, "Let me check.") {
		t.Error("model text should be preserved after stripping functionCall")
	}
	if !strings.Contains(allText, "thanks") {
		t.Error("user text should be preserved")
	}
}

func TestSanitize_ThoughtSignaturePreserved(t *testing.T) {
	// Tool calls WITH thought_signature should be kept.
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hello"}}},
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "search", Args: map[string]any{}},
				ThoughtSignature: "sig123"},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "search", Response: map[string]any{}}},
		}},
		{Role: "model", Parts: []geminiPart{{Text: "done"}}},
	}

	result := sanitizeGeminiContents(contents)

	if len(result) != 4 {
		t.Fatalf("expected 4 turns, got %d", len(result))
	}
	if !hasFuncCall(result[1]) {
		t.Error("functionCall with thoughtSignature should be preserved")
	}
	if result[1].Parts[0].ThoughtSignature != "sig123" {
		t.Error("thoughtSignature value should be preserved")
	}
}

func TestSanitize_MissingSignatureAtPosition(t *testing.T) {
	// Reproduces the exact error: "function call is missing thought_signature, position 7"
	// This happens when iteration 0 produces a tool call (no signature from streaming),
	// and iteration 1 sends it back in history.
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "show history"}}},
		{Role: "model", Parts: []geminiPart{
			{Text: "I'll look up your sessions."},
			{FunctionCall: &geminiFunctionCall{Name: "sessions_history", Args: map[string]any{}}},
			// No ThoughtSignature — this is from the current model's own response
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "sessions_history", Response: map[string]any{"result": "session data"}}},
		}},
	}

	result := sanitizeGeminiContents(contents)

	// The unsigned function call pair must be stripped.
	for _, turn := range result {
		if hasFuncCall(turn) {
			t.Error("functionCall without signature should be stripped")
		}
		if hasFuncResponse(turn) {
			t.Error("paired functionResponse should be stripped")
		}
	}

	// But the text should survive.
	found := false
	for _, turn := range result {
		for _, p := range turn.Parts {
			if strings.Contains(p.Text, "sessions") {
				found = true
			}
		}
	}
	if !found {
		t.Error("text from model turn should be preserved")
	}
}

func TestSanitize_UnpairedFunctionCall(t *testing.T) {
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hello"}}},
		{Role: "model", Parts: []geminiPart{
			{Text: "Let me check."},
			{FunctionCall: &geminiFunctionCall{Name: "web_fetch", Args: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{{Text: "never mind"}}},
	}

	result := sanitizeGeminiContents(contents)

	for _, turn := range result {
		if hasFuncCall(turn) {
			t.Error("unpaired functionCall should have been stripped")
		}
	}
}

func TestSanitize_OrphanedFunctionResponse(t *testing.T) {
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "hello"}}},
		{Role: "model", Parts: []geminiPart{{Text: "hi there"}}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "web_fetch", Response: map[string]any{"result": "stale"}}},
		}},
		{Role: "user", Parts: []geminiPart{{Text: "what's up?"}}},
	}

	result := sanitizeGeminiContents(contents)

	for _, turn := range result {
		if hasFuncResponse(turn) {
			t.Error("orphaned functionResponse should have been stripped")
		}
	}
}

func TestSanitize_MergeDoesNotMixFuncResponse(t *testing.T) {
	// Two consecutive user turns: one with functionResponse, one with text.
	// They should NOT be merged (would create mixed turn).
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "search"}}},
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "search", Args: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "search", Response: map[string]any{}}},
		}},
		{Role: "user", Parts: []geminiPart{{Text: "thanks"}}},
	}

	result := sanitizeGeminiContents(contents)

	// The functionResponse user turn and the text user turn must stay separate.
	for _, turn := range result {
		if turn.Role != "user" {
			continue
		}
		if hasFuncResponse(turn) {
			for _, p := range turn.Parts {
				if p.Text != "" {
					t.Error("merge created mixed functionResponse + text turn")
				}
			}
		}
	}
}

func TestSanitize_MultipleToolCalls(t *testing.T) {
	// Multiple tool calls WITH thought signatures should be preserved.
	contents := []geminiContent{
		{Role: "user", Parts: []geminiPart{{Text: "search both"}}},
		{Role: "model", Parts: []geminiPart{
			{FunctionCall: &geminiFunctionCall{Name: "search_a", Args: map[string]any{}}, ThoughtSignature: "sig1"},
			{FunctionCall: &geminiFunctionCall{Name: "search_b", Args: map[string]any{}}, ThoughtSignature: "sig2"},
		}},
		{Role: "user", Parts: []geminiPart{
			{FunctionResponse: &geminiFuncResponse{Name: "search_a", Response: map[string]any{}}},
			{FunctionResponse: &geminiFuncResponse{Name: "search_b", Response: map[string]any{}}},
		}},
		{Role: "model", Parts: []geminiPart{{Text: "results"}}},
	}

	result := sanitizeGeminiContents(contents)

	if len(result) != 4 {
		t.Fatalf("expected 4 turns, got %d", len(result))
	}
}
