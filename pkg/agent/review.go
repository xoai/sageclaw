package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// BackgroundReviewer periodically extracts memories from conversation
// history without blocking the main agent loop. Spawns a background
// goroutine every N user turns to persist user preferences, project
// context, and (for complex tasks) procedural knowledge.
type BackgroundReviewer struct {
	memEngine memory.MemoryEngine
	llmCall   LLMCaller    // agent.LLMCaller (message-based signature)
	onEvent   EventHandler // Optional: emits EventBackgroundReview events.
	interval  int          // User turns between reviews (0 = disabled)

	turnCount int32        // Atomic: current turn counter
	reviewing atomic.Int32 // 1 = review in progress (debounce)
	mu        sync.Mutex   // Protects non-atomic state if needed
}

// NewBackgroundReviewer creates a reviewer that fires every interval user turns.
// Pass interval=0 to disable. llmCall must use the message-based signature
// (same type as CompactionManager). onEvent is optional (nil = no events).
func NewBackgroundReviewer(memEngine memory.MemoryEngine, llmCall LLMCaller, interval int, onEvent ...EventHandler) *BackgroundReviewer {
	if interval < 0 {
		interval = 0
	}
	var handler EventHandler
	if len(onEvent) > 0 {
		handler = onEvent[0]
	}
	return &BackgroundReviewer{
		memEngine: memEngine,
		llmCall:   llmCall,
		onEvent:   handler,
		interval:  interval,
	}
}

// OnUserTurn increments the turn counter and triggers a background review
// if the interval is reached. Non-blocking — spawns a goroutine if needed.
//
// sessionID: current session for logging.
// history: conversation messages (shallow-copied before goroutine spawn).
// iterationCount: how many loop iterations this turn took (5+ triggers procedure extraction).
func (br *BackgroundReviewer) OnUserTurn(sessionID string, history []canonical.Message, iterationCount int) {
	if br.interval <= 0 || br.memEngine == nil || br.llmCall == nil {
		return
	}

	newCount := atomic.AddInt32(&br.turnCount, 1)
	if int(newCount)%br.interval != 0 {
		return
	}

	// Debounce: skip if a review is already running.
	if !br.reviewing.CompareAndSwap(0, 1) {
		log.Printf("[%s] background review: skipped (already reviewing)", sessionID[:min(8, len(sessionID))])
		return
	}

	// Shallow-copy history — safe because the main loop only appends new
	// messages, never mutates existing Message structs or their Content slices.
	historyCopy := make([]canonical.Message, len(history))
	copy(historyCopy, history)

	go br.runReview(sessionID, historyCopy, iterationCount)
}

// runReview executes the background review in a goroutine.
func (br *BackgroundReviewer) runReview(sessionID string, history []canonical.Message, iterationCount int) {
	defer br.reviewing.Store(0)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sid := sessionID[:min(8, len(sessionID))]
	log.Printf("[%s] background review: starting (turns=%d, iterations=%d)",
		sid, atomic.LoadInt32(&br.turnCount), iterationCount)
	br.emit(Event{Type: EventBackgroundReview, SessionID: sessionID, Text: "starting"})

	prompt := br.buildReviewPrompt(iterationCount)

	response, err := br.llmCall(ctx, prompt, history)
	if err != nil {
		log.Printf("[%s] background review: failed (%v)", sid, err)
		br.emit(Event{Type: EventBackgroundReview, SessionID: sessionID, Text: "failed", Error: err})
		return
	}

	extracted := br.parseAndStore(ctx, sid, response)

	// Bump confidence on existing procedures that match the current context.
	br.bumpProcedureConfidence(ctx, sid, history)

	log.Printf("[%s] background review: completed (%d entries stored)", sid, extracted)
	br.emit(Event{Type: EventBackgroundReview, SessionID: sessionID, Text: fmt.Sprintf("completed (%d entries)", extracted)})
}

// bumpProcedureConfidence searches for existing procedures relevant to the
// conversation and increases their confidence score. This organic improvement
// loop means procedures that keep being useful gain confidence over time.
func (br *BackgroundReviewer) bumpProcedureConfidence(ctx context.Context, sid string, history []canonical.Message) {
	// Extract last user query for search.
	var query string
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "user" {
			for _, c := range history[i].Content {
				if c.Type == "text" && c.Text != "" {
					query = c.Text
					break
				}
			}
			if query != "" {
				break
			}
		}
	}
	if query == "" {
		return
	}

	// Search for matching procedures.
	results, err := br.memEngine.Search(ctx, query, memory.SearchOptions{
		FilterTags: []string{"self-learning", "procedure"},
		Limit:      3,
	})
	if err != nil || len(results) == 0 {
		return
	}

	// Bump confidence by 0.15 (capped at 0.9) for matching procedures.
	cw, ok := br.memEngine.(memory.ConfidenceWriter)
	if !ok {
		return
	}

	for _, r := range results {
		// Bump by 0.15, capped at 0.9. Progression: 0.5 → 0.65 → 0.8 → 0.9.
		if err := cw.BumpConfidence(ctx, r.ID, 0.15, 0.9); err != nil {
			log.Printf("[%s] background review: confidence bump failed for %q: %v", sid, r.Title, err)
		}
	}
}

// emit sends an event if a handler is registered.
func (br *BackgroundReviewer) emit(e Event) {
	if br.onEvent != nil {
		br.onEvent(e)
	}
}

// buildReviewPrompt constructs the review system prompt. When iterationCount >= 5,
// includes procedure extraction instructions.
func (br *BackgroundReviewer) buildReviewPrompt(iterationCount int) string {
	base := `Review this conversation and extract information worth remembering
for future conversations. Return a JSON array of objects:

[{"title": "...", "content": "...", "type": "memory"}]

For type "memory", extract:
- User preferences and communication style
- Project context and constraints
- Recurring patterns in what the user asks for
- Corrections the user gave about approach or behavior

Do NOT extract:
- Code-specific facts (file paths, function names) — these change
- Secrets, tokens, passwords
- One-time operations that won't recur

Return only the JSON array, no other text.`

	if iterationCount >= 5 {
		base += `

Additionally, this task required ` + fmt.Sprintf("%d", iterationCount) + ` iterations to complete,
suggesting a complex multi-step approach. If you identify a reusable
procedure, include an entry with type "procedure":

{"title": "[PROC] How to ...", "content": "TRIGGER: ...\nSTEPS:\n1. ...\n2. ...\nPITFALLS:\n- ...", "type": "procedure"}

Only create a procedure if the approach is genuinely reusable and
non-obvious. Do NOT create procedures that recommend destructive
commands (rm -rf, DROP TABLE), credential access, or disabling
security features. Do NOT include secrets in procedure content.`
	}

	return base
}

// reviewEntry represents a single extracted memory or procedure.
type reviewEntry struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	Type    string `json:"type"` // "memory" or "procedure"
}

// parseAndStore parses the LLM response and writes entries to memory.
// Returns the number of entries stored.
func (br *BackgroundReviewer) parseAndStore(ctx context.Context, sid string, response string) int {
	var entries []reviewEntry

	// Extract JSON from response (may have markdown wrapping).
	jsonStr := response
	if idx := strings.Index(jsonStr, "["); idx >= 0 {
		if endIdx := strings.LastIndex(jsonStr, "]"); endIdx > idx {
			jsonStr = jsonStr[idx : endIdx+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &entries); err != nil {
		log.Printf("[%s] background review: malformed JSON, storing raw response", sid)
		// Fallback: store the raw response as a single memory entry.
		br.memEngine.Write(ctx, response,
			fmt.Sprintf("Background review (session %s)", sid),
			[]string{"review", "auto-extracted"})
		return 1
	}

	stored := 0
	for _, entry := range entries {
		if entry.Title == "" || entry.Content == "" {
			continue
		}

		tags := []string{"review", "auto-extracted"}
		if entry.Type == "procedure" {
			tags = append(tags, "self-learning", "procedure")
			// Use ConfidenceWriter if available (procedures start at 0.5).
			if cw, ok := br.memEngine.(memory.ConfidenceWriter); ok {
				if _, err := cw.WriteWithConfidence(ctx, entry.Content, entry.Title, tags, 0.5); err != nil {
					log.Printf("[%s] background review: failed to write procedure %q: %v", sid, entry.Title, err)
					continue
				}
				stored++
				continue
			}
		}

		if _, err := br.memEngine.Write(ctx, entry.Content, entry.Title, tags); err != nil {
			log.Printf("[%s] background review: failed to write %q: %v", sid, entry.Title, err)
			continue
		}
		stored++
	}

	return stored
}
