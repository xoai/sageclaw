package provider

import (
	"log"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// ContextBridge handles conversation state transfer when the model router
// switches providers mid-conversation. Since all messages are stored in
// canonical format, the bridge focuses on intelligent truncation and
// coherence preservation.
type ContextBridge struct {
	// ModelContextWindows maps model names to their max context window (tokens).
	// Used to decide how much history to keep when switching models.
	ModelContextWindows map[string]int
}

// NewContextBridge creates a context bridge with known model context windows.
func NewContextBridge() *ContextBridge {
	return &ContextBridge{
		ModelContextWindows: map[string]int{
			// Anthropic
			"claude-sonnet-4-20250514":    200000,
			"claude-opus-4-20250514":      200000,
			"claude-haiku-3-20240307":     200000,
			// OpenAI
			"gpt-4o":                      128000,
			"gpt-4o-mini":                 128000,
			"gpt-4-turbo":                 128000,
			"gpt-4":                       8192,
			"gpt-3.5-turbo":              16384,
			"o1":                          200000,
			"o1-mini":                     128000,
			"o3-mini":                     200000,
			// Gemini
			"gemini-2.5-pro":             1048576,
			"gemini-2.5-flash":           1048576,
			"gemini-2.0-flash":           1048576,
			// Ollama (common models)
			"llama3":                      8192,
			"llama3.1":                    128000,
			"mistral":                     32768,
			"codellama":                   16384,
			"deepseek-coder":             16384,
		},
	}
}

// BridgeResult contains the result of a context bridge operation.
type BridgeResult struct {
	Messages       []canonical.Message
	Truncated      bool
	OriginalCount  int
	RetainedCount  int
	FromModel      string
	ToModel        string
	Reason         string
}

// Transfer prepares conversation history for a new model, truncating if needed.
// Messages are already in canonical format (provider-agnostic), so no
// format conversion is necessary — just intelligent truncation.
func (cb *ContextBridge) Transfer(messages []canonical.Message, fromModel, toModel string) BridgeResult {
	result := BridgeResult{
		OriginalCount: len(messages),
		FromModel:     fromModel,
		ToModel:       toModel,
	}

	if len(messages) == 0 {
		result.Messages = messages
		result.RetainedCount = 0
		return result
	}

	// Check if truncation is needed.
	toWindow := cb.getContextWindow(toModel)
	estimatedTokens := cb.estimateTokens(messages)

	// Use 80% of context window as budget (leave room for response).
	budget := int(float64(toWindow) * 0.8)

	if estimatedTokens <= budget {
		// Fits — no truncation needed.
		result.Messages = messages
		result.RetainedCount = len(messages)
		result.Reason = "fits within context window"
		return result
	}

	// Need to truncate. Strategy: keep system-relevant messages + recent history.
	result.Truncated = true
	result.Messages = cb.truncateMessages(messages, budget)
	result.RetainedCount = len(result.Messages)
	result.Reason = "truncated to fit context window"

	log.Printf("context-bridge: switched %s → %s, truncated %d → %d messages (est %d tokens → budget %d)",
		fromModel, toModel, result.OriginalCount, result.RetainedCount, estimatedTokens, budget)

	return result
}

// truncateMessages keeps the most important messages within a token budget.
// Strategy:
//   - Always keep the first user message (sets the conversation topic)
//   - Keep tool call + result pairs together (never orphan a tool result)
//   - Keep the most recent messages (recency matters most)
//   - If still over budget, summarize the middle
func (cb *ContextBridge) truncateMessages(messages []canonical.Message, budget int) []canonical.Message {
	if len(messages) <= 3 {
		return messages // Too few to truncate meaningfully.
	}

	// Phase 1: Keep first message + last N messages.
	// Start with last messages and work backwards until we hit the budget.
	first := messages[0]
	firstTokens := cb.estimateMessageTokens(first)
	remaining := budget - firstTokens

	var tail []canonical.Message
	tailTokens := 0

	for i := len(messages) - 1; i >= 1; i-- {
		msgTokens := cb.estimateMessageTokens(messages[i])
		if tailTokens+msgTokens > remaining {
			break
		}
		tail = append([]canonical.Message{messages[i]}, tail...)
		tailTokens += msgTokens
	}

	// Phase 2: Repair tool call/result pairs.
	// If we start with a tool result but its tool call was truncated,
	// remove the orphaned result.
	tail = cb.repairToolPairs(tail)

	// Combine: first message + [gap marker] + recent tail.
	result := make([]canonical.Message, 0, len(tail)+2)
	result = append(result, first)

	// Add a gap marker if we skipped messages.
	if len(tail) < len(messages)-1 {
		skipped := len(messages) - 1 - len(tail)
		result = append(result, canonical.Message{
			Role: "user",
			Content: []canonical.Content{{
				Type: "text",
				Text: "[Context bridge: " + itoa(skipped) + " earlier messages were trimmed to fit the model's context window. The conversation continues below.]",
			}},
		})
	}

	result = append(result, tail...)
	return result
}

// repairToolPairs ensures tool_result messages have their corresponding
// tool_call in the retained history. Orphaned results are removed.
func (cb *ContextBridge) repairToolPairs(messages []canonical.Message) []canonical.Message {
	// Collect all tool call IDs present in the messages.
	callIDs := map[string]bool{}
	for _, msg := range messages {
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				callIDs[c.ToolCall.ID] = true
			}
		}
	}

	// Filter out tool results whose call ID is missing.
	var repaired []canonical.Message
	for _, msg := range messages {
		if msg.Role == "tool" || hasToolResult(msg) {
			allPresent := true
			for _, c := range msg.Content {
				if c.ToolResult != nil && !callIDs[c.ToolResult.ToolCallID] {
					allPresent = false
					break
				}
			}
			if !allPresent {
				continue // Skip orphaned tool result.
			}
		}
		repaired = append(repaired, msg)
	}
	return repaired
}

// estimateTokens gives a rough token count for a message list.
// Uses the ~4 chars per token heuristic.
func (cb *ContextBridge) estimateTokens(messages []canonical.Message) int {
	total := 0
	for _, m := range messages {
		total += cb.estimateMessageTokens(m)
	}
	return total
}

func (cb *ContextBridge) estimateMessageTokens(m canonical.Message) int {
	tokens := 4 // Message overhead (role, formatting).
	for _, c := range m.Content {
		switch {
		case c.Text != "":
			tokens += len(c.Text) / 4
		case c.ToolCall != nil:
			tokens += len(c.ToolCall.Name)/4 + len(c.ToolCall.Input)/4 + 10
		case c.ToolResult != nil:
			tokens += len(c.ToolResult.Content) / 4
		case c.Thinking != "":
			tokens += len(c.Thinking) / 4
		}
	}
	return tokens
}

func (cb *ContextBridge) getContextWindow(model string) int {
	if w, ok := cb.ModelContextWindows[model]; ok {
		return w
	}
	// Default: 8K for unknown models (conservative).
	return 8192
}

func hasToolResult(m canonical.Message) bool {
	for _, c := range m.Content {
		if c.ToolResult != nil {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	s := ""
	for n > 0 {
		s = string(rune('0'+n%10)) + s
		n /= 10
	}
	return s
}
