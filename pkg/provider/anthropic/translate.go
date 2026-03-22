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
		am := apiMessage{Role: msg.Role}
		var blocks []any

		for _, c := range msg.Content {
			switch c.Type {
			case "text":
				tb := apiTextBlock{Type: "text", Text: c.Text}
				// Add cache breakpoint on designated conversation turns.
				if enableCache && cachePoints[msgIdx] && turnCacheApplied < maxTurnCaches {
					tb.CacheControl = &cacheCtrl{Type: "ephemeral"}
					turnCacheApplied++
				}
				blocks = append(blocks, tb)
			case "tool_call":
				if c.ToolCall != nil {
					blocks = append(blocks, apiToolUseBlock{
						Type:  "tool_use",
						ID:    c.ToolCall.ID,
						Name:  c.ToolCall.Name,
						Input: c.ToolCall.Input,
					})
				}
			case "tool_result":
				if c.ToolResult != nil {
					blocks = append(blocks, apiToolResultBlock{
						Type:      "tool_result",
						ToolUseID: c.ToolResult.ToolCallID,
						Content:   c.ToolResult.Content,
						IsError:   c.ToolResult.IsError,
					})
				}
			case "image":
				if c.Source != nil {
					blocks = append(blocks, apiImageBlock{
						Type: "image",
						Source: apiImageSource{
							Type:      c.Source.Type,
							MediaType: c.Source.MediaType,
							Data:      c.Source.Data,
						},
					})
				}
			}
		}

		// If only one text block, use string shorthand.
		if len(blocks) == 1 {
			if tb, ok := blocks[0].(apiTextBlock); ok {
				am.Content = tb.Text
			} else {
				am.Content = blocks
			}
		} else {
			am.Content = blocks
		}

		ar.Messages = append(ar.Messages, am)
	}

	return json.Marshal(ar)
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
