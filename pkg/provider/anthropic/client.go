package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

const (
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
)

// Client is a native Anthropic HTTP client with SSE streaming support.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	enableCache bool
}

// ClientOption configures the client.
type ClientOption func(*Client)

// WithBaseURL overrides the API base URL (useful for testing).
func WithBaseURL(url string) ClientOption {
	return func(c *Client) { c.baseURL = url }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = hc }
}

// WithCache enables prompt caching.
func WithCache(enable bool) ClientOption {
	return func(c *Client) { c.enableCache = enable }
}

// NewClient creates a new Anthropic API client.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:      apiKey,
		baseURL:     defaultBaseURL,
		httpClient:  &http.Client{Timeout: 5 * time.Minute},
		enableCache: true,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Name implements provider.Provider.
func (c *Client) Name() string { return "anthropic" }

// Chat sends a non-streaming request and returns the full response.
func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	req.Stream = false
	body, err := ToAPIRequest(req, c.enableCache)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}

	// Log request size for debugging token usage / rate limit issues.
	estimatedTokens := len(body) / 4 // rough chars-to-tokens ratio
	log.Printf("anthropic: Chat request %d bytes (~%d tokens), model=%s, msgs=%d, tools=%d",
		len(body), estimatedTokens, req.Model, len(req.Messages), len(req.Tools))

	thinkingEnabled := hasThinking(req)
	httpReq, err := c.newRequest(ctx, body, thinkingEnabled)
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

	canonResp, err := FromAPIResponse(respBody)
	if err != nil {
		return nil, err
	}

	return canonResp, nil
}

// ChatStream sends a streaming request and returns a channel of events.
func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	req.Stream = true
	body, err := ToAPIRequest(req, c.enableCache)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}

	estimatedTokens := len(body) / 4
	log.Printf("anthropic: ChatStream request %d bytes (~%d tokens), model=%s, msgs=%d, tools=%d",
		len(body), estimatedTokens, req.Model, len(req.Messages), len(req.Tools))

	thinkingEnabled := hasThinking(req)
	httpReq, err := c.newRequest(ctx, body, thinkingEnabled)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
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

func (c *Client) newRequest(ctx context.Context, body []byte, thinkingEnabled bool) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	// Build beta header — may include multiple features.
	var betas []string
	if c.enableCache {
		betas = append(betas, "prompt-caching-2024-07-31")
	}
	if thinkingEnabled {
		betas = append(betas, "interleaved-thinking-2025-05-14")
	}
	if len(betas) > 0 {
		req.Header.Set("anthropic-beta", strings.Join(betas, ","))
	}

	return req, nil
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	return provider.DoWithRetry(c.httpClient, req, provider.DefaultRetryConfig())
}

// ListModels queries the Anthropic API for available models.
// Paginates with limit=1000, follows after_id cursor, caps at 3 pages.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	var allModels []provider.ModelInfo
	var afterID string
	const maxPages = 3

	for page := 0; page < maxPages; page++ {
		url := c.baseURL + "/v1/models?limit=1000"
		if afterID != "" {
			url += "&after_id=" + afterID
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("creating list models request: %w", err)
		}
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("anthropic-version", apiVersion)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("listing models: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("list models: HTTP %d: %s", resp.StatusCode, string(body))
		}

		var result modelsListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decoding models response: %w", err)
		}

		for _, m := range result.Data {
			caps := make(map[string]bool)
			if m.Capabilities.ImageInput.Supported {
				caps["vision"] = true
			}
			if m.Capabilities.PDFInput.Supported {
				caps["document"] = true
			}
			if m.Capabilities.Thinking.Supported {
				caps["thinking"] = true
			}
			if m.Capabilities.CodeExecution.Supported {
				caps["code_execution"] = true
			}
			if m.Capabilities.Batch.Supported {
				caps["batch"] = true
			}

			allModels = append(allModels, provider.ModelInfo{
				ID:            "anthropic/" + m.ID,
				Provider:      "anthropic",
				ModelID:       m.ID,
				Name:          m.DisplayName,
				ContextWindow: m.MaxInputTokens,
			})
		}

		if !result.HasMore {
			break
		}
		afterID = result.LastID
	}

	return allModels, nil
}

// modelsListResponse is the paginated response from GET /v1/models.
type modelsListResponse struct {
	Data    []modelEntry `json:"data"`
	HasMore bool         `json:"has_more"`
	LastID  string       `json:"last_id"`
}

type modelEntry struct {
	ID             string            `json:"id"`
	DisplayName    string            `json:"display_name"`
	MaxInputTokens int               `json:"max_input_tokens"`
	MaxTokens      int               `json:"max_tokens"`
	Capabilities   modelCapabilities `json:"capabilities"`
}

type modelCapabilities struct {
	ImageInput    capSupport `json:"image_input"`
	PDFInput      capSupport `json:"pdf_input"`
	Thinking      capSupport `json:"thinking"`
	CodeExecution capSupport `json:"code_execution"`
	Batch         capSupport `json:"batch"`
}

type capSupport struct {
	Supported bool `json:"supported"`
}

// Compile-time interface checks.
var _ provider.Provider = (*Client)(nil)
var _ provider.ModelLister = (*Client)(nil)

// APIError represents a structured API error.
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// Supports implements provider.ProviderCapabilities.
// Anthropic supports vision and document analysis.
func (c *Client) Supports(cap string) bool {
	switch cap {
	case provider.CapVision, provider.CapDocument:
		return true
	default:
		return false
	}
}

// validThinkingLevels are the supported thinking budget levels.
var validThinkingLevels = map[string]bool{"low": true, "medium": true, "high": true}

// hasThinking returns true if the request has a valid thinking level in Options.
func hasThinking(req *canonical.Request) bool {
	if req.Options == nil {
		return false
	}
	level, _ := req.Options["thinking_level"].(string)
	return validThinkingLevels[level]
}

func parseAPIError(data []byte) *APIError {
	var wrapper struct {
		Error APIError `json:"error"`
	}
	if json.Unmarshal(data, &wrapper) == nil {
		return &wrapper.Error
	}
	return nil
}
