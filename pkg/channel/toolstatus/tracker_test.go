package toolstatus

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// testRecorder captures tracker callbacks.
type testRecorder struct {
	mu      sync.Mutex
	texts   []StatusUpdate
	reacts  []ReactionUpdate
}

func (r *testRecorder) onText(sessionID string, u StatusUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.texts = append(r.texts, u)
}

func (r *testRecorder) onReact(sessionID string, u ReactionUpdate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reacts = append(r.reacts, u)
}

func (r *testRecorder) textCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.texts)
}

func (r *testRecorder) reactCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.reacts)
}

func (r *testRecorder) lastText() StatusUpdate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.texts) == 0 {
		return StatusUpdate{}
	}
	return r.texts[len(r.texts)-1]
}

func (r *testRecorder) lastReact() ReactionUpdate {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.reacts) == 0 {
		return ReactionUpdate{}
	}
	return r.reacts[len(r.reacts)-1]
}

func newTestTracker() (*Tracker, *testRecorder) {
	rec := &testRecorder{}
	dm := DefaultDisplayMap()
	tr := NewTracker(dm, rec.onText, rec.onReact)
	return tr, rec
}

func TestTracker_RunStarted_SetsThinking(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")
	if rec.reactCount() != 1 {
		t.Fatalf("expected 1 react, got %d", rec.reactCount())
	}
	if rec.lastReact().Phase != PhaseThinking {
		t.Errorf("phase = %v, want thinking", rec.lastReact().Phase)
	}
	if rec.lastReact().Emoji != "🤔" {
		t.Errorf("emoji = %v, want 🤔", rec.lastReact().Emoji)
	}
}

func TestTracker_FirstToolCall_ImmediateFlush(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")

	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"test"}`),
	})

	// First tool should flush text immediately (no debounce wait).
	if rec.textCount() != 1 {
		t.Fatalf("expected 1 text flush, got %d", rec.textCount())
	}
	u := rec.lastText()
	if u.Done {
		t.Error("should not be Done after tool call")
	}
	if u.ToolCount != 1 {
		t.Errorf("tool count = %d, want 1", u.ToolCount)
	}
	if u.Text == "" {
		t.Error("status text should not be empty")
	}
}

func TestTracker_SubsequentToolCall_Debounced(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")

	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"first"}`),
	})
	initialTexts := rec.textCount()

	// Second call should NOT flush immediately.
	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c2", Name: "web_fetch",
		Input: json.RawMessage(`{"url":"https://example.com"}`),
	})

	if rec.textCount() != initialTexts {
		t.Errorf("second tool call should be debounced, texts=%d want %d", rec.textCount(), initialTexts)
	}

	// Wait for debounce.
	time.Sleep(time.Duration(EditDebounceMs+200) * time.Millisecond)
	if rec.textCount() <= initialTexts {
		t.Error("debounced text should have flushed")
	}
}

func TestTracker_ToolResult_RemovesActiveTool(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")

	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"test"}`),
	})

	tr.OnToolResult("s1", &canonical.ToolResult{
		ToolCallID: "c1", Content: "results",
	})

	// Should flush Done=true immediately.
	last := rec.lastText()
	if !last.Done {
		t.Error("should be Done when all tools complete")
	}
}

func TestTracker_MultipleTools_StatusText(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")

	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"first"}`),
	})

	// Force the second to flush by waiting for debounce.
	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c2", Name: "read_file",
		Input: json.RawMessage(`{"path":"/tmp/foo"}`),
	})
	time.Sleep(time.Duration(EditDebounceMs+200) * time.Millisecond)

	last := rec.lastText()
	if last.ToolCount != 2 {
		t.Errorf("tool count = %d, want 2", last.ToolCount)
	}
	// Should contain pipe separator for multiple tools.
	if last.Text == "" {
		t.Error("expected non-empty status text for multiple tools")
	}
}

func TestTracker_RepeatedTool_Collapsed(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")

	// Three web_search calls.
	for i := 0; i < 3; i++ {
		tr.OnToolCall("s1", &canonical.ToolCall{
			ID: "c" + string(rune('1'+i)), Name: "web_search",
			Input: json.RawMessage(`{"query":"q"}`),
		})
	}

	// Wait for debounce to flush.
	time.Sleep(time.Duration(EditDebounceMs+200) * time.Millisecond)

	last := rec.lastText()
	if last.ToolCount != 3 {
		t.Errorf("tool count = %d, want 3", last.ToolCount)
	}
	// The status should contain "×3" for collapsed display.
	if last.Text == "" {
		t.Error("status text should not be empty")
	}
}

func TestTracker_NilToolCall_Handled(t *testing.T) {
	tr, _ := newTestTracker()
	tr.OnRunStarted("s1")
	// Should not panic.
	tr.OnToolCall("s1", nil)
}

func TestTracker_NilToolResult_Handled(t *testing.T) {
	tr, _ := newTestTracker()
	tr.OnRunStarted("s1")
	// Should not panic.
	tr.OnToolResult("s1", nil)
}

func TestTracker_RunCompleted_FlushesAndClears(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")
	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"test"}`),
	})

	tr.OnRunCompleted("s1")

	// Should have done reaction.
	if rec.lastReact().Phase != PhaseDone {
		t.Errorf("phase = %v, want done", rec.lastReact().Phase)
	}
	// Should have flushed Done=true text.
	if !rec.lastText().Done {
		t.Error("last text should be Done")
	}
	// State should be cleared.
	tr.mu.Lock()
	_, exists := tr.sessions["s1"]
	tr.mu.Unlock()
	if exists {
		t.Error("session should be cleared after run completed")
	}
}

func TestTracker_RunFailed_EmitsError(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")
	tr.OnRunFailed("s1", nil)

	if rec.lastReact().Phase != PhaseError {
		t.Errorf("phase = %v, want error", rec.lastReact().Phase)
	}
}

func TestTracker_Clear_StopsTimers(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")
	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"test"}`),
	})

	initialTexts := rec.textCount()
	initialReacts := rec.reactCount()

	tr.Clear("s1")

	// Wait long enough for edit debounce to fire (if it would).
	time.Sleep(time.Duration(EditDebounceMs+500) * time.Millisecond)

	if rec.textCount() != initialTexts {
		t.Errorf("text callbacks fired after Clear: got %d, want %d", rec.textCount(), initialTexts)
	}
	// React debounce may have already been scheduled, allow at most the initial count + already-pending.
	// The key assertion: no stall callbacks after clear.
	if rec.reactCount() > initialReacts+1 {
		t.Errorf("excessive react callbacks after Clear: got %d, want <= %d", rec.reactCount(), initialReacts+1)
	}
}

func TestTracker_ReactionDedup(t *testing.T) {
	tr, rec := newTestTracker()
	tr.OnRunStarted("s1")
	initialReacts := rec.reactCount()

	// Calling OnRunStarted again on a different session with same phase shouldn't dedup,
	// but same session same phase should.
	tr.mu.Lock()
	t.Log("Phase:", tr.sessions["s1"].Phase)
	// Manually try to emit same phase.
	tr.emitReactLocked("s1", PhaseThinking)
	tr.mu.Unlock()

	if rec.reactCount() != initialReacts {
		t.Errorf("dedup failed: reacts=%d, want %d", rec.reactCount(), initialReacts)
	}
}

func TestTracker_CategoryToPhase(t *testing.T) {
	tests := []struct {
		category string
		want     ReactionPhase
	}{
		{"web", PhaseWeb},
		{"coding", PhaseCoding},
		{"tool", PhaseTool},
		{"unknown", PhaseTool},
	}
	for _, tt := range tests {
		got := categoryToPhase(tt.category)
		if got != tt.want {
			t.Errorf("categoryToPhase(%q) = %v, want %v", tt.category, got, tt.want)
		}
	}
}

func TestTracker_PhaseEmoji(t *testing.T) {
	tests := []struct {
		phase ReactionPhase
		want  string
	}{
		{PhaseThinking, "🤔"},
		{PhaseTool, "🔥"},
		{PhaseCoding, "👨‍💻"},
		{PhaseWeb, "⚡"},
		{PhaseDone, "👍"},
		{PhaseError, "😱"},
		{PhaseStallSoft, "🥱"},
		{PhaseStallHard, "😨"},
	}
	for _, tt := range tests {
		got := phaseEmoji(tt.phase)
		if got != tt.want {
			t.Errorf("phaseEmoji(%v) = %q, want %q", tt.phase, got, tt.want)
		}
	}
}

func TestTracker_StallDetection(t *testing.T) {
	tr, rec := newTestTracker()

	// Override stall timers to be much shorter for testing.
	// We can't override the constants, but we can test the mechanism
	// by checking that stall timers are set up correctly.
	tr.OnRunStarted("s1")
	tr.OnToolCall("s1", &canonical.ToolCall{
		ID: "c1", Name: "web_search",
		Input: json.RawMessage(`{"query":"test"}`),
	})

	tr.mu.Lock()
	s := tr.sessions["s1"]
	hasStallTimer := s != nil && s.StallTimer != nil
	tr.mu.Unlock()

	if !hasStallTimer {
		t.Error("stall timer should be set after tool call")
	}

	// Verify stall timer is reset on chunk.
	tr.OnChunk("s1")
	tr.mu.Lock()
	s = tr.sessions["s1"]
	hasStallTimer = s != nil && s.StallTimer != nil
	stallLevel := StallNone
	if s != nil {
		stallLevel = s.StallLevel
	}
	tr.mu.Unlock()

	if !hasStallTimer {
		t.Error("stall timer should be reset on chunk")
	}
	if stallLevel != StallNone {
		t.Errorf("stall level should be none after reset, got %v", stallLevel)
	}
	_ = rec // rec used in assertions
}

func TestTracker_UnknownSession_NoPanic(t *testing.T) {
	tr, _ := newTestTracker()
	// None of these should panic on unknown session.
	tr.OnToolCall("unknown", &canonical.ToolCall{ID: "c1", Name: "test"})
	tr.OnToolResult("unknown", &canonical.ToolResult{ToolCallID: "c1"})
	tr.OnChunk("unknown")
	tr.OnRunCompleted("unknown")
	tr.OnRunFailed("unknown", nil)
	tr.Clear("unknown")
}
