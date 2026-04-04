// Package openrouter implements provider.Provider for the OpenRouter API.
//
// OpenRouter is OpenAI-compatible, so we reuse the OpenAI translator.
// Differences:
// - Base URL: https://openrouter.ai/api/v1
// - Auth: Bearer token (same as OpenAI)
// - Extra headers: HTTP-Referer, X-Title for app identification
// - Supports 200+ models from multiple providers
package openrouter

import (
	"context"
	"net/http"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/openai"
)

// Client wraps the OpenAI client with OpenRouter-specific config.
type Client struct {
	inner *openai.Client
}

// NewClient creates a new OpenRouter client.
func NewClient(apiKey string) *Client {
	return &Client{
		inner: openai.NewClient(apiKey,
			openai.WithBaseURL("https://openrouter.ai/api/v1"),
			openai.WithHTTPClient(&http.Client{
				Transport: &openRouterTransport{apiKey: apiKey},
			}),
		),
	}
}

func (c *Client) Name() string { return "openrouter" }

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return c.inner.Chat(ctx, req)
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return c.inner.ChatStream(ctx, req)
}

// Healthy checks if the API key works.
func (c *Client) Healthy(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+c.inner.APIKey())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// openRouterTransport adds OpenRouter-specific headers.
type openRouterTransport struct {
	apiKey string
}

func (t *openRouterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("HTTP-Referer", "https://sageclaw.dev")
	req.Header.Set("X-Title", "SageClaw")
	return http.DefaultTransport.RoundTrip(req)
}

// ListModels delegates to the inner OpenAI-compatible client.
// OpenRouter's /v1/models endpoint returns all available models.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return c.inner.ListModels(ctx)
}

var _ provider.Provider = (*Client)(nil)
var _ provider.ModelLister = (*Client)(nil)
