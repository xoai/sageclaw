package openai

import (
	"encoding/json"
	"testing"
)

func TestFromOpenAIResponse_WithTokenDetails(t *testing.T) {
	// Simulates an OpenAI response with prompt_tokens_details and completion_tokens_details.
	body := []byte(`{
		"id": "chatcmpl-test",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hello"}, "finish_reason": "stop"}],
		"usage": {
			"prompt_tokens": 1000,
			"completion_tokens": 500,
			"total_tokens": 1500,
			"prompt_tokens_details": {
				"cached_tokens": 800
			},
			"completion_tokens_details": {
				"reasoning_tokens": 200
			}
		}
	}`)

	resp, err := FromOpenAIResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if resp.Usage.InputTokens != 1000 {
		t.Errorf("InputTokens = %d, want 1000", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 500 {
		t.Errorf("OutputTokens = %d, want 500", resp.Usage.OutputTokens)
	}
	if resp.Usage.CacheRead != 800 {
		t.Errorf("CacheRead = %d, want 800 (from prompt_tokens_details.cached_tokens)", resp.Usage.CacheRead)
	}
	if resp.Usage.ThinkingTokens != 200 {
		t.Errorf("ThinkingTokens = %d, want 200 (from completion_tokens_details.reasoning_tokens)", resp.Usage.ThinkingTokens)
	}
}

func TestFromOpenAIResponse_WithoutTokenDetails(t *testing.T) {
	// Older response format without detail breakdowns.
	body := []byte(`{
		"id": "chatcmpl-old",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "Hi"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 50, "completion_tokens": 10, "total_tokens": 60}
	}`)

	resp, err := FromOpenAIResponse(body)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if resp.Usage.CacheRead != 0 {
		t.Errorf("CacheRead = %d, want 0 (no details)", resp.Usage.CacheRead)
	}
	if resp.Usage.ThinkingTokens != 0 {
		t.Errorf("ThinkingTokens = %d, want 0 (no details)", resp.Usage.ThinkingTokens)
	}
}

func TestOpenAIUsageToCanonical(t *testing.T) {
	tests := []struct {
		name           string
		usage          chatUsage
		wantCacheRead  int
		wantThinking   int
	}{
		{
			name: "full_details",
			usage: chatUsage{
				PromptTokens: 1000, CompletionTokens: 500,
				PromptTokensDetails:     &promptTokensDetails{CachedTokens: 600},
				CompletionTokensDetails: &completionTokensDetails{ReasoningTokens: 150},
			},
			wantCacheRead: 600, wantThinking: 150,
		},
		{
			name:  "no_details",
			usage: chatUsage{PromptTokens: 100, CompletionTokens: 50},
			wantCacheRead: 0, wantThinking: 0,
		},
		{
			name: "only_cache",
			usage: chatUsage{
				PromptTokens: 500, CompletionTokens: 100,
				PromptTokensDetails: &promptTokensDetails{CachedTokens: 400},
			},
			wantCacheRead: 400, wantThinking: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := openAIUsageToCanonical(tt.usage)
			if u.CacheRead != tt.wantCacheRead {
				t.Errorf("CacheRead = %d, want %d", u.CacheRead, tt.wantCacheRead)
			}
			if u.ThinkingTokens != tt.wantThinking {
				t.Errorf("ThinkingTokens = %d, want %d", u.ThinkingTokens, tt.wantThinking)
			}
		})
	}
}

func TestStreamChunk_UsageWithDetails(t *testing.T) {
	// Simulate a streaming usage chunk with details.
	data := `{"id":"test","choices":[],"usage":{"prompt_tokens":2000,"completion_tokens":800,"total_tokens":2800,"prompt_tokens_details":{"cached_tokens":1500},"completion_tokens_details":{"reasoning_tokens":300}}}`

	var chunk streamChunk
	if err := json.Unmarshal([]byte(data), &chunk); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if chunk.Usage == nil {
		t.Fatal("usage should not be nil")
	}
	if chunk.Usage.PromptTokensDetails == nil {
		t.Fatal("prompt_tokens_details should not be nil")
	}
	if chunk.Usage.PromptTokensDetails.CachedTokens != 1500 {
		t.Errorf("cached_tokens = %d, want 1500", chunk.Usage.PromptTokensDetails.CachedTokens)
	}
	if chunk.Usage.CompletionTokensDetails == nil {
		t.Fatal("completion_tokens_details should not be nil")
	}
	if chunk.Usage.CompletionTokensDetails.ReasoningTokens != 300 {
		t.Errorf("reasoning_tokens = %d, want 300", chunk.Usage.CompletionTokensDetails.ReasoningTokens)
	}
}
