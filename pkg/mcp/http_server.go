package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/xoai/sageclaw/pkg/tool"
)

// HTTPServer implements the MCP streamable HTTP transport.
// This is the simplest transport — stateless request/response over HTTP POST.
//
// Endpoint:
//   POST /mcp — JSON-RPC request → JSON-RPC response
type HTTPServer struct {
	toolReg *tool.Registry
}

// NewHTTPServer creates a new MCP HTTP transport server.
func NewHTTPServer(toolReg *tool.Registry) *HTTPServer {
	return &HTTPServer{toolReg: toolReg}
}

// HandleRequest handles a single MCP JSON-RPC request over HTTP.
func (s *HTTPServer) HandleRequest(w http.ResponseWriter, r *http.Request) {
	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONError(w, nil, -32700, "parse error")
		return
	}

	handler := &Server{toolReg: s.toolReg}
	resp := handler.handle(r.Context(), req)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
