package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	canonResp, err := FromAPIResponse(respBody)
	if err != nil {
		return nil, err
	}

	// Track cache stats.
	provider.GlobalCacheStats.Record(
		canonResp.Usage.InputTokens, canonResp.Usage.OutputTokens,
		canonResp.Usage.CacheCreation, canonResp.Usage.CacheRead,
	)

	return canonResp, nil
}

// ChatStream sends a streaming request and returns a channel of events.
func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	req.Stream = true
	body, err := ToAPIRequest(req, c.enableCache)
	if err != nil {
		return nil, fmt.Errorf("translating request: %w", err)
	}

	httpReq, err := c.newRequest(ctx, body)
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

func (c *Client) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.baseURL+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	if c.enableCache {
		req.Header.Set("anthropic-beta", "prompt-caching-2024-07-31")
	}

	return req, nil
}

func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			// Read and discard body to allow reuse.
			if req.Body != nil {
				body, _ := io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(body))
			}
			backoff := time.Duration(1<<uint(attempt)) * 500 * time.Millisecond
			select {
			case <-time.After(backoff):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// Retry on 429 and 5xx.
		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}

		return resp, nil
	}
	return nil, fmt.Errorf("all retries exhausted: %w", lastErr)
}

// Compile-time interface check.
var _ provider.Provider = (*Client)(nil)

// APIError represents a structured API error.
type APIError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
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
