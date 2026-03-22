// Package gemini implements provider.Provider for Google's Gemini API.
package gemini

import (
	"bufio"
	"bytes"
	"context"
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

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := line[6:]
			if data == "[DONE]" {
				events <- provider.StreamEvent{Type: "done"}
				return
			}

			var chunk geminiStreamChunk
			if json.Unmarshal([]byte(data), &chunk) != nil {
				continue
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
				}
			}
		}

		events <- provider.StreamEvent{Type: "done"}
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
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text,omitempty"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiResponse struct {
	Candidates []geminiCandidate `json:"candidates"`
}

type geminiCandidate struct {
	Content geminiContent `json:"content"`
}

type geminiStreamChunk struct {
	Candidates []geminiCandidate `json:"candidates"`
}

func toGeminiRequest(req *canonical.Request) *geminiRequest {
	gr := &geminiRequest{}

	if req.System != "" {
		gr.SystemInstruction = &geminiContent{
			Role:  "user",
			Parts: []geminiPart{{Text: req.System}},
		}
	}

	for _, msg := range req.Messages {
		role := msg.Role
		if role == "assistant" {
			role = "model"
		}

		var parts []geminiPart
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				parts = append(parts, geminiPart{Text: c.Text})
			}
		}

		if len(parts) > 0 {
			gr.Contents = append(gr.Contents, geminiContent{Role: role, Parts: parts})
		}
	}

	if req.MaxTokens > 0 {
		gr.GenerationConfig = &geminiGenerationConfig{MaxOutputTokens: req.MaxTokens}
	}

	return gr
}

func fromGeminiResponse(body []byte) (*canonical.Response, error) {
	var gr geminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return nil, fmt.Errorf("parsing gemini response: %w", err)
	}

	var content []canonical.Content
	for _, candidate := range gr.Candidates {
		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content = append(content, canonical.Content{Type: "text", Text: part.Text})
			}
		}
	}

	return &canonical.Response{
		Messages: []canonical.Message{{
			Role:    "assistant",
			Content: content,
		}},
		StopReason: "end_turn",
	}, nil
}

var _ provider.Provider = (*Client)(nil)
