// Package openaicompat provides a generic client for OpenAI-compatible APIs.
//
// Instead of writing a new Go package per provider, configure a Client with
// a Config struct. Quirks handle provider-specific behavior (thinking fields,
// schema cleaning, etc.). Hooks allow custom request/response transforms.
//
// Example:
//
//	client := openaicompat.New(openaicompat.Config{
//	    Name:    "deepseek",
//	    BaseURL: "https://api.deepseek.com/v1",
//	    APIKey:  os.Getenv("DEEPSEEK_API_KEY"),
//	    Quirks:  openaicompat.Quirks{ThinkingField: "reasoning_content"},
//	})
package openaicompat

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
	"github.com/xoai/sageclaw/pkg/provider/openai"
)

// Config defines an OpenAI-compatible provider.
type Config struct {
	Name    string            // Provider name (e.g. "deepseek", "openrouter").
	BaseURL string            // API base URL (e.g. "https://api.deepseek.com/v1").
	APIKey  string            // API key (sent as Bearer token).
	Headers map[string]string // Extra HTTP headers per request.
	Quirks  Quirks            // Provider-specific behavior overrides.

	// Hooks for custom transforms. Both are optional.
	RequestHook  func(body []byte) []byte        // Transform request JSON before sending.
	ResponseHook func(body []byte) []byte        // Transform response JSON before parsing.
}

// Quirks describes provider-specific deviations from the OpenAI spec.
type Quirks struct {
	// ThinkingField is the JSON field name for reasoning/thinking content
	// in streaming deltas (e.g. "reasoning_content" for DeepSeek R1).
	// When set, the field is extracted and emitted as thinking content deltas.
	ThinkingField string

	// SchemaCleanMode is passed to provider.CleanSchema for tool schemas.
	// Empty = no cleaning. "gemini" or "anthropic" for known modes.
	SchemaCleanMode string

	// StripSystemRole converts system messages to user messages.
	// Some providers (e.g. older Ollama models) don't support system role.
	StripSystemRole bool

	// MaxTokensField overrides the JSON field for max tokens.
	// Default: "max_tokens". Some providers use "max_completion_tokens".
	MaxTokensField string

	// NoStreamOptions disables sending stream_options in streaming requests.
	// Some providers reject the stream_options field.
	NoStreamOptions bool
}

// Client implements provider.Provider for any OpenAI-compatible API.
type Client struct {
	cfg    Config
	client *http.Client
}

// New creates a new OpenAI-compatible provider client.
func New(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.openai.com/v1"
	}
	// Ensure base URL doesn't end with /.
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")

	return &Client{
		cfg:    cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (c *Client) Name() string { return c.cfg.Name }

func (c *Client) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	req.Stream = false
	body, err := openai.ToOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("%s: translating request: %w", c.cfg.Name, err)
	}

	body = c.applyQuirksToRequest(body)
	if c.cfg.RequestHook != nil {
		body = c.cfg.RequestHook(body)
	}

	httpReq, err := c.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := provider.DoWithRetry(c.client, httpReq, provider.DefaultRetryConfig())
	if err != nil {
		return nil, provider.EnrichError(err, c.cfg.Name, req.Model)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s: reading response: %w", c.cfg.Name, err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, provider.NewHTTPError(resp.StatusCode, string(respBody), c.cfg.Name, req.Model)
	}

	if c.cfg.ResponseHook != nil {
		respBody = c.cfg.ResponseHook(respBody)
	}

	canonResp, err := c.parseResponse(respBody)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", c.cfg.Name, err)
	}
	return canonResp, nil
}

func (c *Client) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	req.Stream = true
	body, err := openai.ToOpenAIRequest(req)
	if err != nil {
		return nil, fmt.Errorf("%s: translating request: %w", c.cfg.Name, err)
	}

	body = c.applyQuirksToRequest(body)
	if c.cfg.RequestHook != nil {
		body = c.cfg.RequestHook(body)
	}

	httpReq, err := c.newRequest(ctx, body)
	if err != nil {
		return nil, err
	}

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return nil, &provider.ProviderError{
			Reason: provider.ReasonTimeout, Provider: c.cfg.Name, Model: req.Model, Err: err,
		}
	}

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, provider.NewHTTPError(resp.StatusCode, string(errBody), c.cfg.Name, req.Model)
	}

	events := make(chan provider.StreamEvent, 32)
	if c.cfg.Quirks.ThinkingField != "" {
		// Use quirks-aware stream parser.
		go func() {
			parseQuirksStream(resp.Body, events, c.cfg.Quirks)
			resp.Body.Close()
		}()
	} else {
		// Standard OpenAI stream parser.
		go func() {
			openai.ParseSSEStream(resp.Body, events)
			resp.Body.Close()
		}()
	}

	return events, nil
}

// Healthy checks if the API is reachable.
func (c *Client) Healthy(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/models", nil)
	if err != nil {
		return false
	}
	c.setHeaders(req)

	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ListModels queries the provider's /models endpoint.
func (c *Client) ListModels(ctx context.Context) ([]provider.ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("%s list models: HTTP %d", c.cfg.Name, resp.StatusCode)
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
		models = append(models, provider.ModelInfo{
			ID:       c.cfg.Name + "/" + m.ID,
			Provider: c.cfg.Name,
			ModelID:  m.ID,
			Name:     m.ID,
		})
	}
	return models, nil
}

func (c *Client) newRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST",
		c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: creating request: %w", c.cfg.Name, err)
	}
	c.setHeaders(req)
	return req, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	for k, v := range c.cfg.Headers {
		req.Header.Set(k, v)
	}
}

// applyQuirksToRequest transforms the serialized request JSON based on quirks.
func (c *Client) applyQuirksToRequest(body []byte) []byte {
	q := c.cfg.Quirks
	if q.StripSystemRole || q.MaxTokensField != "" || q.SchemaCleanMode != "" || q.NoStreamOptions {
		var raw map[string]any
		if json.Unmarshal(body, &raw) != nil {
			return body
		}
		changed := false

		// Strip system role: convert to user message.
		if q.StripSystemRole {
			if msgs, ok := raw["messages"].([]any); ok {
				for i, m := range msgs {
					if msg, ok := m.(map[string]any); ok && msg["role"] == "system" {
						msg["role"] = "user"
						msgs[i] = msg
						changed = true
					}
				}
			}
		}

		// Rename max_tokens field.
		if q.MaxTokensField != "" && q.MaxTokensField != "max_tokens" {
			if mt, ok := raw["max_tokens"]; ok {
				raw[q.MaxTokensField] = mt
				delete(raw, "max_tokens")
				changed = true
			}
		}

		// Strip stream_options if provider doesn't support it.
		if q.NoStreamOptions {
			if _, ok := raw["stream_options"]; ok {
				delete(raw, "stream_options")
				changed = true
			}
		}

		// Clean tool schemas.
		if q.SchemaCleanMode != "" {
			if tools, ok := raw["tools"].([]any); ok {
				for _, t := range tools {
					if tool, ok := t.(map[string]any); ok {
						if fn, ok := tool["function"].(map[string]any); ok {
							if params, ok := fn["parameters"]; ok {
								if paramBytes, err := json.Marshal(params); err == nil {
									cleaned := provider.CleanSchema(paramBytes, q.SchemaCleanMode)
									var cleanedObj any
									if json.Unmarshal(cleaned, &cleanedObj) == nil {
										fn["parameters"] = cleanedObj
										changed = true
									}
								}
							}
						}
					}
				}
			}
		}

		if changed {
			if out, err := json.Marshal(raw); err == nil {
				return out
			}
		}
	}
	return body
}

// parseResponse handles provider-specific response parsing.
func (c *Client) parseResponse(body []byte) (*canonical.Response, error) {
	resp, err := openai.FromOpenAIResponse(body)
	if err != nil {
		return nil, err
	}

	// Extract thinking content from quirks field.
	if c.cfg.Quirks.ThinkingField != "" {
		c.extractThinking(body, resp)
	}

	return resp, nil
}

// extractThinking extracts thinking/reasoning content from provider-specific fields.
// Uses map-based parsing to dynamically read whatever field ThinkingField names.
func (c *Client) extractThinking(body []byte, resp *canonical.Response) {
	field := c.cfg.Quirks.ThinkingField
	if field == "" || len(resp.Messages) == 0 {
		return
	}

	var raw struct {
		Choices []struct {
			Message map[string]any `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(body, &raw) != nil || len(raw.Choices) == 0 {
		return
	}

	thinkingText, _ := raw.Choices[0].Message[field].(string)
	if thinkingText == "" {
		return
	}

	// Prepend thinking block before existing content.
	thinking := canonical.Content{Type: "thinking", Thinking: thinkingText}
	resp.Messages[0].Content = append([]canonical.Content{thinking}, resp.Messages[0].Content...)
}

// Compile-time checks.
var _ provider.Provider = (*Client)(nil)
var _ provider.ModelLister = (*Client)(nil)
