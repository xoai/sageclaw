package openai

import (
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

const defaultBaseURL = "https://api.openai.com/v1"

// Client implements provider.Provider for OpenAI-compatible APIs.
type Client struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

// Option configures the client.
type Option func(*Client)

// WithBaseURL overrides the API base URL.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) { c.client = hc }
}

// NewClient creates a new OpenAI-compatible API client.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: 5 * time.Minute},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) Name() string  { return "openai" }
func (c *Client) APIKey() string { return c.apiKey }

// ListModels queries the OpenAI API for available models.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openai list models: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}

	var models []provider.ModelInfo
	for _, m := range result.Data {
		// Filter to chat models only (skip embeddings, tts, whisper, dall-e).
		if strings.HasPrefix(m.ID, "text-embedding") || strings.HasPrefix(m.ID, "tts-") ||
			strings.HasPrefix(m.ID, "whisper") || strings.HasPrefix(m.ID, "dall-e") {
			continue
		}
		models = append(models, provider.ModelInfo{
			ID:       "openai/" + m.ID,
			Provider: "openai",
			ModelID:  m.ID,
			Name:     m.ID,
		})
	}
	return models, nil
}

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	req.Stream = false
	body, err := ToOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.doWithRetry(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(respBody))
	}

	return FromOpenAIResponse(respBody)
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	req.Stream = true
	body, err := ToOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("API error (HTTP %d): %s", resp.StatusCode, string(body))
	}

	events := make(chan provider.StreamEvent, 32)
	go func() {
		ParseSSEStream(resp.Body, events)
		resp.Body.Close()
	}()

	return events, nil
}

func (c *Client) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	return req, nil
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	return provider.DoWithRetry(c.client, req, provider.DefaultRetryConfig())
}

// Supports implements provider.ProviderCapabilities.
// OpenAI supports vision and image generation.
func (c *Client) Supports(cap string) bool {
	switch cap {
	case provider.CapVision, provider.CapImageGen:
		return true
	default:
		return false
	}
}

// Compile-time check.
var _ provider.Provider = (*Client)(nil)
