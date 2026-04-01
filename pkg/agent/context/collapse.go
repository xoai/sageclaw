package context

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	collapseTimeout       = 30 * time.Second
	collapseSummaryPrompt = `Summarize this conversation segment concisely. Preserve:
- Decisions made and their rationale
- File paths that were read or modified
- Errors encountered and how they were resolved
- User preferences or instructions expressed
Keep under 200 tokens. Start directly with the summary.`

	// collapseProtectedTail is the number of recent messages never collapsed.
	collapseProtectedTail = 5
)

// applyCollapse produces a projected view of the history by replacing
// collapsed iteration ranges with summary messages. Original history
// is NOT mutated.
//
// Collapse triggers when estimated token usage exceeds threshold.
// The oldest 50% of non-protected messages are collapsed.
//
// Protected messages: first user message, last 5 messages, messages
// with write tool results.
func applyCollapse(
	ctx context.Context,
	sessionID string,
	history []canonical.Message,
	collapseStore *CollapseStore,
	llmCaller LLMCaller,
	threshold float64,
	budgetTokens int,
) []canonical.Message {
	if collapseStore == nil || budgetTokens <= 0 {
		return history
	}

	// Apply existing collapses first, then check if further collapse is needed.
	existing := collapseStore.Get(sessionID)
	if len(existing) > 0 {
		projected := projectView(history, existing)
		// Re-check budget on projected view — may need further collapse.
		projectedTokens := 0
		for i := range projected {
			projectedTokens += EstimateTokens(&projected[i])
		}
		if float64(projectedTokens)/float64(budgetTokens) < threshold {
			return projected // Existing collapses are sufficient.
		}
		// Still over threshold — fall through to create a new collapse
		// on the projected view.
		history = projected
	}

	// Estimate current token usage.
	totalTokens := 0
	for i := range history {
		totalTokens += EstimateTokens(&history[i])
	}

	usage := float64(totalTokens) / float64(budgetTokens)
	if usage < threshold {
		return history // Under threshold, no collapse needed.
	}

	// Determine collapse range: oldest 50% of non-protected messages.
	protectedSet := buildProtectedSet(history)
	midpoint := len(history) / 2

	// Build set of protected iterations — if ANY message at an iteration
	// is protected, the entire iteration is protected (keeps tool_call/
	// tool_result pairs intact with their user message).
	protectedIters := make(map[int]bool)
	for i, msg := range history {
		if protectedSet[i] && msg.Annotations != nil {
			protectedIters[msg.Annotations.Iteration] = true
		}
	}

	// Collect collapsible iteration numbers and messages.
	var toCollapse []canonical.Message
	collapsibleIters := make(map[int]bool)

	for i := 0; i < midpoint; i++ {
		ann := history[i].Annotations
		if ann == nil {
			continue
		}
		if protectedIters[ann.Iteration] {
			continue
		}
		collapsibleIters[ann.Iteration] = true
		toCollapse = append(toCollapse, history[i])
	}

	// Derive range bounds from collapsible iterations (for display).
	var startIter, endIter int
	startIter = -1
	for iter := range collapsibleIters {
		if startIter < 0 || iter < startIter {
			startIter = iter
		}
		if iter > endIter {
			endIter = iter
		}
	}

	if len(toCollapse) < 3 || startIter < 0 {
		return history // Not enough messages to justify a collapse.
	}

	// Generate summary.
	summary := generateCollapseSummary(ctx, toCollapse, llmCaller)
	if summary == "" {
		return history // Summary generation failed — skip collapse.
	}

	// Store the collapse entry.
	tokensSaved := 0
	for _, msg := range toCollapse {
		if msg.Annotations != nil {
			tokensSaved += msg.Annotations.TokenEstimate
		}
	}

	entry := CollapseEntry{
		StartIter:  startIter,
		EndIter:    endIter,
		Iterations: collapsibleIters,
		Summary:    summary,
		CreatedAt:  time.Now(),
		Tokens:     tokensSaved,
	}
	collapseStore.Add(sessionID, entry)

	return projectView(history, []CollapseEntry{entry})
}

// projectView produces the projected view by replacing messages in
// collapsed ranges with summary messages. Uses iteration-based matching
// for stability across history mutations.
func projectView(history []canonical.Message, collapses []CollapseEntry) []canonical.Message {
	if len(collapses) == 0 {
		return history
	}

	var result []canonical.Message
	emitted := make(map[int]bool) // Track which collapse entries have been emitted.

	for _, msg := range history {
		ann := msg.Annotations

		// Check if this message's iteration is in a collapsed set.
		collapsed := false
		if ann != nil {
			for i, entry := range collapses {
				if entry.Iterations != nil && entry.Iterations[ann.Iteration] {
					collapsed = true
					if !emitted[i] {
						emitted[i] = true
						result = append(result, canonical.Message{
							Role: "assistant",
							Content: []canonical.Content{{
								Type: "text",
								Text: fmt.Sprintf("[Collapsed iterations %d-%d]\n\n%s", entry.StartIter, entry.EndIter, entry.Summary),
							}},
						})
					}
					break
				}
			}
		}

		if !collapsed {
			result = append(result, msg)
		}
	}

	return SanitizePreservingAnnotations(result)
}

// buildProtectedSet marks messages that should never be collapsed:
// - First user message
// - Last collapseProtectedTail messages
// - Messages with write tool results
func buildProtectedSet(history []canonical.Message) map[int]bool {
	protected := make(map[int]bool)

	// Protect first user message.
	for i, msg := range history {
		if msg.Role == "user" {
			protected[i] = true
			break
		}
	}

	// Protect last N messages.
	start := len(history) - collapseProtectedTail
	if start < 0 {
		start = 0
	}
	for i := start; i < len(history); i++ {
		protected[i] = true
	}

	// Protect messages with write tool results.
	toolNames := buildToolNameMap(history)
	for i, msg := range history {
		for _, c := range msg.Content {
			if c.ToolResult != nil {
				name := toolNames[c.ToolResult.ToolCallID]
				if !readOnlyTools[name] && name != "" {
					protected[i] = true
				}
			}
		}
	}

	return protected
}

// generateCollapseSummary creates a summary of messages to collapse.
func generateCollapseSummary(ctx context.Context, msgs []canonical.Message, llmCaller LLMCaller) string {
	if llmCaller == nil {
		return ""
	}

	// Build a condensed representation of the messages.
	var content string
	for _, msg := range msgs {
		for _, c := range msg.Content {
			if c.Text != "" {
				snippet := c.Text
				if len(snippet) > 500 {
					snippet = snippet[:500] + "..."
				}
				content += fmt.Sprintf("[%s] %s\n", msg.Role, snippet)
			}
			if c.ToolCall != nil {
				content += fmt.Sprintf("[tool_call] %s\n", c.ToolCall.Name)
			}
			if c.ToolResult != nil {
				snippet := c.ToolResult.Content
				if len(snippet) > 300 {
					snippet = snippet[:300] + "..."
				}
				content += fmt.Sprintf("[tool_result] %s\n", snippet)
			}
		}
	}

	// Cap total input.
	if len(content) > 8000 {
		content = content[:8000] + "\n[truncated]"
	}

	summary, err := llmCaller(ctx, collapseSummaryPrompt, content, collapseTimeout)
	if err != nil {
		log.Printf("[context-pipeline] collapse summary failed: %v", err)
		return ""
	}

	return summary
}
