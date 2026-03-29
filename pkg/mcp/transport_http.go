package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync/atomic"
	"time"
)

// HTTPTransport implements Transport over streamable-HTTP (POST JSON-RPC).
type HTTPTransport struct {
	url     string
	headers map[string]string
	name    string
	timeout time.Duration

	client  *http.Client
	nextID  atomic.Int64
	healthy atomic.Bool
}

// NewHTTPTransport creates a streamable-HTTP transport for a remote MCP server.
func NewHTTPTransport(name, url string, headers map[string]string, timeout time.Duration) *HTTPTransport {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	return &HTTPTransport{
		url:     url,
		headers: headers,
		name:    name,
		timeout: timeout,
		client:  &http.Client{Timeout: timeout},
	}
}

func (t *HTTPTransport) Connect(ctx context.Context) error {
	// MCP initialize handshake.
	initResp, err := t.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "sageclaw", "version": "0.5.0"},
		"capabilities":    map[string]any{},
	})
	if err != nil {
		return fmt.Errorf("http %s: initialize: %w", t.name, err)
	}

	log.Printf("mcp-client %s [http]: initialized (protocol: %v)", t.name, initResp.Result)

	// Send initialized notification (fire and forget).
	t.callIgnoreResponse(ctx, "initialized", nil)

	t.healthy.Store(true)
	return nil
}

func (t *HTTPTransport) Call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
	id := t.nextID.Add(1)

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

	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("http %s: create request: %w", t.name, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		t.healthy.Store(false)
		return nil, fmt.Errorf("http %s: request failed: %w", t.name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		t.healthy.Store(false)
		return nil, fmt.Errorf("http %s: status %d: %s", t.name, resp.StatusCode, string(bodyBytes))
	}

	var rpcResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, fmt.Errorf("http %s: decode response: %w", t.name, err)
	}

	t.healthy.Store(true)
	return &rpcResp, nil
}

func (t *HTTPTransport) Close() error {
	t.healthy.Store(false)
	return nil
}

func (t *HTTPTransport) Healthy() bool {
	return t.healthy.Load()
}

func (t *HTTPTransport) callIgnoreResponse(ctx context.Context, method string, params any) {
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
	httpReq, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(body))
	if err != nil {
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := t.client.Do(httpReq)
	if err != nil {
		return
	}
	resp.Body.Close()
}
