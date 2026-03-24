package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SSETransport implements Transport over Server-Sent Events.
// The client connects via GET for events and sends requests via POST.
type SSETransport struct {
	url     string // Base SSE URL (GET endpoint).
	headers map[string]string
	name    string
	timeout time.Duration

	client     *http.Client
	messageURL string // POST endpoint received from server.
	nextID     atomic.Int64
	mu         sync.Mutex
	pending    map[int64]chan JSONRPCResponse
	healthy    atomic.Bool
	cancel     context.CancelFunc
}

// NewSSETransport creates an SSE transport for a remote MCP server.
func NewSSETransport(name, url string, headers map[string]string, timeout time.Duration) *SSETransport {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &SSETransport{
		url:     url,
		headers: headers,
		name:    name,
		timeout: timeout,
		client:  &http.Client{Timeout: 0}, // No timeout for SSE stream.
		pending: make(map[int64]chan JSONRPCResponse),
	}
}

func (t *SSETransport) Connect(ctx context.Context) error {
	// Create a cancelable context for the SSE stream.
	sseCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel

	// Connect to SSE endpoint.
	req, err := http.NewRequestWithContext(sseCtx, "GET", t.url, nil)
	if err != nil {
		cancel()
		return fmt.Errorf("sse %s: create request: %w", t.name, err)
	}
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return fmt.Errorf("sse %s: connect failed: %w", t.name, err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		cancel()
		return fmt.Errorf("sse %s: status %d", t.name, resp.StatusCode)
	}

	// Wait for the endpoint event that tells us where to POST.
	endpointCh := make(chan string, 1)
	go t.readSSEStream(resp.Body, endpointCh)

	select {
	case endpoint := <-endpointCh:
		if endpoint == "" {
			cancel()
			return fmt.Errorf("sse %s: no endpoint received", t.name)
		}
		// Resolve relative URL.
		if strings.HasPrefix(endpoint, "/") {
			// Extract base from the SSE URL.
			base := t.url
			if idx := strings.Index(base, "://"); idx != -1 {
				slashIdx := strings.Index(base[idx+3:], "/")
				if slashIdx != -1 {
					base = base[:idx+3+slashIdx]
				}
			}
			t.messageURL = base + endpoint
		} else {
			t.messageURL = endpoint
		}
	case <-time.After(10 * time.Second):
		cancel()
		return fmt.Errorf("sse %s: timeout waiting for endpoint event", t.name)
	case <-ctx.Done():
		cancel()
		return ctx.Err()
	}

	// MCP initialize handshake.
	initResp, err := t.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "sageclaw", "version": "0.5.0"},
		"capabilities":    map[string]any{},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("sse %s: initialize: %w", t.name, err)
	}

	log.Printf("mcp-client %s [sse]: initialized (protocol: %v)", t.name, initResp.Result)

	// Send initialized notification.
	t.postNotification("initialized", nil)

	t.healthy.Store(true)
	return nil
}

func (t *SSETransport) Call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	if t.messageURL == "" {
		return nil, fmt.Errorf("sse %s: not connected (no message URL)", t.name)
	}

	id := t.nextID.Add(1)

	var paramsData json.RawMessage
	if params != nil {
		paramsData, _ = json.Marshal(params)
	}

	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsData,
	}

	respCh := make(chan JSONRPCResponse, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()

	body, _ := json.Marshal(rpcReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.messageURL, bytes.NewReader(body))
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	postClient := &http.Client{Timeout: t.timeout}
	resp, err := postClient.Do(httpReq)
	if err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		t.healthy.Store(false)
		return nil, fmt.Errorf("sse %s: post failed: %w", t.name, err)
	}
	resp.Body.Close()

	// Wait for response via SSE stream.
	select {
	case rpcResp := <-respCh:
		return &rpcResp, nil
	case <-time.After(t.timeout):
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, fmt.Errorf("sse %s: call timeout", t.name)
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (t *SSETransport) Close() error {
	t.healthy.Store(false)
	if t.cancel != nil {
		t.cancel()
	}
	return nil
}

func (t *SSETransport) Healthy() bool {
	return t.healthy.Load()
}

func (t *SSETransport) readSSEStream(body io.ReadCloser, endpointCh chan<- string) {
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var eventType, eventData string
	endpointSent := false

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			// Empty line = dispatch event.
			if eventType == "endpoint" && !endpointSent {
				endpointCh <- strings.TrimSpace(eventData)
				endpointSent = true
			} else if eventType == "message" || eventType == "" {
				t.handleSSEMessage(eventData)
			}
			eventType = ""
			eventData = ""
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			eventData = strings.TrimPrefix(line, "data: ")
		}
	}

	if !endpointSent {
		endpointCh <- ""
	}
	t.healthy.Store(false)
}

func (t *SSETransport) handleSSEMessage(data string) {
	if data == "" {
		return
	}

	var resp JSONRPCResponse
	if err := json.Unmarshal([]byte(data), &resp); err != nil {
		return
	}

	if resp.ID != nil {
		var id int64
		switch v := resp.ID.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		}

		t.mu.Lock()
		ch, ok := t.pending[id]
		if ok {
			delete(t.pending, id)
		}
		t.mu.Unlock()

		if ok {
			ch <- resp
		}
	}
}

func (t *SSETransport) postNotification(method string, params any) {
	if t.messageURL == "" {
		return
	}

	var paramsData json.RawMessage
	if params != nil {
		paramsData, _ = json.Marshal(params)
	}

	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsData,
	}

	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequest("POST", t.messageURL, bytes.NewReader(body))
	if err != nil {
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	postClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := postClient.Do(httpReq)
	if err != nil {
		return
	}
	resp.Body.Close()
}
