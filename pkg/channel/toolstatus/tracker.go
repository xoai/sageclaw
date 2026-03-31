package toolstatus

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Debounce and stall timing constants.
const (
	EditDebounceMs   = 1000
	ReactDebounceMs  = 700
	StallSoftMs      = 10000
	StallHardMs      = 30000
)

// ReactionPhase identifies the current processing phase for emoji reactions.
type ReactionPhase string

const (
	PhaseThinking  ReactionPhase = "thinking"
	PhaseTool      ReactionPhase = "tool"
	PhaseCoding    ReactionPhase = "coding"
	PhaseWeb       ReactionPhase = "web"
	PhaseDone      ReactionPhase = "done"
	PhaseError     ReactionPhase = "error"
	PhaseStallSoft ReactionPhase = "stallSoft"
	PhaseStallHard ReactionPhase = "stallHard"
)

// StallLevel tracks escalating stall severity.
type StallLevel int

const (
	StallNone StallLevel = iota
	StallSoft
	StallHard
)

// StatusUpdate is delivered to the onText callback.
type StatusUpdate struct {
	Text      string // Formatted status, e.g. "🔍 Searching: market trends..."
	ToolCount int    // Number of active tools
	Done      bool   // True when all tools completed
}

// ReactionUpdate is delivered to the onReact callback.
type ReactionUpdate struct {
	Phase ReactionPhase
	Emoji string
}

// ActiveTool represents a currently executing tool.
type ActiveTool struct {
	Name      string
	CallID    string
	StartedAt time.Time
	Display   ToolDisplay
}

// SessionState holds per-session tracker state.
type SessionState struct {
	SessionID   string
	ActiveTools []ActiveTool
	Counts      map[string]int // tool name → call count
	Phase       ReactionPhase
	LastFlush   time.Time
	EditTimer   *time.Timer
	ReactTimer  *time.Timer
	StallTimer  *time.Timer
	StallLevel  StallLevel
	FirstTool   bool // true until first tool flush occurs
}

// Tracker manages tool status state per session.
type Tracker struct {
	mu       sync.Mutex
	sessions map[string]*SessionState
	display  *ToolDisplayMap
	onText   func(sessionID string, update StatusUpdate)
	onReact  func(sessionID string, update ReactionUpdate)
	now      func() time.Time // injectable clock for testing
}

// NewTracker creates a tracker with the given display map and callbacks.
// Callbacks may be nil initially and set later via SetCallbacks.
func NewTracker(
	display *ToolDisplayMap,
	onText func(sessionID string, update StatusUpdate),
	onReact func(sessionID string, update ReactionUpdate),
) *Tracker {
	return &Tracker{
		sessions: make(map[string]*SessionState),
		display:  display,
		onText:   onText,
		onReact:  onReact,
		now:      time.Now,
	}
}

// SetCallbacks sets or replaces the flush callbacks. Thread-safe.
func (t *Tracker) SetCallbacks(
	onText func(sessionID string, update StatusUpdate),
	onReact func(sessionID string, update ReactionUpdate),
) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onText = onText
	t.onReact = onReact
}

// OnRunStarted initializes session state and sets thinking phase.
func (t *Tracker) OnRunStarted(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clean up any leftover state from a previous run.
	t.clearLocked(sessionID)

	t.sessions[sessionID] = &SessionState{
		SessionID: sessionID,
		Counts:    make(map[string]int),
		FirstTool: true,
	}

	t.emitReactLocked(sessionID, PhaseThinking)
}

// OnToolCall records a new tool call and triggers status/reaction updates.
func (t *Tracker) OnToolCall(sessionID string, tc *canonical.ToolCall) {
	if tc == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.sessions[sessionID]
	if s == nil {
		return
	}

	d := t.display.ResolveDisplay(tc.Name, tc.Input)
	s.ActiveTools = append(s.ActiveTools, ActiveTool{
		Name:      tc.Name,
		CallID:    tc.ID,
		StartedAt: t.now(),
		Display:   d,
	})
	s.Counts[tc.Name]++

	// Reset stall timer on any event.
	t.resetStallLocked(s)

	// Determine reaction phase from category.
	phase := categoryToPhase(d.Category)
	t.debounceReactLocked(sessionID, phase)

	// Status text: first tool flushes immediately, subsequent debounce.
	if s.FirstTool {
		s.FirstTool = false
		t.flushTextLocked(sessionID, s)
	} else {
		t.debounceTextLocked(sessionID, s)
	}
}

// OnToolResult records tool completion.
func (t *Tracker) OnToolResult(sessionID string, tr *canonical.ToolResult) {
	if tr == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.sessions[sessionID]
	if s == nil {
		return
	}

	// Remove the matching active tool.
	for i, at := range s.ActiveTools {
		if at.CallID == tr.ToolCallID {
			s.ActiveTools = append(s.ActiveTools[:i], s.ActiveTools[i+1:]...)
			break
		}
	}

	// Reset stall timer.
	t.resetStallLocked(s)

	// If no active tools remain, flush immediately with Done=true.
	if len(s.ActiveTools) == 0 {
		t.stopTimer(&s.EditTimer)
		t.flushTextDoneLocked(sessionID, s)
	} else {
		t.debounceTextLocked(sessionID, s)
	}
}

// OnChunk resets the stall timer (text is streaming).
func (t *Tracker) OnChunk(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.sessions[sessionID]
	if s == nil {
		return
	}
	t.resetStallLocked(s)
}

// OnRunCompleted marks the run as done and flushes terminal state.
func (t *Tracker) OnRunCompleted(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.sessions[sessionID]
	if s == nil {
		return
	}

	// Terminal: immediate reaction, no debounce.
	t.emitReactLocked(sessionID, PhaseDone)
	t.flushTextDoneLocked(sessionID, s)
	t.clearLocked(sessionID)
}

// OnRunFailed marks the run as failed.
func (t *Tracker) OnRunFailed(sessionID string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	s := t.sessions[sessionID]
	if s == nil {
		return
	}

	t.emitReactLocked(sessionID, PhaseError)
	t.flushTextDoneLocked(sessionID, s)
	t.clearLocked(sessionID)
}

// Clear stops all timers and removes session state.
func (t *Tracker) Clear(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clearLocked(sessionID)
}

// --- internal helpers ---

func (t *Tracker) clearLocked(sessionID string) {
	s := t.sessions[sessionID]
	if s == nil {
		return
	}
	t.stopTimer(&s.EditTimer)
	t.stopTimer(&s.ReactTimer)
	t.stopTimer(&s.StallTimer)
	delete(t.sessions, sessionID)
}

func (t *Tracker) stopTimer(tp **time.Timer) {
	if *tp != nil {
		(*tp).Stop()
		*tp = nil
	}
}

func (t *Tracker) emitReactLocked(sessionID string, phase ReactionPhase) {
	s := t.sessions[sessionID]
	if s != nil {
		if s.Phase == phase {
			return // dedup: same phase, skip API call
		}
		s.Phase = phase
	}
	cb := t.onReact
	if cb != nil {
		emoji := phaseEmoji(phase)
		// Release lock before I/O-bound callback.
		t.mu.Unlock()
		cb(sessionID, ReactionUpdate{
			Phase: phase,
			Emoji: emoji,
		})
		t.mu.Lock()
	}
}

func (t *Tracker) debounceReactLocked(sessionID string, phase ReactionPhase) {
	s := t.sessions[sessionID]
	if s == nil {
		return
	}
	t.stopTimer(&s.ReactTimer)
	s.ReactTimer = time.AfterFunc(time.Duration(ReactDebounceMs)*time.Millisecond, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		// Re-check session still exists (may have been cleared).
		if _, ok := t.sessions[sessionID]; ok {
			t.emitReactLocked(sessionID, phase)
		}
	})
}

func (t *Tracker) flushTextLocked(sessionID string, s *SessionState) {
	text := t.buildStatusText(s)
	s.LastFlush = t.now()
	cb := t.onText
	count := len(s.ActiveTools)
	if cb != nil {
		// Release lock before I/O-bound callback.
		t.mu.Unlock()
		cb(sessionID, StatusUpdate{
			Text:      text,
			ToolCount: count,
			Done:      false,
		})
		t.mu.Lock()
	}
}

func (t *Tracker) flushTextDoneLocked(sessionID string, s *SessionState) {
	cb := t.onText
	if cb != nil {
		t.mu.Unlock()
		cb(sessionID, StatusUpdate{
			Text:      "",
			ToolCount: 0,
			Done:      true,
		})
		t.mu.Lock()
	}
}

func (t *Tracker) debounceTextLocked(sessionID string, s *SessionState) {
	t.stopTimer(&s.EditTimer)
	s.EditTimer = time.AfterFunc(time.Duration(EditDebounceMs)*time.Millisecond, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		ss := t.sessions[sessionID]
		if ss == nil {
			return
		}
		t.flushTextLocked(sessionID, ss)
	})
}

func (t *Tracker) resetStallLocked(s *SessionState) {
	t.stopTimer(&s.StallTimer)
	s.StallLevel = StallNone

	sessionID := s.SessionID
	s.StallTimer = time.AfterFunc(time.Duration(StallSoftMs)*time.Millisecond, func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		ss := t.sessions[sessionID]
		if ss == nil || len(ss.ActiveTools) == 0 {
			return
		}
		ss.StallLevel = StallSoft
		t.emitReactLocked(sessionID, PhaseStallSoft)

		// Schedule hard stall.
		ss.StallTimer = time.AfterFunc(time.Duration(StallHardMs-StallSoftMs)*time.Millisecond, func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			ss2 := t.sessions[sessionID]
			if ss2 == nil || len(ss2.ActiveTools) == 0 {
				return
			}
			ss2.StallLevel = StallHard
			t.emitReactLocked(sessionID, PhaseStallHard)
		})
	})
}

func (t *Tracker) buildStatusText(s *SessionState) string {
	if len(s.ActiveTools) == 0 {
		return ""
	}

	// Deduplicate by tool name, keeping first display + count.
	type entry struct {
		display ToolDisplay
		count   int
	}
	seen := make(map[string]*entry)
	var order []string
	for _, at := range s.ActiveTools {
		if e, ok := seen[at.Name]; ok {
			e.count++
		} else {
			seen[at.Name] = &entry{display: at.Display, count: 1}
			order = append(order, at.Name)
		}
	}

	var parts []string
	for _, name := range order {
		e := seen[name]
		parts = append(parts, e.display.FormatStatus(e.count))
	}
	return strings.Join(parts, " | ")
}

// categoryToPhase maps tool category to reaction phase.
func categoryToPhase(category string) ReactionPhase {
	switch category {
	case "web":
		return PhaseWeb
	case "coding":
		return PhaseCoding
	default:
		return PhaseTool
	}
}

// phaseEmoji maps reaction phase to its display emoji.
func phaseEmoji(phase ReactionPhase) string {
	switch phase {
	case PhaseThinking:
		return "🤔"
	case PhaseTool:
		return "🔥"
	case PhaseCoding:
		return "👨‍💻"
	case PhaseWeb:
		return "⚡"
	case PhaseDone:
		return "👍"
	case PhaseError:
		return "😱"
	case PhaseStallSoft:
		return "🥱"
	case PhaseStallHard:
		return "😨"
	default:
		return fmt.Sprintf("[%s]", phase)
	}
}
