package tool

import (
	"context"
	"fmt"
	"strings"
)

// tailKeywords are indicators of important content in the tail of command output
// (error summaries, test results, exit codes). Used by capOutputHeadTail.
var tailKeywords = []string{
	"error", "failed", "panic", "FAIL", "exit code",
	"total", "summary", "fatal", "traceback", "stack trace", "result",
}

// adaptiveMax computes an iteration-aware character budget using proportional
// step-function scaling. The reduction is proportional to defaultMax, so this
// works correctly for any default value.
//
//   - 0-49% iteration: defaultMax (full budget)
//   - 50-74%: defaultMax / 2
//   - 75%+: defaultMax / 4
//   - Floor: 2000 chars minimum
func adaptiveMax(ctx context.Context, defaultMax int) int {
	if iter, ok := GetIteration(ctx); ok && iter.Max > 0 {
		pct := float64(iter.Current) / float64(iter.Max)
		switch {
		case pct >= 0.75:
			if half := defaultMax / 4; half < defaultMax {
				defaultMax = half
			}
		case pct >= 0.50:
			if half := defaultMax / 2; half < defaultMax {
				defaultMax = half
			}
		}
	}
	if defaultMax < 2000 {
		defaultMax = 2000
	}
	return defaultMax
}

// capOutput truncates text to maxChars at a newline boundary and appends a
// truncation notice. Returns text unchanged if it fits within the budget.
func capOutput(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	totalLen := len(text)

	// Find last newline before the cap.
	cut := maxChars
	if idx := strings.LastIndex(text[:cut], "\n"); idx > 0 {
		cut = idx
	}

	return text[:cut] + fmt.Sprintf("\n... [truncated: %d chars total, showing first %d]", totalLen, cut)
}

// capOutputHeadTail truncates with a head+tail split when the tail contains
// important keywords (error summaries, test results). If the tail is not
// important, falls back to head-only truncation via capOutput.
//
// Budget split when tail is important: 70% head, 30% tail (tail max 4K chars).
func capOutputHeadTail(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	totalLen := len(text)

	// Check if tail contains important keywords.
	tailSize := maxChars * 30 / 100
	if tailSize > 4000 {
		tailSize = 4000
	}
	tail := text[len(text)-tailSize:]
	tailLower := strings.ToLower(tail)

	hasImportant := false
	for _, kw := range tailKeywords {
		if strings.Contains(tailLower, strings.ToLower(kw)) {
			hasImportant = true
			break
		}
	}

	if !hasImportant {
		return capOutput(text, maxChars)
	}

	// Head+tail split.
	headSize := maxChars - tailSize
	separator := fmt.Sprintf("\n\n... [truncated: %d chars total — showing head + tail] ...\n\n", totalLen)
	headSize -= len(separator)
	if headSize < 500 {
		headSize = 500
	}

	// Cut head at newline boundary.
	head := text[:headSize]
	if idx := strings.LastIndex(head, "\n"); idx > 0 {
		head = head[:idx]
	}

	// Cut tail at newline boundary (from the start of the tail section).
	tailStart := len(text) - tailSize
	if idx := strings.Index(text[tailStart:], "\n"); idx >= 0 && idx < tailSize {
		tailStart += idx + 1
	}

	return head + separator + text[tailStart:]
}
