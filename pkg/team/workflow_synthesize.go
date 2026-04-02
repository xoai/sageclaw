package team

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaxResultChars is the maximum total characters for all task results before truncation.
const MaxResultChars = 50000

// FormatWorkflowResults formats task results as a [Workflow Results] block for LLM injection.
func FormatWorkflowResults(resultsJSON string) string {
	var results []TaskResult
	if err := json.Unmarshal([]byte(resultsJSON), &results); err != nil || len(results) == 0 {
		return ""
	}

	// Calculate total size and truncate if needed.
	totalSize := 0
	for _, r := range results {
		totalSize += len(r.Result) + len(r.Error) + len(r.Title) + 50 // overhead
	}

	truncate := totalSize > MaxResultChars
	budgetPerTask := MaxResultChars / len(results)

	var sb strings.Builder
	sb.WriteString("[Workflow Results]\n")

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("Task %d \"%s\" (%s):\n", i+1, r.Title, r.Status))
		switch r.Status {
		case "completed":
			content := r.Result
			if truncate && len(content) > budgetPerTask {
				content = content[:budgetPerTask] + fmt.Sprintf("\n[truncated — original was %d chars]", len(r.Result))
			}
			sb.WriteString(fmt.Sprintf("  <result>%s</result>\n", content))
		case "failed":
			sb.WriteString(fmt.Sprintf("  <error>%s</error>\n", r.Error))
		case "cancelled":
			sb.WriteString("  <cancelled/>\n")
		}
	}

	sb.WriteString("[/Workflow Results]\n\n")
	sb.WriteString("Synthesize the results above into a coherent response for the user. ")
	sb.WriteString("Reference all completed task outputs. Note any tasks that failed or were cancelled.")

	return sb.String()
}

// FormatWorkflowWakeMessage creates the system message used to wake the lead for synthesis.
func FormatWorkflowWakeMessage(resultsJSON string) string {
	formatted := FormatWorkflowResults(resultsJSON)
	if formatted == "" {
		return "All delegated tasks have completed but no results were collected."
	}
	return formatted
}
