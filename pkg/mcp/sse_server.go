package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/xoai/sageclaw/pkg/tool"
)

// SSEServer implements the MCP SSE transport.
// The client connects via SSE to receive responses, and sends requests via HTTP POST.
//
// Endpoints:
//   GET  /mcp/sse      — SSE event stream (responses flow here)
//   POST /mcp/messages — client sends JSON-RPC requests here
type SSEServer struct {
	toolReg    *tool.Registry
	clients    map[string]*sseClient
	mu         sync.RWMutex
	nextID     atomic.Int64
}

type sseClient struct {
	id     string
	events chan []byte
	done   chan struct{}
}

// NewSSEServer creates a new MCP SSE transport server.
func NewSSEServer(toolReg *tool.Registry) *SSEServer {
	return &SSEServer{
		toolReg: toolReg,
		clients: make(map[string]*sseClient),
	}
}

// HandleSSE handles the SSE event stream connection.
func (s *SSEServer) HandleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	clientID := fmt.Sprintf("mcp-%d", s.nextID.Add(1))
	client := &sseClient{
		id:     clientID,
		events: make(chan []byte, 64),
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.clients[clientID] = client
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Send the endpoint event so the client knows where to POST.
	fmt.Fprintf(w, "event: endpoint\ndata: /mcp/messages?sessionId=%s\n\n", clientID)
	flusher.Flush()

	log.Printf("mcp-sse: client %s connected", clientID)

	// Stream events to client until disconnect.
	for {
		select {
		case data := <-client.events:
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			s.mu.Lock()
			delete(s.clients, clientID)
			close(client.done)
			s.mu.Unlock()
			log.Printf("mcp-sse: client %s disconnected", clientID)
			return
		}
	}
}

// HandleMessages handles JSON-RPC requests from SSE clients.
func (s *SSEServer) HandleMessages(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")

	s.mu.RLock()
	client, exists := s.clients[sessionID]
	s.mu.RUnlock()

	if !exists {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		sendJSONError(w, nil, -32700, "parse error")
		return
	}

	// Process the request using the shared handler.
	handler := &Server{toolReg: s.toolReg}
	resp := handler.handle(r.Context(), req)

	// Send response via SSE.
	data, _ := json.Marshal(resp)
	select {
	case client.events <- data:
	default:
		log.Printf("mcp-sse: client %s event buffer full, dropping", sessionID)
	}

	// Also respond with 202 Accepted.
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprintf(w, `{"status":"accepted"}`)
}

func sendJSONError(w http.ResponseWriter, id any, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0", ID: id,
		Error: &Error{Code: code, Message: msg},
	})
}
