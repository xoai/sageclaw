package context

import "time"

// Constants for tool result summarization. Used by the lazy summary path
// in snip (generates a summary only when about to snip a result).
const (
	toolSummaryTimeout = 10 * time.Second
	toolSummaryPrompt  = "One sentence: what did this tool do and what was the result? Be specific about file paths, counts, or errors."
)
