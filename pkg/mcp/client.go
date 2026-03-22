package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"sync/atomic"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

// Client connects to an external MCP server (stdio transport) and imports its tools.
type Client struct {
	command string   // e.g. "npx" or "python"
	args    []string // e.g. ["-y", "@modelcontextprotocol/server-filesystem", "/path"]
	name    string   // Human-friendly name for this MCP server.

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Scanner

	mu     sync.Mutex
	nextID atomic.Int64
	pending map[int64]chan JSONRPCResponse

	tools []ToolDef
}

// ClientConfig defines how to connect to an MCP server.
type ClientConfig struct {
	Name    string   `json:"name" yaml:"name"`       // Display name.
	Command string   `json:"command" yaml:"command"` // Command to run.
	Args    []string `json:"args" yaml:"args"`       // Arguments.
}

// NewClient creates a new MCP client for an external server.
func NewClient(config ClientConfig) *Client {
	return &Client{
		command: config.Command,
		args:    config.Args,
		name:    config.Name,
		pending: make(map[int64]chan JSONRPCResponse),
	}
}

// Start launches the MCP server process and initializes the connection.
func (c *Client) Start(ctx context.Context) error {
	c.cmd = exec.CommandContext(ctx, c.command, c.args...)

	var err error
	c.stdin, err = c.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcp-client %s: stdin pipe: %w", c.name, err)
	}

	c.stdout, err = c.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcp-client %s: stdout pipe: %w", c.name, err)
	}

	if err := c.cmd.Start(); err != nil {
		return fmt.Errorf("mcp-client %s: start: %w", c.name, err)
	}

	c.reader = bufio.NewScanner(c.stdout)
	c.reader.Buffer(make([]byte, 1024*1024), 1024*1024)

	// Start response reader.
	go c.readResponses()

	// Initialize.
	initResp, err := c.call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "sageclaw", "version": "0.4.0"},
		"capabilities":    map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("mcp-client %s: initialize: %w", c.name, err)
	}

	log.Printf("mcp-client %s: initialized (protocol: %v)", c.name, initResp.Result)

	// Send initialized notification.
	c.send(JSONRPCRequest{JSONRPC: "2.0", Method: "initialized"})

	// List tools.
	toolsResp, err := c.call(ctx, "tools/list", nil)
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

// Stop shuts down the MCP server process.
func (c *Client) Stop() {
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
}

// RegisterTools registers the external MCP server's tools into the tool registry.
// Each tool is prefixed with the server name to avoid collisions.
func (c *Client) RegisterTools(reg *tool.Registry) {
	for _, t := range c.tools {
		mcpTool := t // Capture for closure.
		prefix := c.name + "_"
		name := prefix + mcpTool.Name

		reg.Register(name, fmt.Sprintf("[MCP:%s] %s", c.name, mcpTool.Description),
			mcpTool.InputSchema,
			func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
				result, err := c.CallTool(ctx, mcpTool.Name, input)
				if err != nil {
					return &canonical.ToolResult{Content: err.Error(), IsError: true}, nil
				}
				return result, nil
			})
	}
}

// CallTool invokes a tool on the external MCP server.
func (c *Client) CallTool(ctx context.Context, name string, arguments json.RawMessage) (*canonical.ToolResult, error) {
	resp, err := c.call(ctx, "tools/call", map[string]any{
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

func (c *Client) call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	id := c.nextID.Add(1)

	var paramsData json.RawMessage
	if params != nil {
		paramsData, _ = json.Marshal(params)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsData,
	}

	respCh := make(chan JSONRPCResponse, 1)
	c.mu.Lock()
	c.pending[id] = respCh
	c.mu.Unlock()

	if err := c.send(req); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-respCh:
		return &resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) send(req JSONRPCRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.stdin, "%s\n", data)
	return err
}

func (c *Client) readResponses() {
	for c.reader.Scan() {
		var resp JSONRPCResponse
		if err := json.Unmarshal(c.reader.Bytes(), &resp); err != nil {
			continue
		}

		// Match response to pending request.
		if resp.ID != nil {
			var id int64
			switch v := resp.ID.(type) {
			case float64:
				id = int64(v)
			case int64:
				id = v
			}

			c.mu.Lock()
			ch, ok := c.pending[id]
			if ok {
				delete(c.pending, id)
			}
			c.mu.Unlock()

			if ok {
				ch <- resp
			}
		}
	}
}
