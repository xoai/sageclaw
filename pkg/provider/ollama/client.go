package ollama

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/openai"
)

const defaultOllamaURL = "http://localhost:11434/v1"

// Client implements provider.Provider for Ollama via its OpenAI-compatible API.
type Client struct {
	inner   *openai.Client
	baseURL string
	client  *http.Client
}

// Option configures the Ollama client.
type Option func(*Client)

// WithBaseURL overrides the Ollama API URL.
func WithBaseURL(url string) Option {
	return func(c *Client) { c.baseURL = url }
}

// New creates a new Ollama provider client.
func New(opts ...Option) *Client {
	c := &Client{
		baseURL: defaultOllamaURL,
		client:  &http.Client{Timeout: 1 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.inner = openai.NewClient("", openai.WithBaseURL(c.baseURL))
	return c
}

func (c *Client) Name() string { return "ollama" }

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	return c.inner.Chat(ctx, req)
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	// Ollama doesn't support stream_options — tell ToOpenAIRequest to skip it.
	if req.Options == nil {
		req.Options = make(map[string]any)
	}
	req.Options["no_stream_options"] = true
	return c.inner.ChatStream(ctx, req)
}

// Healthy checks if Ollama is running by calling GET /api/tags.
func (c *Client) Healthy(ctx context.Context) bool {
	// Ollama's native endpoint (not OpenAI-compat).
	nativeURL := c.baseURL
	// Strip /v1 suffix to get the native base.
	if len(nativeURL) > 3 && nativeURL[len(nativeURL)-3:] == "/v1" {
		nativeURL = nativeURL[:len(nativeURL)-3]
	}

	req, err := http.NewRequestWithContext(ctx, "GET", nativeURL+"/api/tags", nil)
	if err != nil {
		return false
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// Models returns the list of installed models.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	nativeURL := c.baseURL
	if len(nativeURL) > 3 && nativeURL[len(nativeURL)-3:] == "/v1" {
		nativeURL = nativeURL[:len(nativeURL)-3]
	}

	req, err := http.NewRequestWithContext(ctx, "GET", nativeURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing models: %w", err)
	}

	names := make([]string, len(result.Models))
	for i, m := range result.Models {
		names[i] = m.Name
	}
	return names, nil
}

// ListModels returns available Ollama models as ModelInfo.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	names, err := c.Models(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]provider.ModelInfo, len(names))
	for i, name := range names {
		models[i] = provider.ModelInfo{
			ID:       "ollama/" + name,
			Provider: "ollama",
			ModelID:  name,
			Name:     name,
			Tier:     "local",
		}
	}
	return models, nil
}

var _ provider.Provider = (*Client)(nil)
