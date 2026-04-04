package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/memory"
)

// CompactionConfig controls auto-compaction behavior.
// Loaded from agent memory.yaml.
type CompactionConfig struct {
	Enabled          bool    `yaml:"enabled" json:"enabled"`
	MessageThreshold int     `yaml:"message_threshold" json:"message_threshold"`
	TokenRatio       float64 `yaml:"token_ratio" json:"token_ratio"`
	KeepRecent       float64 `yaml:"keep_recent" json:"keep_recent"`
	MinKeep          int     `yaml:"min_keep" json:"min_keep"`
}

// DefaultCompactionConfig returns sensible defaults.
func DefaultCompactionConfig() CompactionConfig {
	return CompactionConfig{
		Enabled:          true,
		MessageThreshold: 50,  // Fallback — primary trigger is token budget.
		TokenRatio:       0.60, // Trigger at 60% of context window to leave headroom.
		KeepRecent:       0.30,
		MinKeep:          4,
	}
}

// LLMCaller is a function that sends messages to an LLM and returns the response text.
// Used for memory flush and summarization during compaction.
type LLMCaller func(ctx context.Context, systemPrompt string, msgs []canonical.Message) (string, error)

// CompactionManager handles auto-compaction of conversation history.
type CompactionManager struct {
	memEngine memory.MemoryEngine
	llmCall   LLMCaller
	locks     map[string]*sync.Mutex // Per-session locks.
	mu        sync.Mutex             // Protects the locks map.
}

// NewCompactionManager creates a new compaction manager.
func NewCompactionManager(memEngine memory.MemoryEngine, llmCall LLMCaller) *CompactionManager {
	return &CompactionManager{
		memEngine: memEngine,
		llmCall:   llmCall,
		locks:     make(map[string]*sync.Mutex),
	}
}

// sessionLock returns a per-session mutex, creating it if needed.
func (cm *CompactionManager) sessionLock(sessionID string) *sync.Mutex {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if _, ok := cm.locks[sessionID]; !ok {
		cm.locks[sessionID] = &sync.Mutex{}
	}
	return cm.locks[sessionID]
}

// TryCompact attempts to compact a session's history.
// Returns the new message list if compaction occurred, or nil if skipped.
// Non-blocking: if another compaction is running for this session, returns immediately.
func (cm *CompactionManager) TryCompact(ctx context.Context, sessionID string, msgs []canonical.Message, cfg CompactionConfig, contextWindow int) []canonical.Message {
	if !cfg.Enabled {
		return nil
	}
	if cfg.MessageThreshold == 0 {
		cfg = DefaultCompactionConfig()
	}

	// Check if compaction is needed.
	if !NeedsCompaction(msgs, contextWindow, cfg.MessageThreshold, cfg.TokenRatio) {
		return nil
	}

	// Try to acquire the per-session lock (non-blocking).
	lock := cm.sessionLock(sessionID)
	if !lock.TryLock() {
		log.Printf("compaction: session %s already compacting, skipping", sessionID[:8])
		return nil
	}
	defer lock.Unlock()

	log.Printf("compaction: starting for session %s (%d messages)", sessionID[:8], len(msgs))

	// Split messages into "to compact" and "to keep".
	toCompact, toKeep := CompactionSplit(msgs, cfg.KeepRecent, cfg.MinKeep)
	if len(toCompact) == 0 {
		return nil
	}

	// Step 1: Memory flush — extract facts from the messages being compacted.
	cm.memoryFlush(ctx, sessionID, toCompact)

	// Step 2: Summarize — condense the compacted messages.
	summary := cm.summarize(ctx, toCompact)

	// Step 3: Inject summary + keep recent messages.
	result := InjectSummary(summary, toKeep)

	log.Printf("compaction: session %s compacted %d → %d messages", sessionID[:8], len(msgs), len(result))
	return result
}

// memoryFlush extracts important facts from messages and stores them in memory.
func (cm *CompactionManager) memoryFlush(ctx context.Context, sessionID string, msgs []canonical.Message) {
	if cm.memEngine == nil || cm.llmCall == nil {
		return
	}

	flushCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	// Build the extraction prompt.
	systemPrompt := `Extract important facts, decisions, key information, and action items from this conversation.
Return them as a JSON array of objects, each with "title" and "content" fields.
Focus on information that would be useful to recall in future conversations.
Be specific — include names, numbers, dates, decisions, and context.
Return only the JSON array, no other text.`

	response, err := cm.llmCall(flushCtx, systemPrompt, msgs)
	if err != nil {
		log.Printf("compaction: memory flush failed: %v", err)
		return
	}

	// Parse the response as JSON array of facts.
	var facts []struct {
		Title   string `json:"title"`
		Content string `json:"content"`
	}

	// Try to extract JSON from the response (might have markdown wrapping).
	jsonStr := response
	if idx := strings.Index(jsonStr, "["); idx >= 0 {
		if endIdx := strings.LastIndex(jsonStr, "]"); endIdx > idx {
			jsonStr = jsonStr[idx : endIdx+1]
		}
	}

	if err := json.Unmarshal([]byte(jsonStr), &facts); err != nil {
		log.Printf("compaction: failed to parse memory flush response: %v", err)
		// Store the raw response as a single memory entry.
		cm.memEngine.Write(ctx, response,
			fmt.Sprintf("Conversation summary (session %s)", sessionID[:8]),
			[]string{"compaction", "auto-extracted"})
		return
	}

	// Store each fact as a memory entry.
	for _, fact := range facts {
		if fact.Title == "" || fact.Content == "" {
			continue
		}
		cm.memEngine.Write(ctx, fact.Content, fact.Title,
			[]string{"compaction", "auto-extracted"})
	}

	log.Printf("compaction: flushed %d facts to memory from session %s", len(facts), sessionID[:8])
}

// summarize condenses messages into a summary using the LLM.
func (cm *CompactionManager) summarize(ctx context.Context, msgs []canonical.Message) string {
	if cm.llmCall == nil {
		return cm.fallbackSummary(msgs)
	}

	sumCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	systemPrompt := `Summarize this conversation. Use these exact sections:

DECISIONS: [key decisions made, with rationale]
FACTS: [specific facts learned — file paths, error messages, values]
ACTIONS: [what was done, what remains]
CONTEXT: [user preferences, constraints, goals expressed]

IMPORTANT: If a previous SUMMARY with these sections exists in the
messages (marked with "[Previous conversation summary]"), UPDATE it —
merge new information into existing sections. Do not discard information
from the previous summary. Append new decisions, facts, and actions.
Update the CONTEXT section if user preferences have changed.

Stay under 800 tokens total. Omit sections with no content.
Be specific and factual. Write in past tense. Do not add commentary.`

	response, err := cm.llmCall(sumCtx, systemPrompt, msgs)
	if err != nil {
		log.Printf("compaction: summarization failed: %v", err)
		return cm.fallbackSummary(msgs)
	}

	return response
}

// TryCompactWithBudget attempts compaction using a calibrated ContextBudget.
// This is the budget-aware entry point — use instead of TryCompact when
// a ContextBudget is available.
func (cm *CompactionManager) TryCompactWithBudget(ctx context.Context, sessionID string, msgs []canonical.Message, budget *ContextBudget) []canonical.Message {
	if budget == nil {
		return nil
	}

	// Only compact when history exceeds 100% of calibrated budget.
	if budget.Usage(msgs) < 1.0 {
		return nil
	}

	// Try to acquire the per-session lock (non-blocking).
	lock := cm.sessionLock(sessionID)
	if !lock.TryLock() {
		log.Printf("compaction: session %s already compacting, skipping", sessionID[:min(8, len(sessionID))])
		return nil
	}
	defer lock.Unlock()

	log.Printf("compaction: starting for session %s (%d messages, usage=%.2f)",
		sessionID[:min(8, len(sessionID))], len(msgs), budget.Usage(msgs))

	cfg := DefaultCompactionConfig()
	toCompact, toKeep := CompactionSplit(msgs, cfg.KeepRecent, cfg.MinKeep)
	if len(toCompact) == 0 {
		return nil
	}

	cm.memoryFlush(ctx, sessionID, toCompact)
	summary := cm.summarize(ctx, toCompact)
	result := InjectSummary(summary, toKeep)

	log.Printf("compaction: session %s compacted %d → %d messages",
		sessionID[:min(8, len(sessionID))], len(msgs), len(result))
	return result
}

// MaybeBackgroundCompact runs compaction in a background goroutine if the
// session history exceeds 85% of the full context window. Non-blocking:
// if another compaction is already running for this session, returns immediately.
//
// Pattern source: GoClaw internal/agent/loop_history.go — maybeSummarize().
func (cm *CompactionManager) MaybeBackgroundCompact(sessionID string, msgs []canonical.Message, budget *ContextBudget) {
	if budget == nil || len(msgs) <= 6 {
		return
	}

	// Check if history exceeds 85% of the full context window.
	historyTokens := estimateHistoryTokens(msgs)
	threshold := int(float64(budget.ContextWindow()) * 0.85)
	if historyTokens < threshold {
		return
	}

	lock := cm.sessionLock(sessionID)
	if !lock.TryLock() {
		return // Another compaction already running.
	}

	go func() {
		defer lock.Unlock()
		bgCtx := context.Background()

		log.Printf("background-compact: starting for session %s (%d messages, %d tokens)",
			sessionID[:min(8, len(sessionID))], len(msgs), historyTokens)

		cfg := DefaultCompactionConfig()
		toCompact, toKeep := CompactionSplit(msgs, cfg.KeepRecent, cfg.MinKeep)
		if len(toCompact) == 0 {
			return
		}

		// Flush memories BEFORE compacting — preserve important context.
		cm.memoryFlush(bgCtx, sessionID, toCompact)

		summary := cm.summarize(bgCtx, toCompact)
		result := InjectSummary(summary, toKeep)

		log.Printf("background-compact: session %s done %d → %d messages",
			sessionID[:min(8, len(sessionID))], len(msgs), len(result))

		// Note: The compacted result is not persisted here — the pipeline
		// owns message storage. This runs for the memory flush side effect.
		// Future: accept a store callback to persist compacted history.
	}()
}

// fallbackSummary creates a basic summary without LLM when the LLM call fails.
func (cm *CompactionManager) fallbackSummary(msgs []canonical.Message) string {
	var sb strings.Builder
	sb.WriteString("Conversation summary (auto-generated):\n\n")

	turnCount := 0
	for _, msg := range msgs {
		if msg.Role == "user" {
			turnCount++
			text := ""
			for _, c := range msg.Content {
				if c.Type == "text" {
					text = c.Text
					break
				}
			}
			if text != "" {
				if len(text) > 100 {
					text = text[:100] + "..."
				}
				fmt.Fprintf(&sb, "- User: %s\n", text)
			}
		}
	}
	fmt.Fprintf(&sb, "\n(%d turns in original conversation)", turnCount)
	return sb.String()
}
