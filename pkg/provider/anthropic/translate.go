package anthropic

import (
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// --- Request translation: canonical → Anthropic ---

type apiRequest struct {
	Model     string       `json:"model"`
	Messages  []apiMessage `json:"messages"`
	System    any          `json:"system,omitempty"`
	Tools     []apiTool    `json:"tools,omitempty"`
	MaxTokens int          `json:"max_tokens"`
	Stream    bool         `json:"stream,omitempty"`
}

type apiMessage struct {
	Role    string     `json:"role"`
	Content apiContent `json:"content"`
}

// apiContent is either a string or array of content blocks.
type apiContent = any

type apiTextBlock struct {
	Type         string       `json:"type"`
	Text         string       `json:"text"`
	CacheControl *cacheCtrl   `json:"cache_control,omitempty"`
}

type apiToolUseBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type apiToolResultBlock struct {
	Type       string `json:"type"`
	ToolUseID  string `json:"tool_use_id"`
	Content    string `json:"content"`
	IsError    bool   `json:"is_error,omitempty"`
}

type apiImageBlock struct {
	Type   string         `json:"type"`
	Source apiImageSource `json:"source"`
}

type apiImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type apiTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	CacheControl *cacheCtrl     `json:"cache_control,omitempty"`
}

type cacheCtrl struct {
	Type string `json:"type"`
}

type apiSystemBlock struct {
	Type         string     `json:"type"`
	Text         string     `json:"text"`
	CacheControl *cacheCtrl `json:"cache_control,omitempty"`
}

// ToAPIRequest converts a canonical request to an Anthropic API request.
func ToAPIRequest(req *canonical.Request, enableCache bool) ([]byte, error) {
	ar := apiRequest{
		Model:     req.Model,
		MaxTokens: req.MaxTokens,
		Stream:    req.Stream,
	}

	if ar.MaxTokens == 0 {
		ar.MaxTokens = 8192
	}

	// System prompt with optional caching.
	if req.System != "" {
		block := apiSystemBlock{Type: "text", Text: req.System}
		if enableCache {
			block.CacheControl = &cacheCtrl{Type: "ephemeral"}
		}
		ar.System = []apiSystemBlock{block}
	}

	// Tools with optional caching on the last tool.
	for i, t := range req.Tools {
		at := apiTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
		if enableCache && i == len(req.Tools)-1 {
			at.CacheControl = &cacheCtrl{Type: "ephemeral"}
		}
		ar.Tools = append(ar.Tools, at)
	}

	// Messages — with cache breakpoints on stable turns.
	// Cache the first major turn boundary to avoid re-tokenizing conversation history.
	// Anthropic allows up to 4 cache breakpoints per request.
	// We use: 1 on system, 1 on last tool, 1-2 on conversation turns.
	turnCacheApplied := 0
	maxTurnCaches := 2
	// Place cache breakpoints at ~25% and ~50% of conversation history.
	cachePoints := map[int]bool{}
	if enableCache && len(req.Messages) >= 6 {
		cachePoints[len(req.Messages)/4] = true
		cachePoints[len(req.Messages)/2] = true
	}

	for msgIdx, msg := range req.Messages {
		var textBlocks []any
		var toolUseBlocks []any
		var toolResultBlocks []any

		for _, c := range msg.Content {
			switch c.Type {
			case "text":
				if c.Text == "" {
					continue
				}
				tb := apiTextBlock{Type: "text", Text: c.Text}
				if enableCache && cachePoints[msgIdx] && turnCacheApplied < maxTurnCaches {
					tb.CacheControl = &cacheCtrl{Type: "ephemeral"}
					turnCacheApplied++
				}
				textBlocks = append(textBlocks, tb)
			case "tool_call":
				if c.ToolCall != nil {
					toolUseBlocks = append(toolUseBlocks, apiToolUseBlock{
						Type:  "tool_use",
						ID:    c.ToolCall.ID,
						Name:  c.ToolCall.Name,
						Input: c.ToolCall.Input,
					})
				}
			case "tool_result":
				if c.ToolResult != nil {
					toolResultBlocks = append(toolResultBlocks, apiToolResultBlock{
						Type:      "tool_result",
						ToolUseID: c.ToolResult.ToolCallID,
						Content:   c.ToolResult.Content,
						IsError:   c.ToolResult.IsError,
					})
				}
			case "image":
				if c.Source != nil {
					textBlocks = append(textBlocks, apiImageBlock{
						Type: "image",
						Source: apiImageSource{
							Type:      c.Source.Type,
							MediaType: c.Source.MediaType,
							Data:      c.Source.Data,
						},
					})
				}
			case "audio":
				text := "[Voice message]"
				if c.Audio != nil && c.Audio.Transcript != "" {
					text = c.Audio.Transcript
				}
				textBlocks = append(textBlocks, apiTextBlock{Type: "text", Text: text})
			}
		}

		// Anthropic structural rules:
		// - tool_use blocks go in "assistant" messages
		// - tool_result blocks go in "user" messages
		// - tool_result must follow the assistant message with matching tool_use
		// We split mixed messages into separate properly-roled messages.

		if len(toolUseBlocks) > 0 {
			// Assistant message: text + tool_use blocks.
			var blocks []any
			blocks = append(blocks, textBlocks...)
			blocks = append(blocks, toolUseBlocks...)
			ar.Messages = append(ar.Messages, apiMessage{Role: "assistant", Content: blocks})
		} else if len(toolResultBlocks) > 0 {
			// User message with tool_result blocks.
			// Emit any text blocks as a separate user message first if present.
			if len(textBlocks) > 0 {
				ar.Messages = append(ar.Messages, apiMessage{Role: "user", Content: makeContent(textBlocks)})
			}
			ar.Messages = append(ar.Messages, apiMessage{Role: "user", Content: any(toolResultBlocks)})
		} else if len(textBlocks) > 0 {
			// Pure text/image message — preserve original role.
			role := msg.Role
			if role == "tool" {
				role = "user" // Normalize "tool" role to "user" for Anthropic.
			}
			ar.Messages = append(ar.Messages, apiMessage{Role: role, Content: makeContent(textBlocks)})
		}
		// Skip messages with no translatable content.
	}

	// Post-process 1: Validate tool_use/tool_result adjacency.
	// Anthropic requires each tool_result to be in the message immediately
	// after the assistant message containing the matching tool_use.
	// Non-adjacent pairs (from cross-model history) are converted to plain text.
	ar.Messages = flattenNonAdjacentToolPairs(ar.Messages)

	// Post-process 2: Merge consecutive same-role messages.
	// Anthropic doesn't allow two consecutive "user" or "assistant" messages.
	ar.Messages = mergeConsecutiveRoles(ar.Messages)

	return json.Marshal(ar)
}

// makeContent converts a block slice to the appropriate content format.
// Single text block → string shorthand; otherwise → array.
func makeContent(blocks []any) apiContent {
	if len(blocks) == 1 {
		if tb, ok := blocks[0].(apiTextBlock); ok {
			return tb.Text
		}
	}
	return blocks
}

// mergeConsecutiveRoles merges consecutive messages with the same role.
// Anthropic rejects consecutive user or assistant messages.
func mergeConsecutiveRoles(msgs []apiMessage) []apiMessage {
	if len(msgs) <= 1 {
		return msgs
	}

	var merged []apiMessage
	for _, msg := range msgs {
		if len(merged) > 0 && merged[len(merged)-1].Role == msg.Role {
			// Merge into previous message — both must become arrays.
			prev := &merged[len(merged)-1]
			prevBlocks := toBlockSlice(prev.Content)
			newBlocks := toBlockSlice(msg.Content)
			prev.Content = append(prevBlocks, newBlocks...)
		} else {
			merged = append(merged, msg)
		}
	}
	return merged
}

// toBlockSlice normalizes content (string or []any) to []any.
func toBlockSlice(content apiContent) []any {
	switch v := content.(type) {
	case string:
		return []any{apiTextBlock{Type: "text", Text: v}}
	case []any:
		return v
	default:
		return []any{content}
	}
}

// flattenNonAdjacentToolPairs converts non-adjacent tool_use/tool_result pairs
// into plain text. Anthropic requires tool_result in the message immediately
// following the assistant message with the matching tool_use. Historical tool
// pairs from other providers may not satisfy this — converting them to text
// preserves the context without breaking the protocol.
func flattenNonAdjacentToolPairs(msgs []apiMessage) []apiMessage {
	// Build a map: tool_use ID → message index where it appears.
	toolUseIndex := make(map[string]int)
	for i, msg := range msgs {
		if msg.Role != "assistant" {
			continue
		}
		for _, block := range toBlockSlice(msg.Content) {
			if tu, ok := block.(apiToolUseBlock); ok {
				toolUseIndex[tu.ID] = i
			}
		}
	}

	// Find tool_result blocks that are NOT in the message immediately after their tool_use.
	orphanedResults := make(map[string]bool) // tool_use_id → orphaned
	orphanedUses := make(map[string]bool)
	for i, msg := range msgs {
		for _, block := range toBlockSlice(msg.Content) {
			if tr, ok := block.(apiToolResultBlock); ok {
				useIdx, exists := toolUseIndex[tr.ToolUseID]
				if !exists || useIdx+1 != i {
					// tool_result is not adjacent to its tool_use — mark both for flattening.
					orphanedResults[tr.ToolUseID] = true
					orphanedUses[tr.ToolUseID] = true
				}
			}
		}
	}

	if len(orphanedResults) == 0 {
		return msgs // All pairs are adjacent — no changes needed.
	}

	// Rebuild messages, converting orphaned tool_use/tool_result to text.
	var result []apiMessage
	for _, msg := range msgs {
		blocks := toBlockSlice(msg.Content)
		var newBlocks []any

		for _, block := range blocks {
			switch b := block.(type) {
			case apiToolUseBlock:
				if orphanedUses[b.ID] {
					// Convert to text: "[Used tool: name]"
					newBlocks = append(newBlocks, apiTextBlock{
						Type: "text",
						Text: fmt.Sprintf("[Used tool: %s]", b.Name),
					})
				} else {
					newBlocks = append(newBlocks, b)
				}
			case apiToolResultBlock:
				if orphanedResults[b.ToolUseID] {
					// Convert to text: "[Tool result: content]"
					content := b.Content
					if len(content) > 500 {
						content = content[:500] + "..."
					}
					newBlocks = append(newBlocks, apiTextBlock{
						Type: "text",
						Text: fmt.Sprintf("[Tool result: %s]", content),
					})
				} else {
					newBlocks = append(newBlocks, b)
				}
			default:
				newBlocks = append(newBlocks, block)
			}
		}

		if len(newBlocks) > 0 {
			result = append(result, apiMessage{Role: msg.Role, Content: makeContent(newBlocks)})
		}
	}

	return result
}

// --- Response translation: Anthropic → canonical ---

type apiResponse struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Model      string          `json:"model"`
	StopReason string          `json:"stop_reason"`
	Usage      apiUsage        `json:"usage"`
}

type apiUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type apiContentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text,omitempty"`
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
}

// FromAPIResponse converts an Anthropic API response to canonical format.
func FromAPIResponse(data []byte) (*canonical.Response, error) {
	var ar apiResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	var blocks []apiContentBlock
	if err := json.Unmarshal(ar.Content, &blocks); err != nil {
		return nil, fmt.Errorf("unmarshaling content blocks: %w", err)
	}

	var content []canonical.Content
	for _, b := range blocks {
		switch b.Type {
		case "text":
			content = append(content, canonical.Content{Type: "text", Text: b.Text})
		case "tool_use":
			content = append(content, canonical.Content{
				Type: "tool_call",
				ToolCall: &canonical.ToolCall{
					ID:    b.ID,
					Name:  b.Name,
					Input: b.Input,
				},
			})
		}
	}

	return &canonical.Response{
		ID: ar.ID,
		Messages: []canonical.Message{
			{Role: ar.Role, Content: content},
		},
		Usage: canonical.Usage{
			InputTokens:   ar.Usage.InputTokens,
			OutputTokens:  ar.Usage.OutputTokens,
			CacheCreation: ar.Usage.CacheCreationInputTokens,
			CacheRead:     ar.Usage.CacheReadInputTokens,
		},
		StopReason: ar.StopReason,
	}, nil
}
