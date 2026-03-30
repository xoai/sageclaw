package gemini

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestToGeminiRequest_SchemaCleanedForGemini(t *testing.T) {
	// MCP tools often have $ref/$defs which Gemini rejects.
	// toGeminiRequest must clean schemas before sending.
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
		Tools: []canonical.ToolDef{
			{
				Name:        "mcp_tool",
				Description: "A tool with complex schema",
				InputSchema: json.RawMessage(`{
					"type": "object",
					"$ref": "#/$defs/Params",
					"$defs": {"Params": {"type": "object"}},
					"additionalProperties": false,
					"properties": {
						"query": {
							"type": "string",
							"default": "test",
							"examples": ["foo"]
						}
					}
				}`),
			},
		},
	}

	gr := toGeminiRequest(req)

	if len(gr.Tools) == 0 || len(gr.Tools[0].FunctionDeclarations) == 0 {
		t.Fatal("expected tool declarations")
	}

	decl := gr.Tools[0].FunctionDeclarations[0]
	var schema map[string]any
	if err := json.Unmarshal(decl.Parameters, &schema); err != nil {
		t.Fatalf("failed to unmarshal cleaned schema: %v", err)
	}

	// Gemini-incompatible keys must be stripped.
	for _, key := range []string{"$ref", "$defs", "additionalProperties"} {
		if _, ok := schema[key]; ok {
			t.Errorf("expected %q to be stripped from Gemini tool schema", key)
		}
	}

	// Nested properties should also be cleaned.
	props := schema["properties"].(map[string]any)
	queryProp := props["query"].(map[string]any)
	for _, key := range []string{"default", "examples"} {
		if _, ok := queryProp[key]; ok {
			t.Errorf("expected %q stripped from nested property", key)
		}
	}

	// Type should be preserved.
	if schema["type"] != "object" {
		t.Errorf("expected type=object preserved, got %v", schema["type"])
	}
}

func TestToGeminiRequest_ToolCallRoundTrip(t *testing.T) {
	// Verify that tool calls with Meta (thought_signature) are
	// correctly serialized into Gemini functionCall parts.
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "do it"}}},
			{Role: "assistant", Content: []canonical.Content{
				{Type: "tool_call", ToolCall: &canonical.ToolCall{
					ID:    "call_search_0",
					Name:  "search",
					Input: json.RawMessage(`{"q":"test"}`),
					Meta:  map[string]string{"thought_signature": "sig_abc"},
				}},
			}},
			{Role: "user", Content: []canonical.Content{
				{Type: "tool_result", ToolResult: &canonical.ToolResult{
					ToolCallID: "call_search_0",
					Content:    "results here",
				}},
			}},
		},
	}

	gr := toGeminiRequest(req)

	// Find the model turn with functionCall.
	var fcPart *geminiPart
	for _, c := range gr.Contents {
		if c.Role == "model" {
			for i := range c.Parts {
				if c.Parts[i].FunctionCall != nil {
					fcPart = &c.Parts[i]
				}
			}
		}
	}

	if fcPart == nil {
		t.Fatal("expected functionCall part in model turn")
	}
	if fcPart.ThoughtSignature != "sig_abc" {
		t.Errorf("expected thought_signature=sig_abc, got %q", fcPart.ThoughtSignature)
	}
	if fcPart.FunctionCall.Name != "search" {
		t.Errorf("expected name=search, got %q", fcPart.FunctionCall.Name)
	}
}

func TestChatStream_EmitsCompleteToolCalls(t *testing.T) {
	// Gemini sends complete function calls in SSE chunks.
	// ChatStream must emit them via Delta.ToolCall (complete path),
	// not via ToolCallID/ToolName/ToolInput (delta path).
	sseData := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Let me search."}]}}]}

data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"web_search","args":{"query":"weather paris"}},"thoughtSignature":"sig_xyz"}]}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	client := NewClient("test-key", WithBaseURL(srv.URL), WithModel("gemini-2.0-flash"))
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "weather in paris"}}},
		},
		Stream: true,
	}

	stream, err := client.ChatStream(testCtx(t), req)
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	var events []provider.StreamEvent
	for ev := range stream {
		events = append(events, ev)
	}

	// Should have: content_delta (text), tool_call (complete), done.
	var toolCallEvent *provider.StreamEvent
	for i := range events {
		if events[i].Type == "tool_call" {
			toolCallEvent = &events[i]
		}
	}

	if toolCallEvent == nil {
		t.Fatal("expected tool_call event")
	}

	// Must use complete path (Delta.ToolCall), not delta path.
	if toolCallEvent.Delta == nil {
		t.Fatal("expected Delta in tool_call event")
	}
	if toolCallEvent.Delta.ToolCall == nil {
		t.Fatal("expected Delta.ToolCall (complete path), got delta path fields")
	}

	tc := toolCallEvent.Delta.ToolCall
	if tc.Name != "web_search" {
		t.Errorf("expected name=web_search, got %q", tc.Name)
	}
	if tc.Meta["thought_signature"] != "sig_xyz" {
		t.Errorf("expected thought_signature=sig_xyz, got %v", tc.Meta)
	}

	// Input should be valid JSON.
	var args map[string]any
	if err := json.Unmarshal(tc.Input, &args); err != nil {
		t.Fatalf("tool call input is not valid JSON: %v", err)
	}
	if args["query"] != "weather paris" {
		t.Errorf("expected query='weather paris', got %v", args["query"])
	}
}

func TestChatStream_MultipleToolCalls(t *testing.T) {
	// Test that multiple function calls in a single chunk all emit as complete.
	sseData := `data: {"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"search_a","args":{"q":"a"}},"thoughtSignature":"sig1"},{"functionCall":{"name":"search_b","args":{"q":"b"}},"thoughtSignature":"sig2"}]}}]}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	client := NewClient("test-key", WithBaseURL(srv.URL))
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "search both"}}},
		},
		Stream: true,
	}

	stream, err := client.ChatStream(testCtx(t), req)
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	var toolCalls []*canonical.ToolCall
	for ev := range stream {
		if ev.Type == "tool_call" && ev.Delta != nil && ev.Delta.ToolCall != nil {
			toolCalls = append(toolCalls, ev.Delta.ToolCall)
		}
	}

	if len(toolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(toolCalls))
	}
	if toolCalls[0].Name != "search_a" {
		t.Errorf("first tool call name: expected search_a, got %q", toolCalls[0].Name)
	}
	if toolCalls[1].Name != "search_b" {
		t.Errorf("second tool call name: expected search_b, got %q", toolCalls[1].Name)
	}
}

func TestChatStream_UsageMetadata(t *testing.T) {
	// Gemini sends usageMetadata in the last SSE chunk.
	sseData := `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hello!"}]}}],"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":5,"cachedContentTokenCount":2}}

data: [DONE]

`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, sseData)
	}))
	defer srv.Close()

	client := NewClient("test-key", WithBaseURL(srv.URL))
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hi"}}},
		},
		Stream: true,
	}

	stream, err := client.ChatStream(testCtx(t), req)
	if err != nil {
		t.Fatalf("ChatStream error: %v", err)
	}

	var usageEvent *provider.StreamEvent
	for ev := range stream {
		if ev.Type == "usage" {
			usageEvent = &ev
		}
	}

	if usageEvent == nil {
		t.Fatal("expected usage event from streaming")
	}
	if usageEvent.Usage.InputTokens != 10 {
		t.Errorf("expected InputTokens=10, got %d", usageEvent.Usage.InputTokens)
	}
	if usageEvent.Usage.OutputTokens != 5 {
		t.Errorf("expected OutputTokens=5, got %d", usageEvent.Usage.OutputTokens)
	}
	if usageEvent.Usage.CacheRead != 2 {
		t.Errorf("expected CacheRead=2, got %d", usageEvent.Usage.CacheRead)
	}
}

func TestToGeminiRequest_GoogleSearchGrounding(t *testing.T) {
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
		Options: map[string]any{"grounding": "google_search"},
	}

	gr := toGeminiRequest(req)

	found := false
	for _, td := range gr.Tools {
		if td.GoogleSearch != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected googleSearch tool declaration when grounding=google_search")
	}
}

func TestToGeminiRequest_CodeExecution(t *testing.T) {
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
		Options: map[string]any{"code_execution": true},
	}

	gr := toGeminiRequest(req)

	found := false
	for _, td := range gr.Tools {
		if td.CodeExecution != nil {
			found = true
		}
	}
	if !found {
		t.Error("expected codeExecution tool declaration when code_execution=true")
	}
}

func TestToGeminiRequest_CachedContent(t *testing.T) {
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
		Options: map[string]any{"cached_content": "cachedContents/abc123"},
	}

	gr := toGeminiRequest(req)

	if gr.CachedContent != "cachedContents/abc123" {
		t.Errorf("expected CachedContent=cachedContents/abc123, got %q", gr.CachedContent)
	}
}

func TestToGeminiRequest_NoOptionsNoBuiltinTools(t *testing.T) {
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
	}

	gr := toGeminiRequest(req)

	// No tools at all when no function declarations and no options.
	if len(gr.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(gr.Tools))
	}
}

func TestToGeminiRequest_CombinedOptions(t *testing.T) {
	// All provider-specific options set simultaneously with function declarations.
	req := &canonical.Request{
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
		Tools: []canonical.ToolDef{
			{Name: "search", Description: "search tool", InputSchema: json.RawMessage(`{"type":"object"}`)},
		},
		Options: map[string]any{
			"grounding":      "google_search",
			"code_execution": true,
			"cached_content": "cachedContents/xyz",
		},
	}

	gr := toGeminiRequest(req)

	// Should have 3 tool declarations: function decls + google search + code execution.
	if len(gr.Tools) != 3 {
		t.Fatalf("expected 3 tool declarations, got %d", len(gr.Tools))
	}

	hasFunc, hasSearch, hasCode := false, false, false
	for _, td := range gr.Tools {
		if len(td.FunctionDeclarations) > 0 {
			hasFunc = true
		}
		if td.GoogleSearch != nil {
			hasSearch = true
		}
		if td.CodeExecution != nil {
			hasCode = true
		}
	}
	if !hasFunc {
		t.Error("missing function declarations")
	}
	if !hasSearch {
		t.Error("missing googleSearch")
	}
	if !hasCode {
		t.Error("missing codeExecution")
	}
	if gr.CachedContent != "cachedContents/xyz" {
		t.Errorf("expected cachedContent, got %q", gr.CachedContent)
	}
}

// testCtx returns a context from a testing.T (helper to avoid repeating context.Background).
func testCtx(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
