package context

import (
	"github.com/xoai/sageclaw/pkg/canonical"
)

// EnsureAnnotations lazily initializes annotations on a message.
func EnsureAnnotations(msg *canonical.Message) *canonical.MessageAnnotations {
	if msg.Annotations == nil {
		msg.Annotations = &canonical.MessageAnnotations{}
	}
	return msg.Annotations
}

// AnnotateIteration stamps new messages (those without annotations) with the
// current loop iteration number. Called at the start of each iteration.
func AnnotateIteration(msgs []canonical.Message, iteration int) {
	for i := range msgs {
		ann := EnsureAnnotations(&msgs[i])
		if ann.Iteration == 0 {
			ann.Iteration = iteration
		}
	}
}

// EstimateTokens computes and caches token count for a message.
// Uses a simple heuristic: ~4 characters per token.
func EstimateTokens(msg *canonical.Message) int {
	ann := EnsureAnnotations(msg)
	if ann.TokenEstimate > 0 {
		return ann.TokenEstimate
	}

	chars := 0
	for _, c := range msg.Content {
		chars += len(c.Text)
		if c.Thinking != "" {
			chars += len(c.Thinking)
		}
		if c.ToolCall != nil {
			chars += len(c.ToolCall.Name) + len(c.ToolCall.Input)
		}
		if c.ToolResult != nil {
			chars += len(c.ToolResult.Content)
		}
	}

	estimate := chars/4 + 4 // +4 for role/structural tokens
	ann.TokenEstimate = estimate
	return estimate
}

// IsSnippable returns true if ALL tool_result content blocks in the message
// reference read-only tools. Returns false if any block is non-read-only,
// or if the message has no tool results.
// toolNameByCallID maps ToolCallID → tool name (built from preceding assistant messages).
// readOnly maps tool name → true for read-only tools.
func IsSnippable(msg canonical.Message, toolNameByCallID map[string]string, readOnly map[string]bool) bool {
	hasToolResult := false
	for _, c := range msg.Content {
		if c.ToolResult != nil {
			hasToolResult = true
			name := toolNameByCallID[c.ToolResult.ToolCallID]
			if !readOnly[name] {
				return false
			}
		}
	}
	return hasToolResult
}

// CopyMessageWithAnnotations creates a shallow copy of a message,
// preserving the Annotations pointer. Use when modifying content
// without mutating the original.
func CopyMessageWithAnnotations(msg canonical.Message) canonical.Message {
	cp := canonical.Message{
		Role:        msg.Role,
		Content:     make([]canonical.Content, len(msg.Content)),
		Annotations: msg.Annotations,
	}
	copy(cp.Content, msg.Content)
	return cp
}
