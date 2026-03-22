package pipeline

import (
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// IntentType represents the classified intent of a message.
type IntentType string

const (
	IntentCommand IntentType = "command"
	IntentAgent   IntentType = "agent"
)

// IntentResult holds the classification result.
type IntentResult struct {
	Type    IntentType
	Command string // For command intents.
}

// command keywords (Tier 1: messages < 60 chars).
var commandPatterns = map[string]string{
	"/start":  "start",
	"/help":   "help",
	"/stop":   "stop",
	"/status": "status",
}

// ClassifyIntent performs two-tier intent classification.
// v0.1: single agent, so Tier 2 always returns "agent".
func ClassifyIntent(msgs []canonical.Message) IntentResult {
	if len(msgs) == 0 {
		return IntentResult{Type: IntentAgent}
	}

	// Get last user text.
	lastMsg := msgs[len(msgs)-1]
	text := ""
	for _, c := range lastMsg.Content {
		if c.Type == "text" {
			text = c.Text
			break
		}
	}

	text = strings.TrimSpace(text)

	// Tier 1: Keyword fast-path for short messages.
	if len(text) < 60 {
		lower := strings.ToLower(text)
		for pattern, cmd := range commandPatterns {
			if lower == pattern || strings.HasPrefix(lower, pattern+" ") {
				return IntentResult{Type: IntentCommand, Command: cmd}
			}
		}
	}

	// Tier 2: v0.1 — single agent, always route to agent loop.
	return IntentResult{Type: IntentAgent}
}
