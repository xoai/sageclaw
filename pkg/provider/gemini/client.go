// Package gemini implements provider.Provider for Google's Gemini API.
package gemini

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

const defaultBaseURL = "https://generativelanguage.googleapis.com/v1beta"

type Client struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

type Option func(*Client)

func WithBaseURL(url string) Option { return func(c *Client) { c.baseURL = url } }
func WithModel(m string) Option    { return func(c *Client) { c.model = m } }

func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		model:   "gemini-2.0-flash",
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Name() string { return "gemini" }

func (c *Client) Healthy(ctx context.Context) bool {
	url := fmt.Sprintf("%s/models?key=%s", c.baseURL, c.apiKey)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ListModels queries the Gemini API for available models.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	url := fmt.Sprintf("%s/models?key=%s", c.baseURL, c.apiKey)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini list models: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Models []struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []provider.ModelInfo
	for _, m := range result.Models {
		// Name is "models/gemini-2.0-flash" — strip prefix.
		modelID := strings.TrimPrefix(m.Name, "models/")
		models = append(models, provider.ModelInfo{
			ID:       "gemini/" + modelID,
			Provider: "gemini",
			ModelID:  modelID,
			Name:     m.DisplayName,
		})
	}
	return models, nil
}

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	model := c.resolveModel(req.Model)
	body := toGeminiRequest(req)

	jsonBody, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("gemini API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return fromGeminiResponse(respBody)
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	model := c.resolveModel(req.Model)
	body := toGeminiRequest(req)
	jsonBody, _ := json.Marshal(body)

	url := fmt.Sprintf("%s/models/%s:streamGenerateContent?alt=sse&key=%s", c.baseURL, model, c.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	events := make(chan provider.StreamEvent, 32)
	go func() {
		defer resp.Body.Close()
		defer close(events)

		hasToolCalls := false
		callSeq := 0 // Monotonic counter for unique tool call IDs across chunks.
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024) // 1MB max line for large responses.
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := line[6:]
			if data == "[DONE]" {
				sr := "end_turn"
				if hasToolCalls {
					sr = "tool_use"
				}
				events <- provider.StreamEvent{Type: "done", StopReason: sr}
				return
			}

			var chunk geminiStreamChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
			}

			// Emit usage if present (typically in the last chunk).
			if chunk.UsageMetadata != nil {
				events <- provider.StreamEvent{
					Type: "usage",
					Usage: &canonical.Usage{
						InputTokens:  chunk.UsageMetadata.PromptTokenCount,
						OutputTokens: chunk.UsageMetadata.CandidatesTokenCount,
						CacheRead:    chunk.UsageMetadata.CachedContentTokenCount,
					},
				}
			}

			for _, candidate := range chunk.Candidates {
				for _, part := range candidate.Content.Parts {
					if part.Text != "" {
						events <- provider.StreamEvent{
							Type: "content_delta",
							Delta: &canonical.Content{
								Type: "text",
								Text: part.Text,
							},
						}
					}
					if part.FunctionCall != nil {
						hasToolCalls = true
						inputJSON, _ := json.Marshal(part.FunctionCall.Args)
						callID := fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, callSeq)
						callSeq++
						tc := &canonical.ToolCall{
							ID:    callID,
							Name:  part.FunctionCall.Name,
							Input: inputJSON,
						}
						if part.ThoughtSignature != "" {
							tc.Meta = map[string]string{"thought_signature": part.ThoughtSignature}
						}
						events <- provider.StreamEvent{
							Type:  "tool_call",
							Index: callSeq - 1,
							Delta: &canonical.Content{ToolCall: tc},
						}
					}
				}
			}
		}

		sr := "end_turn"
		if hasToolCalls {
			sr = "tool_use"
		}
		events <- provider.StreamEvent{Type: "done", StopReason: sr}
	}()

	return events, nil
}

func (c *Client) resolveModel(model string) string {
	if model != "" {
		return model
	}
	return c.model
}

// --- Gemini API types ---

type geminiRequest struct {
	Contents          []geminiContent         `json:"contents"`
	SystemInstruction *geminiContent          `json:"systemInstruction,omitempty"`
	GenerationConfig  *geminiGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []geminiToolDecl        `json:"tools,omitempty"`
	ToolConfig        *geminiToolConfig       `json:"toolConfig,omitempty"`
	CachedContent     string                  `json:"cachedContent,omitempty"` // Reuse server-side cached context.
}

type geminiToolConfig struct {
	FunctionCallingConfig *geminiFuncCallingConfig `json:"functionCallingConfig,omitempty"`
}

type geminiFuncCallingConfig struct {
	Mode string `json:"mode"` // "AUTO", "ANY", "NONE"
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text             string              `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *geminiFuncResponse `json:"functionResponse,omitempty"`
	ThoughtSignature string              `json:"thoughtSignature,omitempty"` // Gemini 3: must be preserved on functionCall round-trip.
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args,omitempty"`
}

type geminiFuncResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiToolDecl struct {
	FunctionDeclarations []geminiFuncDecl     `json:"functionDeclarations,omitempty"`
	GoogleSearch         *geminiGoogleSearch   `json:"googleSearch,omitempty"`
	CodeExecution        *geminiCodeExecution  `json:"codeExecution,omitempty"`
}

type geminiGoogleSearch struct{}
type geminiCodeExecution struct{}

type geminiFuncDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates    []geminiCandidate   `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

type geminiUsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	CachedContentTokenCount int `json:"cachedContentTokenCount,omitempty"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiStreamChunk struct {
	Candidates    []geminiCandidate    `json:"candidates"`
	UsageMetadata *geminiUsageMetadata `json:"usageMetadata,omitempty"`
}

func toGeminiRequest(req *canonical.Request) *geminiRequest {
	gr := &geminiRequest{}

	if req.System != "" {
		gr.SystemInstruction = &geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	// Convert tools to Gemini function declarations.
	if len(req.Tools) > 0 {
		var decls []geminiFuncDecl
		for _, t := range req.Tools {
			decl := geminiFuncDecl{
				Name:        t.Name,
				Description: t.Description,
			}
			if len(t.InputSchema) > 0 {
				decl.Parameters = provider.CleanSchema(t.InputSchema, "gemini")
			}
			decls = append(decls, decl)
		}
		gr.Tools = []geminiToolDecl{{FunctionDeclarations: decls}}
	}

	// Gemini-specific built-in tools from request Options.
	if req.Options != nil {
		if g, _ := req.Options["grounding"].(string); g == "google_search" {
			gr.Tools = append(gr.Tools, geminiToolDecl{GoogleSearch: &geminiGoogleSearch{}})
		}
		if ce, _ := req.Options["code_execution"].(bool); ce {
			gr.Tools = append(gr.Tools, geminiToolDecl{CodeExecution: &geminiCodeExecution{}})
		}
		if cc, _ := req.Options["cached_content"].(string); cc != "" {
			gr.CachedContent = cc
		}
	}

	// Build ToolCallID → function name lookup from history.
	// Gemini's functionResponse requires the function name, not the call ID.
	callIDToName := make(map[string]string)
	for _, msg := range req.Messages {
		for _, c := range msg.Content {
			if c.ToolCall != nil {
				callIDToName[c.ToolCall.ID] = c.ToolCall.Name
			}
		}
	}

	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		var parts []geminiPart
		for _, c := range msg.Content {
			switch {
			case c.Type == "text" && c.Text != "":
				parts = append(parts, geminiPart{Text: c.Text})

			case c.ToolCall != nil:
				// Assistant's tool call → Gemini functionCall part.
				var args map[string]any
				if len(c.ToolCall.Input) > 0 {
					json.Unmarshal(c.ToolCall.Input, &args)
				}
				p := geminiPart{
					FunctionCall: &geminiFunctionCall{
						Name: c.ToolCall.Name,
						Args: args,
					},
				}
				// Restore thought signature for Gemini 3 round-trip.
				if c.ToolCall.Meta != nil {
					p.ThoughtSignature = c.ToolCall.Meta["thought_signature"]
				}
				parts = append(parts, p)

			case c.ToolResult != nil:
				// Tool result → Gemini functionResponse part.
				// Gemini requires the function name, not the call ID.
				funcName := callIDToName[c.ToolResult.ToolCallID]
				if funcName == "" {
					funcName = c.ToolResult.ToolCallID // fallback — shouldn't happen
				}
				parts = append(parts, geminiPart{
					FunctionResponse: &geminiFuncResponse{
						Name:     funcName,
						Response: map[string]any{"result": c.ToolResult.Content},
					},
				})
			}
		}

		if len(parts) > 0 {
			gr.Contents = append(gr.Contents, geminiContent{Role: role, Parts: parts})
		}
	}

	if req.MaxTokens > 0 {
		gr.GenerationConfig = &geminiGenerationConfig{MaxOutputTokens: req.MaxTokens}
	}

	// Sanitize turn ordering for Gemini's strict rules:
	// 1. functionResponse must immediately follow functionCall
	// 2. No consecutive turns with the same role
	// 3. Strip orphaned functionCall/functionResponse parts
	gr.Contents = sanitizeGeminiContents(gr.Contents)

	return gr
}

// sanitizeGeminiContents repairs Gemini-specific turn ordering issues.
//
// Gemini rules enforced:
//  1. Conversation must start with a user turn (not model).
//  2. All functionCall parts must have a thought_signature (strip pairs that don't).
//  3. functionCall turn must be immediately followed by functionResponse turn.
//  4. functionResponse parts must NOT be mixed with text parts in the same turn.
//  5. No consecutive turns with the same role.
func sanitizeGeminiContents(contents []geminiContent) []geminiContent {
	if len(contents) == 0 {
		return contents
	}

	// Pass 1: Split user turns that mix functionResponse with text/other parts.
	// Gemini requires functionResponse turns to contain ONLY functionResponse parts.
	var split []geminiContent
	for _, turn := range contents {
		if turn.Role == "user" {
			var funcParts, otherParts []geminiPart
			for _, p := range turn.Parts {
				if p.FunctionResponse != nil {
					funcParts = append(funcParts, p)
				} else {
					otherParts = append(otherParts, p)
				}
			}
			if len(funcParts) > 0 && len(otherParts) > 0 {
				// Split: functionResponse parts first, then text parts.
				split = append(split, geminiContent{Role: "user", Parts: funcParts})
				split = append(split, geminiContent{Role: "user", Parts: otherParts})
				continue
			}
		}
		split = append(split, turn)
	}
	contents = split

	// Pass 2: Strip functionCall parts missing thoughtSignature.
	// Gemini requires thought_signature on ALL functionCall parts in history.
	// Tool calls from non-thinking models or other providers lack signatures
	// and must be stripped (along with their paired functionResponse).
	strip := make(map[int]bool)

	for i, turn := range contents {
		if turn.Role != "model" || !hasFuncCall(turn) {
			continue
		}
		missingSignature := false
		for _, p := range turn.Parts {
			if p.FunctionCall != nil && p.ThoughtSignature == "" {
				missingSignature = true
				break
			}
		}
		if missingSignature {
			strip[i] = true
			// Also strip the paired functionResponse turn.
			if i+1 < len(contents) && hasFuncResponse(contents[i+1]) {
				strip[i+1] = true
			}
		}
	}

	// Pass 3: Also strip unpaired functionCalls (with signature but missing response)
	// and orphaned functionResponses (no preceding functionCall).
	for i, turn := range contents {
		if strip[i] || turn.Role != "model" || !hasFuncCall(turn) {
			continue
		}
		if i+1 < len(contents) && contents[i+1].Role == "user" && hasFuncResponse(contents[i+1]) {
			continue // Properly paired.
		}
		strip[i] = true
	}

	for i, turn := range contents {
		if strip[i] || turn.Role != "user" || !hasFuncResponse(turn) {
			continue
		}
		if i > 0 && contents[i-1].Role == "model" && !strip[i-1] && hasFuncCall(contents[i-1]) {
			continue // Properly paired.
		}
		strip[i] = true
	}

	// Pass 4: Build result, keeping only text from stripped turns.
	var result []geminiContent
	for i, turn := range contents {
		if strip[i] {
			var textParts []geminiPart
			for _, p := range turn.Parts {
				if p.Text != "" {
					textParts = append(textParts, geminiPart{Text: p.Text})
				}
			}
			if len(textParts) > 0 {
				result = append(result, geminiContent{Role: turn.Role, Parts: textParts})
			}
			continue
		}
		result = append(result, turn)
	}

	// Pass 5: Merge consecutive same-role turns (Gemini forbids this).
	// But NEVER merge text parts into a functionResponse turn or vice versa.
	if len(result) > 1 {
		merged := []geminiContent{result[0]}
		for _, turn := range result[1:] {
			last := &merged[len(merged)-1]
			if last.Role == turn.Role && !hasFuncResponse(*last) && !hasFuncResponse(turn) {
				last.Parts = append(last.Parts, turn.Parts...)
			} else {
				merged = append(merged, turn)
			}
		}
		result = merged
	}

	// Pass 6: Ensure conversation starts with a user turn.
	if len(result) > 0 && result[0].Role == "model" {
		result = append([]geminiContent{
			{Role: "user", Parts: []geminiPart{{Text: "Continue."}}},
		}, result...)
	}

	return result
}

func hasFuncCall(t geminiContent) bool {
	for _, p := range t.Parts {
		if p.FunctionCall != nil {
			return true
		}
	}
	return false
}

func hasFuncResponse(t geminiContent) bool {
	for _, p := range t.Parts {
		if p.FunctionResponse != nil {
			return true
		}
	}
	return false
}

func fromGeminiResponse(body []byte) (*canonical.Response, error) {
	var gr geminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, fmt.Errorf("parsing gemini response: %w", err)
	}

	var content []canonical.Content
	stopReason := "end_turn"

	for _, candidate := range gr.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content = append(content, canonical.Content{Type: "text", Text: part.Text})
			}
			if part.FunctionCall != nil {
				inputJSON, _ := json.Marshal(part.FunctionCall.Args)
				tc := &canonical.ToolCall{
					ID:    fmt.Sprintf("call_%s_%d", part.FunctionCall.Name, len(content)),
					Name:  part.FunctionCall.Name,
					Input: inputJSON,
				}
				// Preserve Gemini thought signature for round-trip.
				if part.ThoughtSignature != "" {
					tc.Meta = map[string]string{"thought_signature": part.ThoughtSignature}
				}
				content = append(content, canonical.Content{
					Type:     "tool_call",
					ToolCall: tc,
				})
				stopReason = "tool_use"
			}
		}
	}

	resp := &canonical.Response{
		Messages: []canonical.Message{{
			Role:    "assistant",
			Content: content,
		}},
		StopReason: stopReason,
	}

	// Populate usage from Gemini metadata.
	if gr.UsageMetadata != nil {
		resp.Usage = canonical.Usage{
			InputTokens:  gr.UsageMetadata.PromptTokenCount,
			OutputTokens: gr.UsageMetadata.CandidatesTokenCount,
			CacheRead:    gr.UsageMetadata.CachedContentTokenCount,
		}
	}

	return resp, nil
}

// TranscribeAudio sends audio to Gemini's REST API for transcription.
// Uses gemini-2.0-flash (fast, supports audio) to convert audio to text.
// This bypasses the Live API entirely — no WebSocket, no PCM conversion.
func (c *Client) TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error) {
	model := "gemini-2.5-flash" // Fast model, supports audio input.

	reqBody := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{
						"inlineData": map[string]any{
							"mimeType": mimeType,
							"data":     base64.StdEncoding.EncodeToString(audioData),
						},
					},
					{
						"text": "Transcribe this audio message exactly as spoken. Return only the transcription, nothing else.",
					},
				},
			},
		},
	}

	jsonBody, _ := json.Marshal(reqBody)
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.baseURL, model, c.apiKey)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("gemini transcribe: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini transcribe error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	// Parse response — extract text from candidates.
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("gemini transcribe parse: %w", err)
	}

	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}

	return "", fmt.Errorf("gemini transcribe: no text in response")
}

// APIKey returns the API key for use by other components (e.g. voice pipeline).
func (c *Client) APIKey() string { return c.apiKey }

// BaseURL returns the base URL for use by other components.
func (c *Client) BaseURL() string { return c.baseURL }

// Supports implements provider.ProviderCapabilities.
// Gemini supports vision, document analysis, and image generation.
func (c *Client) Supports(cap string) bool {
	switch cap {
	case provider.CapVision, provider.CapDocument, provider.CapImageGen:
		return true
	default:
		return false
	}
}

var _ provider.Provider = (*Client)(nil)
