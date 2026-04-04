// Package github implements provider.Provider for GitHub Copilot's API.
//
// GitHub Copilot exposes an OpenAI-compatible API at:
//   https://api.githubcopilot.com/chat/completions
//
// Auth uses a GitHub personal access token (PAT) with copilot scope,
// sent as Bearer token. The API supports GPT-4o and Claude models
// that GitHub has licensed.
package github

import (
	"context"
	"net/http"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/openai"
)

// Client wraps the OpenAI client with GitHub Copilot config.
type Client struct {
	inner  *openai.Client
	token  string
}

// NewClient creates a new GitHub Copilot client.
func NewClient(token string) *Client {
	return &Client{
		token: token,
		inner: openai.NewClient(token,
			openai.WithBaseURL("https://api.githubcopilot.com"),
		),
	}
}

func (c *Client) Name() string { return "github" }

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return c.inner.Chat(ctx, req)
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return c.inner.ChatStream(ctx, req)
}

// Healthy checks if the token works.
func (c *Client) Healthy(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ListModels delegates to the inner OpenAI-compatible client.
// GitHub Copilot's API returns available models (GPT-4o, Claude, etc.).
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	return c.inner.ListModels(ctx)
}

var _ provider.Provider = (*Client)(nil)
var _ provider.ModelLister = (*Client)(nil)
