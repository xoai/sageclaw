package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestNew_Defaults(t *testing.T) {
	c := New(Config{Name: "test"})
	if c.Name() != "test" {
		t.Errorf("expected name=test, got %q", c.Name())
	}
}

func TestChat_BasicResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify headers.
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("expected auth header, got %q", r.Header.Get("Authorization"))
		}
		if r.Header.Get("X-Custom") != "value" {
			t.Errorf("expected custom header")
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-1",
			"choices": [{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],
			"usage": {"prompt_tokens":5,"completion_tokens":3}
		}`)
	}))
	defer srv.Close()

	c := New(Config{
		Name:    "test-provider",
		BaseURL: srv.URL,
		APIKey:  "test-key",
		Headers: map[string]string{"X-Custom": "value"},
	})

	resp, err := c.Chat(context.Background(), &canonical.Request{
		Model:     "test-model",
		MaxTokens: 100,
		Messages:  []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}
	if resp.Messages[0].Content[0].Text != "Hello!" {
		t.Errorf("expected Hello!, got %q", resp.Messages[0].Content[0].Text)
	}
}

func TestChat_DeepSeekThinking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "chatcmpl-1",
			"choices": [{"index":0,"message":{"role":"assistant","content":"The answer is 42.","reasoning_content":"Let me think step by step..."},"finish_reason":"stop"}],
			"usage": {"prompt_tokens":5,"completion_tokens":20}
		}`)
	}))
	defer srv.Close()

	c := New(Config{
		Name:    "deepseek",
		BaseURL: srv.URL,
		Quirks:  Quirks{ThinkingField: "reasoning_content"},
	})

	resp, err := c.Chat(context.Background(), &canonical.Request{
		Model:     "deepseek-reasoner",
		MaxTokens: 100,
		Messages:  []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "what is 6*7?"}}}},
	})
	if err != nil {
		t.Fatalf("Chat error: %v", err)
	}

	content := resp.Messages[0].Content
	if len(content) != 2 {
		t.Fatalf("expected 2 content blocks (thinking + text), got %d", len(content))
	}
	if content[0].Type != "thinking" {
		t.Errorf("expected thinking block first, got %s", content[0].Type)
	}
	if content[0].Thinking != "Let me think step by step..." {
		t.Errorf("wrong thinking content: %q", content[0].Thinking)
	}
	if content[1].Text != "The answer is 42." {
		t.Errorf("wrong text: %q", content[1].Text)
	}
}

func TestChatStream_Complete(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"role":"assistant","content":"Hello "},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"world!"},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	c := New(Config{Name: "test", BaseURL: srv.URL})

	stream, err := c.ChatStream(context.Background(), &canonical.Request{
		Model:    "test-model",
		Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	var texts []string
	var stopReason string
	for ev := range stream {
		if ev.Type == "content_delta" && ev.Delta != nil {
			texts = append(texts, ev.Delta.Text)
		}
		if ev.Type == "done" {
			stopReason = ev.StopReason
		}
	}

	if len(texts) != 2 || texts[0] != "Hello " || texts[1] != "world!" {
		t.Errorf("unexpected texts: %v", texts)
	}
	if stopReason != "end_turn" {
		t.Errorf("expected end_turn, got %q", stopReason)
	}
}

func TestChatStream_DeepSeekThinking(t *testing.T) {
	sseData := `data: {"id":"chatcmpl-1","choices":[{"delta":{"reasoning_content":"Think..."},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{"content":"Answer."},"finish_reason":null}]}

data: {"id":"chatcmpl-1","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	c := New(Config{
		Name:    "deepseek",
		BaseURL: srv.URL,
		Quirks:  Quirks{ThinkingField: "reasoning_content"},
	})

	stream, err := c.ChatStream(context.Background(), &canonical.Request{
		Model:    "deepseek-reasoner",
		Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	hasThinking := false
	hasText := false
	for ev := range stream {
		if ev.Type == "content_delta" && ev.Delta != nil {
			if ev.Delta.Type == "thinking" {
				hasThinking = true
			}
			if ev.Delta.Type == "text" {
				hasText = true
			}
		}
	}

	if !hasThinking {
		t.Error("expected thinking content from reasoning_content field")
	}
	if !hasText {
		t.Error("expected text content")
	}
}

func TestChatStream_ToolCallAccumulation(t *testing.T) {
	sseData := strings.Join([]string{
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search","arguments":""}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-1","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"test\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`data: [DONE]`,
	}, "\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	// Use quirks stream path (ThinkingField set).
	c := New(Config{
		Name:    "deepseek",
		BaseURL: srv.URL,
		Quirks:  Quirks{ThinkingField: "reasoning_content"},
	})

	stream, err := c.ChatStream(context.Background(), &canonical.Request{
		Model:    "deepseek-chat",
		Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "search"}}}},
	})
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	var toolCalls []*canonical.ToolCall
	for ev := range stream {
		if ev.Type == "tool_call" && ev.Delta != nil && ev.Delta.ToolCall != nil {
			toolCalls = append(toolCalls, ev.Delta.ToolCall)
		}
	}

	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "search" {
		t.Errorf("expected name=search, got %q", toolCalls[0].Name)
	}
	if string(toolCalls[0].Input) != `{"q":"test"}` {
		t.Errorf("expected accumulated args, got %q", string(toolCalls[0].Input))
	}
}

func TestQuirks_StripSystemRole(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer srv.Close()

	c := New(Config{
		Name:    "test",
		BaseURL: srv.URL,
		Quirks:  Quirks{StripSystemRole: true},
	})

	c.Chat(context.Background(), &canonical.Request{
		Model:     "test",
		System:    "You are helpful.",
		MaxTokens: 100,
		Messages:  []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}}},
	})

	// Check that system message was converted to user role.
	msgs := receivedBody["messages"].([]any)
	firstMsg := msgs[0].(map[string]any)
	if firstMsg["role"] != "user" {
		t.Errorf("expected system role stripped to user, got %v", firstMsg["role"])
	}
}

func TestQuirks_MaxTokensField(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"1","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer srv.Close()

	c := New(Config{
		Name:    "test",
		BaseURL: srv.URL,
		Quirks:  Quirks{MaxTokensField: "max_completion_tokens"},
	})

	c.Chat(context.Background(), &canonical.Request{
		Model:     "test",
		MaxTokens: 2048,
		Messages:  []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}}},
	})

	if _, ok := receivedBody["max_tokens"]; ok {
		t.Error("max_tokens should be removed")
	}
	if receivedBody["max_completion_tokens"].(float64) != 2048 {
		t.Errorf("expected max_completion_tokens=2048, got %v", receivedBody["max_completion_tokens"])
	}
}

func TestKnownProvider(t *testing.T) {
	cfg := KnownProvider("deepseek")
	if cfg == nil {
		t.Fatal("expected deepseek config")
	}
	if cfg.Quirks.ThinkingField != "reasoning_content" {
		t.Errorf("expected ThinkingField=reasoning_content, got %q", cfg.Quirks.ThinkingField)
	}

	cfg = KnownProvider("openrouter")
	if cfg == nil {
		t.Fatal("expected openrouter config")
	}
	if cfg.Headers["HTTP-Referer"] != "https://sageclaw.dev" {
		t.Error("expected HTTP-Referer header for openrouter")
	}

	if KnownProvider("nonexistent") != nil {
		t.Error("expected nil for unknown provider")
	}
}

func TestKnownProviderNames(t *testing.T) {
	names := KnownProviderNames()
	if len(names) < 5 {
		t.Errorf("expected at least 5 known providers, got %d", len(names))
	}

	has := func(name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	for _, want := range []string{"openrouter", "deepseek", "ollama", "groq"} {
		if !has(want) {
			t.Errorf("expected %q in known providers", want)
		}
	}
}

func TestHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.WriteHeader(200)
			fmt.Fprint(w, `{"data":[]}`)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := New(Config{Name: "test", BaseURL: srv.URL})
	if !c.Healthy(context.Background()) {
		t.Error("expected healthy")
	}
}

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"model-a","owned_by":"test"},{"id":"model-b","owned_by":"test"}]}`)
	}))
	defer srv.Close()

	c := New(Config{Name: "mytest", BaseURL: srv.URL})
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Provider != "mytest" {
		t.Errorf("expected provider=mytest, got %q", models[0].Provider)
	}
	if models[0].ID != "mytest/model-a" {
		t.Errorf("expected ID=mytest/model-a, got %q", models[0].ID)
	}
}

// Ensure unused imports don't cause issues.
var _ = provider.StreamEvent{}
