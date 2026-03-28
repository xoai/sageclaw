package tunnel

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nhooyr.io/websocket"
)

// Status represents the tunnel's current state.
type Status struct {
	Running   bool   `json:"running"`
	URL       string `json:"url"`
	LocalPort int    `json:"local_port"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Mode      string `json:"mode"`       // "managed", "self-hosted"
	TwoFA     bool   `json:"two_fa"`     // Whether 2FA is enabled
	Latency   int    `json:"latency_ms"` // Last measured relay latency
}

// Client manages a tunnel connection to a relay server.
type Client struct {
	mu         sync.RWMutex
	localPort  int
	relayURL   string
	token      string
	instanceID string
	conn       *websocket.Conn
	status     Status
	onChange   func(Status)
	onReady    []func(string) // called with public URL when tunnel is ready
	httpClient *http.Client
	cancel     context.CancelFunc
	done       chan struct{}
	sem        chan struct{} // concurrency semaphore for forwarded requests

	// Heartbeat pong tracking.
	lastPong atomic.Int64 // unix millis of last pong received

	// Chunked inbound request assembly.
	chunkMu       sync.Mutex
	chunkBuffer   map[string]*chunkedRequest // request ID → in-progress chunked request
	activeChunkID string                     // most recently started chunked request ID
}

// chunkedRequest accumulates binary frames for a single chunked inbound request.
type chunkedRequest struct {
	msg    *Message // header from request_start (method, path, headers)
	chunks []byte   // accumulated body bytes
}

// NewClient creates a tunnel client. The db is used to load/generate
// instance ID and tunnel token. Config provides relay URL and mode.
func NewClient(db *sql.DB, cfg Config, onChange func(Status)) (*Client, error) {
	instanceID, err := EnsureInstanceID(db)
	if err != nil {
		return nil, fmt.Errorf("tunnel: instance ID: %w", err)
	}

	token, err := EnsureToken(db)
	if err != nil {
		return nil, fmt.Errorf("tunnel: token: %w", err)
	}

	relayURL := cfg.RelayURL
	if relayURL == "" {
		relayURL = DefaultRelayURL
	}

	// For self-hosted mode with explicit token, use that instead.
	if cfg.Token != "" {
		token = cfg.Token
	}

	return &Client{
		localPort:   cfg.LocalPort,
		relayURL:    relayURL,
		token:       token,
		instanceID:  instanceID,
		onChange:     onChange,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		sem:         make(chan struct{}, MaxConcurrentRequests),
		chunkBuffer: make(map[string]*chunkedRequest),
		status: Status{
			LocalPort: cfg.LocalPort,
			Mode:      cfg.Mode,
		},
	}, nil
}

// OnReady adds a callback invoked when the tunnel receives the ready
// message with its public URL. Multiple callbacks can be registered.
func (c *Client) OnReady(fn func(url string)) {
	c.mu.Lock()
	c.onReady = append(c.onReady, fn)
	c.mu.Unlock()
}

// Start connects to the relay and begins forwarding requests.
// The provided context controls the tunnel lifecycle.
func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.status.Running {
		c.mu.Unlock()
		return fmt.Errorf("tunnel already running")
	}

	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.done = make(chan struct{})
	c.status.Running = true
	c.status.StartedAt = time.Now().Format(time.RFC3339)
	c.status.Error = ""
	c.mu.Unlock()
	c.notify()

	go c.runLoop(ctx)
	return nil
}

// Stop disconnects from the relay.
func (c *Client) Stop() error {
	c.mu.Lock()
	if !c.status.Running {
		c.mu.Unlock()
		return fmt.Errorf("tunnel not running")
	}
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done // wait for runLoop to exit
	}

	c.mu.Lock()
	c.status.Running = false
	c.status.URL = ""
	c.status.Error = ""
	c.status.Latency = 0
	c.mu.Unlock()
	c.notify()
	return nil
}

// GetStatus returns current tunnel state.
func (c *Client) GetStatus() Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.status
}

// runLoop maintains the relay connection with exponential backoff reconnection.
func (c *Client) runLoop(ctx context.Context) {
	defer close(c.done)

	backoff := time.Second
	maxBackoff := 60 * time.Second

	for {
		connStart := time.Now()
		err := c.connectAndServe(ctx)
		if ctx.Err() != nil {
			return // shutdown requested
		}

		// Reset backoff if the connection was healthy (lasted > 10s).
		if time.Since(connStart) > 10*time.Second {
			backoff = time.Second
		}

		c.mu.Lock()
		c.status.URL = ""
		c.status.Latency = 0
		c.status.Error = fmt.Sprintf("disconnected: %v — reconnecting in %s", err, backoff)
		c.mu.Unlock()
		c.notify()

		log.Printf("tunnel: disconnected: %v — reconnecting in %s", err, backoff)

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectAndServe establishes a single WebSocket connection and processes
// messages until disconnected. Returns the disconnect error.
func (c *Client) connectAndServe(ctx context.Context) error {
	headers := http.Header{
		"X-Tunnel-Token":     {c.token},
		"X-Instance-ID":      {c.instanceID},
		"X-Protocol-Version": {strconv.Itoa(ProtocolVersion)},
	}

	conn, _, err := websocket.Dial(ctx, c.relayURL, &websocket.DialOptions{
		HTTPHeader: headers,
	})
	if err != nil {
		return fmt.Errorf("dial relay: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "client shutdown")

	// Increase read limit for inline bodies (up to MaxBodySize).
	conn.SetReadLimit(MaxBodySize + 1<<16) // 10MB + overhead

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Reset pong timestamp on new connection.
	c.lastPong.Store(time.Now().UnixMilli())

	log.Printf("tunnel: connected to relay %s", c.relayURL)

	// Start heartbeat with pong timeout detection.
	heartCtx, heartCancel := context.WithCancel(ctx)
	defer heartCancel()
	go c.heartbeat(heartCtx, conn)

	// Read messages.
	for {
		msgType, data, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if msgType == websocket.MessageBinary {
			// Binary frames are chunked body data — dispatch to active chunked request.
			c.handleBinaryFrame(data)
			continue
		}

		msg, err := DecodeMessage(data)
		if err != nil {
			log.Printf("tunnel: bad message: %v", err)
			continue
		}

		switch msg.Type {
		case TypeReady:
			c.handleReady(msg)

		case TypeRequest:
			c.handleRequest(ctx, conn, msg)

		case TypeRequestStart:
			c.handleRequestStart(msg)

		case TypeRequestEnd:
			c.handleRequestEnd(ctx, conn, msg)

		case TypePong:
			c.handlePong(msg)

		case TypeError:
			log.Printf("tunnel: relay error: [%d] %s", msg.Code, msg.Message)

		case TypeClose:
			log.Printf("tunnel: relay closing: %s", msg.Reason)
			return fmt.Errorf("relay closed: %s", msg.Reason)

		default:
			log.Printf("tunnel: unknown message type: %s", msg.Type)
		}
	}
}

func (c *Client) handleReady(msg *Message) {
	c.mu.Lock()
	c.status.URL = msg.URL
	c.status.Error = ""
	callbacks := make([]func(string), len(c.onReady))
	copy(callbacks, c.onReady)
	c.mu.Unlock()
	c.notify()

	log.Printf("tunnel: ready at %s (subdomain: %s)", msg.URL, msg.Subdomain)

	for _, fn := range callbacks {
		go fn(msg.URL)
	}
}

func (c *Client) handleRequest(ctx context.Context, conn *websocket.Conn, msg *Message) {
	// Acquire semaphore slot.
	select {
	case c.sem <- struct{}{}:
	default:
		// At capacity — send 429 response.
		c.sendResponse(ctx, conn, msg.ID, http.StatusTooManyRequests,
			map[string]string{"Content-Type": "text/plain"},
			[]byte("too many concurrent requests"))
		return
	}

	go func() {
		defer func() { <-c.sem }()
		c.forwardRequest(ctx, conn, msg)
	}()
}

// handleRequestStart begins a chunked inbound request assembly.
func (c *Client) handleRequestStart(msg *Message) {
	c.chunkMu.Lock()
	c.chunkBuffer[msg.ID] = &chunkedRequest{msg: msg}
	c.activeChunkID = msg.ID
	c.chunkMu.Unlock()
}

// handleBinaryFrame appends data to the active chunked request by ID.
func (c *Client) handleBinaryFrame(data []byte) {
	c.chunkMu.Lock()
	defer c.chunkMu.Unlock()

	cr, ok := c.chunkBuffer[c.activeChunkID]
	if !ok {
		return // no active chunked request
	}
	cr.chunks = append(cr.chunks, data...)
	if len(cr.chunks) > MaxBodySize {
		cr.chunks = cr.chunks[:MaxBodySize]
	}
}

// handleRequestEnd completes a chunked inbound request and forwards it.
func (c *Client) handleRequestEnd(ctx context.Context, conn *websocket.Conn, msg *Message) {
	c.chunkMu.Lock()
	cr, ok := c.chunkBuffer[msg.ID]
	delete(c.chunkBuffer, msg.ID)
	c.chunkMu.Unlock()

	if !ok {
		log.Printf("tunnel: request_end for unknown request %s", msg.ID)
		return
	}

	// Reconstruct the full request message with the assembled body.
	fullMsg := cr.msg
	fullMsg.Body = cr.chunks

	c.handleRequest(ctx, conn, fullMsg)
}

func (c *Client) handlePong(msg *Message) {
	now := time.Now().UnixMilli()
	c.lastPong.Store(now)

	if msg.Timestamp > 0 {
		latency := now - msg.Timestamp
		c.mu.Lock()
		c.status.Latency = int(latency)
		c.mu.Unlock()
	}
}

// heartbeat sends pings every 30s and closes the connection if no pong
// is received within 10s of a ping.
func (c *Client) heartbeat(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ping := &Message{
				Type:      TypePing,
				Timestamp: time.Now().UnixMilli(),
			}
			data, _ := ping.Encode()
			writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Write(writeCtx, websocket.MessageText, data)
			cancel()
			if err != nil {
				log.Printf("tunnel: heartbeat write failed: %v", err)
				return
			}

			// Wait 10s and check if a pong was received.
			time.Sleep(10 * time.Second)
			lastPong := c.lastPong.Load()
			if time.Since(time.UnixMilli(lastPong)) > 40*time.Second {
				// No pong received since before the ping — connection is dead.
				log.Printf("tunnel: no pong received — closing connection")
				conn.Close(websocket.StatusGoingAway, "pong timeout")
				return
			}
		}
	}
}

// forwardRequest takes an inbound request from the relay and forwards it
// to the local HTTP server, then sends the response back.
func (c *Client) forwardRequest(ctx context.Context, conn *websocket.Conn, msg *Message) {
	// Reconstruct the body.
	var body io.Reader
	if len(msg.Body) > 0 {
		body = bytes.NewReader(msg.Body)
	} else if msg.BodyB64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(msg.BodyB64)
		if err != nil {
			log.Printf("tunnel: bad base64 body for %s: %v", msg.ID, err)
			c.sendResponse(ctx, conn, msg.ID, http.StatusBadRequest,
				map[string]string{"Content-Type": "text/plain"},
				[]byte("invalid body encoding"))
			return
		}
		body = bytes.NewReader(decoded)
	}

	// Build local request.
	localURL := fmt.Sprintf("http://localhost:%d%s", c.localPort, msg.Path)
	req, err := http.NewRequestWithContext(ctx, msg.Method, localURL, body)
	if err != nil {
		log.Printf("tunnel: bad request %s: %v", msg.ID, err)
		c.sendResponse(ctx, conn, msg.ID, http.StatusBadGateway,
			map[string]string{"Content-Type": "text/plain"},
			[]byte("failed to construct request"))
		return
	}

	// Copy headers from the relay message.
	for k, v := range msg.Headers {
		req.Header.Set(k, v)
	}

	// Forward to local server.
	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Printf("tunnel: forward %s %s failed: %v", msg.Method, msg.Path, err)
		c.sendResponse(ctx, conn, msg.ID, http.StatusBadGateway,
			map[string]string{"Content-Type": "text/plain"},
			[]byte("local server unavailable"))
		return
	}
	defer resp.Body.Close()

	// Read response body (bounded to MaxInlineBody — chunked transfer deferred to v2).
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, MaxInlineBody))
	if err != nil {
		log.Printf("tunnel: read response for %s: %v", msg.ID, err)
		c.sendResponse(ctx, conn, msg.ID, http.StatusBadGateway,
			map[string]string{"Content-Type": "text/plain"},
			[]byte("failed to read local response"))
		return
	}

	// Collect response headers.
	respHeaders := make(map[string]string, len(resp.Header))
	for k := range resp.Header {
		respHeaders[k] = resp.Header.Get(k)
	}

	c.sendResponse(ctx, conn, msg.ID, resp.StatusCode, respHeaders, respBody)
}

// sendResponse frames and sends a response message back to the relay.
func (c *Client) sendResponse(ctx context.Context, conn *websocket.Conn, reqID string, status int, headers map[string]string, body []byte) {
	respMsg := &Message{
		Type:    TypeResponse,
		ID:      reqID,
		Status:  status,
		Headers: headers,
	}

	// Inline body — use text for text content, base64 for binary.
	// Chunked transfer deferred to v2 (relay cannot reassemble yet).
	if len(body) > 0 {
		if isTextContent(headers) {
			respMsg.Body = body
		} else {
			respMsg.BodyB64 = base64.StdEncoding.EncodeToString(body)
		}
	}

	data, err := respMsg.Encode()
	if err != nil {
		log.Printf("tunnel: encode response %s: %v", reqID, err)
		return
	}

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		log.Printf("tunnel: write response %s: %v", reqID, err)
	}
}

func (c *Client) notify() {
	c.mu.RLock()
	fn := c.onChange
	s := c.status
	c.mu.RUnlock()
	if fn != nil {
		fn(s)
	}
}

// isTextContent checks if the Content-Type header indicates text content.
func isTextContent(headers map[string]string) bool {
	ct := headers["Content-Type"]
	if ct == "" {
		ct = headers["content-type"]
	}
	return strings.HasPrefix(ct, "text/") ||
		strings.HasPrefix(ct, "application/json") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.Contains(ct, "+json") ||
		strings.Contains(ct, "+xml")
}
