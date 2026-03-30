package agent

import (
	"log"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tokenizer"
)

// ContextBudget tracks the token budget for a session's history.
// After the first LLM response, it calibrates the fixed overhead
// (system prompt + tool definitions) so all subsequent threshold
// calculations are accurate.
//
// Pattern source: GoClaw internal/agent/loop.go (runState fields).
type ContextBudget struct {
	contextWindow   int  // Model's full context window in tokens.
	responseReserve int  // Tokens reserved for the LLM response.
	overheadTokens  int  // System prompt + tool defs (calibrated after first call).
	historyBudget   int  // Tokens available for conversation history.
	calibrated      bool // True after first calibration.
}

// NewContextBudget creates a budget from a model ID and response reserve.
// Before calibration, uses a conservative 70% estimate for history budget.
func NewContextBudget(modelID string, responseReserve int) *ContextBudget {
	contextWindow := 200000 // Default (Claude).
	if m := provider.FindModel(modelID); m != nil {
		contextWindow = m.ContextWindow
	}
	if responseReserve <= 0 {
		responseReserve = 8192
	}

	b := &ContextBudget{
		contextWindow:   contextWindow,
		responseReserve: responseReserve,
	}
	// Conservative pre-calibration estimate: assume 30% overhead.
	b.overheadTokens = int(float64(contextWindow) * 0.30)
	b.historyBudget = contextWindow - b.overheadTokens - responseReserve
	// Cap pre-calibration budget too — prevents sending huge history on first request.
	if b.historyBudget > 25000 {
		b.historyBudget = 25000
	}
	return b
}

// Calibrate sets the overhead from the first LLM response's input token count.
// Only calibrates once — subsequent calls are no-ops.
func (b *ContextBudget) Calibrate(inputTokens int, history []canonical.Message) {
	if b.calibrated || inputTokens <= 0 {
		return
	}

	historyEst := estimateHistoryTokens(history)
	overhead := inputTokens - historyEst
	// Minimum overhead floor: system prompt + tools always cost something.
	// The tokenizer can overcount history (different encoding than Anthropic's
	// actual tokenizer), which makes overhead appear as 0. A floor of 2000
	// tokens (~8K chars of system prompt + tool schemas) prevents this.
	const minOverhead = 2000
	if overhead < minOverhead {
		overhead = minOverhead
	}

	b.overheadTokens = overhead
	b.historyBudget = b.contextWindow - overhead - b.responseReserve
	if b.historyBudget < 1000 {
		b.historyBudget = 1000
	}
	// Cap history budget to a sensible default. No agent needs >25K tokens
	// of history — larger conversations should use compaction/summarization.
	// This prevents sending 78+ messages to rate-limited APIs.
	const maxDefaultHistoryBudget = 25000
	if b.historyBudget > maxDefaultHistoryBudget {
		b.historyBudget = maxDefaultHistoryBudget
	}
	b.calibrated = true

	log.Printf("context-budget: calibrated overhead=%d historyBudget=%d (window=%d reserve=%d)",
		overhead, b.historyBudget, b.contextWindow, b.responseReserve)
}

// Usage returns how full the history budget is (0.0 to 1.0+).
// Uses the token counter when available, falls back to chars/4.
func (b *ContextBudget) Usage(history []canonical.Message) float64 {
	if b.historyBudget <= 0 {
		return 1.0
	}
	tokens := estimateHistoryTokens(history)
	return float64(tokens) / float64(b.historyBudget)
}

// HistoryBudget returns the number of tokens available for history.
func (b *ContextBudget) HistoryBudget() int {
	return b.historyBudget
}

// ContextWindow returns the model's full context window.
func (b *ContextBudget) ContextWindow() int {
	return b.contextWindow
}

// CapHistoryBudget reduces the history budget to fit a per-request token cap.
// The cap accounts for overhead (system prompt + tools) — only the remaining
// tokens are available for history. This enables agents on rate-limited orgs
// to stay within their token/min budget.
func (b *ContextBudget) CapHistoryBudget(maxRequestTokens int) {
	available := maxRequestTokens - b.overheadTokens - b.responseReserve
	if available < 1000 {
		available = 1000
	}
	if available < b.historyBudget {
		b.historyBudget = available
	}
}

// IsCalibrated returns true if the budget has been calibrated.
func (b *ContextBudget) IsCalibrated() bool {
	return b.calibrated
}

// estimateHistoryTokens counts tokens in history using tiktoken,
// falling back to chars/4 if the tokenizer is unavailable.
func estimateHistoryTokens(history []canonical.Message) int {
	counter, err := tokenizer.Get()
	if err != nil {
		// Fallback: rough character-based estimate.
		total := 0
		for _, msg := range history {
			for _, c := range msg.Content {
				switch {
				case c.Type == "text":
					total += len(c.Text)
				case c.ToolCall != nil:
					total += len(c.ToolCall.Name) + len(c.ToolCall.Input)
				case c.ToolResult != nil:
					total += len(c.ToolResult.Content)
				}
			}
		}
		return total / 4
	}
	return counter.CountMessages(history)
}
