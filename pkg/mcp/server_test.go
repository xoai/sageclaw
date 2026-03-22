package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

func TestMCP_Initialize(t *testing.T) {
	reg := tool.NewRegistry()
	output := &bytes.Buffer{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","clientInfo":{"name":"test","version":"1.0"}}}` + "\n")

	srv := NewServer(reg, WithIO(input, output))
	srv.Run(context.Background())

	var resp JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %s", resp.Error.Message)
	}

	resultBytes, _ := json.Marshal(resp.Result)
	var result InitializeResult
	json.Unmarshal(resultBytes, &result)

	if result.ServerInfo.Name != "sageclaw" {
		t.Fatalf("expected sageclaw, got %s", result.ServerInfo.Name)
	}
	if result.Capabilities.Tools == nil {
		t.Fatal("expected tools capability")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register("test_tool", "A test tool",
		json.RawMessage(`{"type":"object"}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return &canonical.ToolResult{Content: "ok"}, nil
		})

	output := &bytes.Buffer{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n")

	srv := NewServer(reg, WithIO(input, output))
	srv.Run(context.Background())

	var resp JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolsListResult
	json.Unmarshal(resultBytes, &result)

	if len(result.Tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "test_tool" {
		t.Fatalf("expected test_tool, got %s", result.Tools[0].Name)
	}
}

func TestMCP_ToolsCall(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register("echo", "Echo input",
		json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			var p struct{ Text string }
			json.Unmarshal(input, &p)
			return &canonical.ToolResult{Content: "echo: " + p.Text}, nil
		})

	output := &bytes.Buffer{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hello"}}}` + "\n")

	srv := NewServer(reg, WithIO(input, output))
	srv.Run(context.Background())

	var resp JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolCallResult
	json.Unmarshal(resultBytes, &result)

	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	if result.Content[0].Text != "echo: hello" {
		t.Fatalf("expected 'echo: hello', got %s", result.Content[0].Text)
	}
}

func TestMCP_UnknownMethod(t *testing.T) {
	reg := tool.NewRegistry()
	output := &bytes.Buffer{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":4,"method":"unknown"}` + "\n")

	srv := NewServer(reg, WithIO(input, output))
	srv.Run(context.Background())

	var resp JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	if resp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Fatalf("expected -32601, got %d", resp.Error.Code)
	}
}

func TestMCP_UnknownTool(t *testing.T) {
	reg := tool.NewRegistry()
	output := &bytes.Buffer{}
	input := strings.NewReader(`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}` + "\n")

	srv := NewServer(reg, WithIO(input, output))
	srv.Run(context.Background())

	var resp JSONRPCResponse
	json.Unmarshal(output.Bytes(), &resp)

	resultBytes, _ := json.Marshal(resp.Result)
	var result ToolCallResult
	json.Unmarshal(resultBytes, &result)

	if !result.IsError {
		t.Fatal("expected error for unknown tool")
	}
}
