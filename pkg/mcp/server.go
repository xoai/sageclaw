package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/xoai/sageclaw/pkg/tool"
)

// Server is an MCP stdio server that exposes SageClaw tools.
type Server struct {
	toolReg *tool.Registry
	reader  io.Reader
	writer  io.Writer
}

// Option configures the MCP server.
type Option func(*Server)

// WithIO overrides stdin/stdout (for testing).
func WithIO(reader io.Reader, writer io.Writer) Option {
	return func(s *Server) {
		s.reader = reader
		s.writer = writer
	}
}

// NewServer creates a new MCP server.
func NewServer(toolReg *tool.Registry, opts ...Option) *Server {
	s := &Server{
		toolReg: toolReg,
		reader:  os.Stdin,
		writer:  os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run starts the MCP stdio server. Blocks until EOF or context cancellation.
func (s *Server) Run(ctx context.Context) error {
	scanner := bufio.NewScanner(s.reader)
	// MCP uses newline-delimited JSON.
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			s.sendError(nil, -32700, "parse error")
			continue
		}

		resp := s.handle(ctx, req)
		s.send(resp)
	}

	return scanner.Err()
}

func (s *Server) handle(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "initialized":
		// Notification, no response needed. But send empty to be safe.
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &Error{Code: -32601, Message: fmt.Sprintf("unknown method: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req JSONRPCRequest) JSONRPCResponse {
	return JSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: InitializeResult{
			ProtocolVersion: "2024-11-05",
			ServerInfo:      Implementation{Name: "sageclaw", Version: "0.4.0"},
			Capabilities: ServerCapabilities{
				Tools: &ToolsCapability{},
			},
		},
	}
}

func (s *Server) handleToolsList(req JSONRPCRequest) JSONRPCResponse {
	defs := s.toolReg.List()
	mcpTools := make([]ToolDef, len(defs))
	for i, d := range defs {
		mcpTools[i] = ToolDef{
			Name:        d.Name,
			Description: d.Description,
			InputSchema: d.InputSchema,
		}
	}
	return JSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: ToolsListResult{Tools: mcpTools},
	}
}

func (s *Server) handleToolsCall(ctx context.Context, req JSONRPCRequest) JSONRPCResponse {
	var params ToolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Error: &Error{Code: -32602, Message: "invalid params"},
		}
	}

	result, err := s.toolReg.Execute(ctx, params.Name, params.Arguments)
	if err != nil {
		return JSONRPCResponse{
			JSONRPC: "2.0", ID: req.ID,
			Result: ToolCallResult{
				Content: []ContentBlock{{Type: "text", Text: err.Error()}},
				IsError: true,
			},
		}
	}

	return JSONRPCResponse{
		JSONRPC: "2.0", ID: req.ID,
		Result: ToolCallResult{
			Content: []ContentBlock{{Type: "text", Text: result.Content}},
			IsError: result.IsError,
		},
	}
}

func (s *Server) send(resp JSONRPCResponse) {
	data, _ := json.Marshal(resp)
	fmt.Fprintf(s.writer, "%s\n", data)
}

func (s *Server) sendError(id any, code int, msg string) {
	s.send(JSONRPCResponse{
		JSONRPC: "2.0", ID: id,
		Error: &Error{Code: code, Message: msg},
	})
}
