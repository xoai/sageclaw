// Package tokenizer provides token counting using tiktoken-go.
// Uses cl100k_base encoding as a universal approximation (~10% variance
// across Anthropic/Gemini/OpenAI models, acceptable for budget decisions).
package tokenizer

import (
	"sync"

	"github.com/pkoukk/tiktoken-go"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// Counter counts tokens in text and messages.
type Counter struct {
	enc *tiktoken.Tiktoken
}

var (
	globalCounter *Counter
	once          sync.Once
	initErr       error
)

// Get returns the global token counter singleton.
// Thread-safe, initialized once on first call.
func Get() (*Counter, error) {
	once.Do(func() {
		enc, err := tiktoken.GetEncoding("cl100k_base")
		if err != nil {
			initErr = err
			return
		}
		globalCounter = &Counter{enc: enc}
	})
	return globalCounter, initErr
}

// Count returns the token count for a text string.
func (c *Counter) Count(text string) int {
	if c == nil || c.enc == nil {
		return len(text) / 4 // Fallback heuristic.
	}
	return len(c.enc.Encode(text, nil, nil))
}

// CountMessages returns the total token count for a slice of messages.
// Accounts for message framing overhead (~4 tokens per message for role/separators).
func (c *Counter) CountMessages(msgs []canonical.Message) int {
	total := 0
	for _, msg := range msgs {
		total += 4 // Role + framing overhead.
		for _, content := range msg.Content {
			switch content.Type {
			case "text":
				total += c.Count(content.Text)
			case "tool_use":
				if content.ToolCall != nil {
					total += c.Count(content.ToolCall.Name)
					if content.ToolCall.Input != nil {
						total += c.Count(string(content.ToolCall.Input))
					}
				}
			case "tool_result":
				if content.ToolResult != nil {
					total += c.Count(content.ToolResult.Content)
				} else {
					total += c.Count(content.Text)
				}
			}
		}
	}
	total += 2 // Start/end tokens.
	return total
}

// ContentText extracts all text content from a message's content blocks.
func ContentText(msg canonical.Message) string {
	var text string
	for _, c := range msg.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return text
}
