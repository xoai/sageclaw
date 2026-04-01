package context

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	// microCompactMinChars is the minimum content size to be eligible.
	microCompactMinChars = 2000

	// microCompactMaxBatch is the max results compressed per pipeline run.
	microCompactMaxBatch = 5

	// microCompactTimeout is the timeout for the compression LLM call.
	microCompactTimeout = 30 * time.Second

	microCompactSystemPrompt = `Compress these tool results to ~30% of their size.
Preserve: file paths, error messages, key data points, numbers.
Remove: boilerplate, repeated patterns, verbose formatting, decorative output.
Return ONLY the compressed results, numbered to match the input.`
)

// applyContentClear replaces aged, large tool results with static placeholders.
// No LLM call — this is the cheap first tier of microcompact, activated at
// moderate context pressure (≥ 0.5). The LLM-based applyMicroCompact is
// reserved for higher pressure (≥ 0.7).
func applyContentClear(
	history []canonical.Message,
	currentIteration int,
	microCompactAge int,
) []canonical.Message {
	if microCompactAge <= 0 {
		return history
	}

	toolNames := buildToolNameMap(history)

	var modified bool
	out := make([]canonical.Message, len(history))
	copy(out, history)

	for i, msg := range out {
		ann := msg.Annotations
		if ann == nil {
			continue
		}
		if ann.Snipped || ann.OverflowPath != "" {
			continue
		}
		age := currentIteration - ann.Iteration
		if age < microCompactAge {
			continue
		}

		for j, c := range msg.Content {
			if c.ToolResult == nil {
				continue
			}
			if len(c.ToolResult.Content) < microCompactMinChars {
				continue
			}

			if !modified {
				modified = true
			}

			toolName := toolNames[c.ToolResult.ToolCallID]
			if toolName == "" {
				toolName = "unknown"
			}
			originalSize := len(c.ToolResult.Content)

			out[i] = CopyMessageWithAnnotations(out[i])
			trCopy := *out[i].Content[j].ToolResult
			trCopy.Content = fmt.Sprintf("[Tool result cleared — iteration %d, tool: %s, original: %d chars]",
				ann.Iteration, toolName, originalSize)
			out[i].Content[j].ToolResult = &trCopy

			if out[i].Annotations != nil {
				out[i].Annotations.TokenEstimate = 0
			}
		}
	}

	if !modified {
		return history
	}
	return out
}

// applyMicroCompact compresses aged tool results using a fast-tier LLM.
// Messages are copied before modification. On LLM failure, results are
// left unchanged (graceful degradation).
func applyMicroCompact(
	ctx context.Context,
	history []canonical.Message,
	currentIteration int,
	microCompactAge int,
	llmCaller LLMCaller,
) []canonical.Message {
	if llmCaller == nil || microCompactAge <= 0 {
		return history
	}

	// Collect eligible results.
	type eligible struct {
		msgIdx     int
		contentIdx int
		content    string
	}

	var candidates []eligible
	for i, msg := range history {
		ann := msg.Annotations
		if ann == nil {
			continue
		}
		// Skip already processed messages.
		if ann.Snipped || ann.OverflowPath != "" {
			continue
		}

		age := currentIteration - ann.Iteration
		if age < microCompactAge {
			continue
		}

		for j, c := range msg.Content {
			if c.ToolResult == nil {
				continue
			}
			if len(c.ToolResult.Content) < microCompactMinChars {
				continue
			}
			candidates = append(candidates, eligible{
				msgIdx:     i,
				contentIdx: j,
				content:    c.ToolResult.Content,
			})
		}
	}

	if len(candidates) == 0 {
		return history
	}

	// Cap batch size.
	if len(candidates) > microCompactMaxBatch {
		candidates = candidates[:microCompactMaxBatch]
	}

	// Build the compression prompt.
	var prompt strings.Builder
	for i, c := range candidates {
		fmt.Fprintf(&prompt, "--- Result %d (%d chars) ---\n%s\n\n", i+1, len(c.content), c.content)
	}

	// Call the LLM.
	compressed, err := llmCaller(ctx, microCompactSystemPrompt, prompt.String(), microCompactTimeout)
	if err != nil {
		log.Printf("[context-pipeline] micro-compact LLM call failed: %v", err)
		return history // Graceful degradation.
	}

	// Parse the compressed results. Split by "--- Result N" markers.
	// If parsing fails, fall back to using the full compressed text
	// for the first candidate only.
	parts := parseMicroCompactResponse(compressed, len(candidates))

	// Apply compressed results.
	out := make([]canonical.Message, len(history))
	copy(out, history)

	for i, c := range candidates {
		var replacement string
		if i < len(parts) && parts[i] != "" {
			replacement = parts[i]
		} else if i == 0 {
			replacement = compressed // Fallback: use full response for first.
		} else {
			continue // Can't match this result — skip.
		}

		// Copy the message and tool result before mutating.
		out[c.msgIdx] = CopyMessageWithAnnotations(out[c.msgIdx])
		trCopy := *out[c.msgIdx].Content[c.contentIdx].ToolResult
		trCopy.Content = fmt.Sprintf("%s\n\n[micro-compacted from %d chars]", replacement, len(c.content))
		out[c.msgIdx].Content[c.contentIdx].ToolResult = &trCopy

		// Reset token estimate.
		if out[c.msgIdx].Annotations != nil {
			out[c.msgIdx].Annotations.TokenEstimate = 0
		}
	}

	return out
}

// parseMicroCompactResponse splits the LLM response into individual results.
// Expects "--- Result N" markers or numbered lines.
func parseMicroCompactResponse(response string, expected int) []string {
	// Try splitting by "--- Result N" markers first.
	parts := make([]string, expected)

	// Look for "--- Result N" or "Result N:" patterns.
	lines := strings.Split(response, "\n")
	currentPart := -1
	var currentLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for result markers.
		matched := false
		for i := 0; i < expected; i++ {
			markers := []string{
				fmt.Sprintf("--- Result %d", i+1),
				fmt.Sprintf("Result %d:", i+1),
				fmt.Sprintf("**Result %d", i+1),
			}
			for _, m := range markers {
				if strings.HasPrefix(trimmed, m) {
					// Save previous part.
					if currentPart >= 0 && currentPart < expected {
						parts[currentPart] = strings.TrimSpace(strings.Join(currentLines, "\n"))
					}
					currentPart = i
					currentLines = nil
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		if !matched && currentPart >= 0 {
			currentLines = append(currentLines, line)
		}
	}

	// Save last part.
	if currentPart >= 0 && currentPart < expected {
		parts[currentPart] = strings.TrimSpace(strings.Join(currentLines, "\n"))
	}

	// If no markers found, return the whole response as part 0.
	allEmpty := true
	for _, p := range parts {
		if p != "" {
			allEmpty = false
			break
		}
	}
	if allEmpty && expected == 1 {
		parts[0] = strings.TrimSpace(response)
	}

	return parts
}
