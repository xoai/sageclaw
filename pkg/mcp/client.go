package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

// Client connects to an external MCP server via any Transport and imports its tools.
type Client struct {
	name      string
	transport Transport
	prefix    string // Tool name prefix (default: name + "_").
	trust     string // "trusted" or "untrusted" (default).
	tools     []ToolDef
}

// NewClient creates a new MCP client with the given transport.
func NewClient(name string, transport Transport) *Client {
	return &Client{
		name:      name,
		transport: transport,
		prefix:    name + "_",
		trust:     "untrusted",
	}
}

// NewClientFromConfig creates a Client from an MCPServerConfig, selecting the right transport.
func NewClientFromConfig(name string, cfg MCPServerConfig) (*Client, error) {
	var t Transport
	timeout := time.Duration(cfg.TimeoutSec) * time.Second

	switch cfg.Transport {
	case "stdio", "":
		if cfg.Command == "" {
			return nil, fmt.Errorf("mcp %s: stdio transport requires command", name)
		}
		t = NewStdioTransport(name, cfg.Command, cfg.Args, cfg.Env)
	case "sse":
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp %s: sse transport requires url", name)
		}
		t = NewSSETransport(name, cfg.URL, cfg.Headers, timeout)
	case "streamable-http", "http":
		if cfg.URL == "" {
			return nil, fmt.Errorf("mcp %s: http transport requires url", name)
		}
		t = NewHTTPTransport(name, cfg.URL, cfg.Headers, timeout)
	default:
		return nil, fmt.Errorf("mcp %s: unknown transport %q", name, cfg.Transport)
	}

	prefix := cfg.ToolPrefix
	if prefix == "" {
		prefix = name + "_"
	}

	trust := cfg.Trust
	if trust == "" {
		trust = "untrusted"
	}

	return &Client{
		name:      name,
		transport: t,
		prefix:    prefix,
		trust:     trust,
	}, nil
}

// MCPServerConfig defines how to connect to an external MCP server.
// Duplicated here for convenience — canonical definition in agentcfg/types.go.
type MCPServerConfig = struct {
	Transport  string            `json:"transport" yaml:"transport"`
	Command    string            `json:"command,omitempty" yaml:"command"`
	Args       []string          `json:"args,omitempty" yaml:"args"`
	Env        map[string]string `json:"env,omitempty" yaml:"env"`
	URL        string            `json:"url,omitempty" yaml:"url"`
	Headers    map[string]string `json:"headers,omitempty" yaml:"headers"`
	ToolPrefix string            `json:"tool_prefix,omitempty" yaml:"tool_prefix"`
	TimeoutSec int               `json:"timeout_sec,omitempty" yaml:"timeout_sec"`
	Trust      string            `json:"trust,omitempty" yaml:"trust"`
	Enabled    *bool             `json:"enabled,omitempty" yaml:"enabled"`
}

// Start connects the transport and discovers tools.
func (c *Client) Start(ctx context.Context) error {
	if err := c.transport.Connect(ctx); err != nil {
		return err
	}

	// List tools.
	toolsResp, err := c.transport.Call(ctx, "tools/list", nil)
	if err != nil {
		return fmt.Errorf("mcp-client %s: tools/list: %w", c.name, err)
	}

	var toolsList ToolsListResult
	resultData, _ := json.Marshal(toolsResp.Result)
	json.Unmarshal(resultData, &toolsList)
	c.tools = toolsList.Tools

	log.Printf("mcp-client %s: %d tools available", c.name, len(c.tools))
	return nil
}

// Stop shuts down the transport.
func (c *Client) Stop() {
	if c.transport != nil {
		c.transport.Close()
	}
}

// Healthy returns whether the transport connection is alive.
func (c *Client) Healthy() bool {
	if c.transport == nil {
		return false
	}
	return c.transport.Healthy()
}

// Name returns the server name.
func (c *Client) Name() string {
	return c.name
}

// Trust returns the trust level.
func (c *Client) Trust() string {
	return c.trust
}

// RegisterTools registers the external MCP server's tools into the tool registry.
func (c *Client) RegisterTools(reg *tool.Registry) {
	for _, t := range c.tools {
		mcpTool := t // Capture for closure.
		name := c.prefix + mcpTool.Name

		reg.RegisterWithGroup(name, fmt.Sprintf("[MCP:%s] %s", c.name, mcpTool.Description),
			mcpTool.InputSchema,
			tool.GroupMCP, tool.RiskSensitive, "mcp:"+c.name,
			func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
				result, err := c.CallTool(ctx, mcpTool.Name, input)
				if err != nil {
					return &canonical.ToolResult{Content: err.Error(), IsError: true}, nil
				}
				// Scrub credentials from all results.
				result.Content = ScrubCredentials(result.Content)
				// Wrap untrusted results with injection boundaries.
				if !IsTrusted(c.trust) {
					result.Content = WrapUntrustedResult(c.name, mcpTool.Name, result.Content)
				}
				return result, nil
			})
	}
}

// CallTool invokes a tool on the external MCP server.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*canonical.ToolResult, error) {
	resp, err := c.transport.Call(ctx, "tools/call", map[string]any{
		"name":      name,
		"arguments": json.RawMessage(arguments),
	})
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return &canonical.ToolResult{Content: resp.Error.Message, IsError: true}, nil
	}

	var result ToolCallResult
	resultData, _ := json.Marshal(resp.Result)
	json.Unmarshal(resultData, &result)

	var text string
	for _, block := range result.Content {
		if block.Type == "text" {
			text += block.Text
		}
	}

	return &canonical.ToolResult{Content: text, IsError: result.IsError}, nil
}

// Tools returns the list of tools from the external server.
func (c *Client) Tools() []ToolDef {
	return c.tools
}
