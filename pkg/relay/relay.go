package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/tunnel"
	"nhooyr.io/websocket"
)

// Relay routes inbound HTTP requests to connected tunnel clients.
type Relay struct {
	mu      sync.RWMutex
	tunnels map[string]*TunnelConn // subdomain → connection
	tokens  TokenStore
	config  Config
}

// TunnelConn represents a connected tunnel client.
type TunnelConn struct {
	conn        *websocket.Conn
	subdomain   string
	instanceID  string
	connectedAt time.Time

	// Request tracking.
	mu       sync.Mutex
	pending  map[string]chan *tunnel.Message // request ID → response channel
	reqCount int
	sem      chan struct{} // concurrency limiter
}

// New creates a relay server.
func New(cfg Config, tokens TokenStore) *Relay {
	return &Relay{
		tunnels: make(map[string]*TunnelConn),
		tokens:  tokens,
		config:  cfg,
	}
}

// Handler returns the HTTP handler for the relay.
func (r *Relay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/connect", r.handleConnect)
	mux.HandleFunc("/health", r.handleHealth)
	mux.HandleFunc("/", r.handleProxy)
	return mux
}

// handleConnect upgrades to WebSocket and registers a tunnel.
func (r *Relay) handleConnect(w http.ResponseWriter, req *http.Request) {
	token := req.Header.Get("X-Tunnel-Token")
	instanceID := req.Header.Get("X-Instance-ID")
	protoVer := req.Header.Get("X-Protocol-Version")

	if token == "" || instanceID == "" {
		http.Error(w, "missing auth headers", http.StatusUnauthorized)
		return
	}

	// Check protocol version.
	if protoVer == "" || protoVer == "0" {
		http.Error(w, "unsupported protocol version", http.StatusBadRequest)
		return
	}

	// Determine subdomain.
	subdomain := allocateSubdomain(instanceID)

	// Validate token (if auth mode is "token").
	if r.config.Auth.Mode == "token" {
		existingSub, ok := r.tokens.Validate(token)
		if !ok {
			// New token — register it.
			if err := r.tokens.Register(token, subdomain, instanceID); err != nil {
				http.Error(w, "token registration failed", http.StatusInternalServerError)
				return
			}
		} else if existingSub != "" {
			subdomain = existingSub // use existing subdomain for this token
		}
		r.tokens.Touch(token)
	} else {
		log.Printf("relay: WARNING — auth mode is %q, any client can connect", r.config.Auth.Mode)
	}

	// Reserve a tunnel slot atomically before the WebSocket upgrade.
	// This prevents TOCTOU race where multiple connections pass the limit check.
	r.mu.Lock()
	existing, replacing := r.tunnels[subdomain]
	if !replacing && len(r.tunnels) >= r.config.Limits.MaxTunnels {
		r.mu.Unlock()
		http.Error(w, "relay at capacity", http.StatusServiceUnavailable)
		return
	}
	// Place a nil reservation to hold the slot during WebSocket upgrade.
	r.tunnels[subdomain] = nil
	r.mu.Unlock()

	// Close the old connection if replacing.
	if replacing && existing != nil {
		existing.conn.Close(websocket.StatusGoingAway, "replaced by new connection")
	}

	// Upgrade to WebSocket.
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		// InsecureSkipVerify disables origin checking (not TLS verification).
		// Tunnel clients connect from arbitrary origins, so origin check is not useful.
		InsecureSkipVerify: true,
	})
	if err != nil {
		// Upgrade failed — release the reservation.
		r.mu.Lock()
		if r.tunnels[subdomain] == nil {
			delete(r.tunnels, subdomain)
		}
		r.mu.Unlock()
		log.Printf("relay: WebSocket upgrade failed: %v", err)
		return
	}

	// Increase read limit for inline bodies (up to MaxBodySize).
	conn.SetReadLimit(tunnel.MaxBodySize + 1<<16)

	publicURL := fmt.Sprintf("https://%s.%s", subdomain, r.config.Domain)

	tc := &TunnelConn{
		conn:        conn,
		subdomain:   subdomain,
		instanceID:  instanceID,
		connectedAt: time.Now(),
		pending:     make(map[string]chan *tunnel.Message),
		sem:         make(chan struct{}, r.config.Limits.MaxConcurrentPerTunnel),
	}

	// Finalize the reservation with the actual connection.
	r.mu.Lock()
	r.tunnels[subdomain] = tc
	r.mu.Unlock()

	log.Printf("relay: tunnel connected: %s (subdomain: %s)", instanceID, subdomain)

	// Send ready message.
	readyMsg := tunnel.Message{
		Type:      tunnel.TypeReady,
		URL:       publicURL,
		Subdomain: subdomain,
	}
	data, _ := json.Marshal(readyMsg)
	conn.Write(req.Context(), websocket.MessageText, data)

	// Read messages from the tunnel client.
	r.readLoop(req.Context(), tc)

	// Cleanup on disconnect.
	r.mu.Lock()
	if r.tunnels[subdomain] == tc {
		delete(r.tunnels, subdomain)
	}
	r.mu.Unlock()

	// Drain pending requests — unblock proxy goroutines immediately
	// instead of making them wait for the 30s timeout.
	tc.mu.Lock()
	for id, ch := range tc.pending {
		close(ch)
		delete(tc.pending, id)
	}
	tc.mu.Unlock()

	log.Printf("relay: tunnel disconnected: %s", subdomain)
}

// readLoop reads messages from a tunnel client connection.
func (r *Relay) readLoop(ctx context.Context, tc *TunnelConn) {
	for {
		msgType, data, err := tc.conn.Read(ctx)
		if err != nil {
			return
		}

		if msgType == websocket.MessageBinary {
			// Binary frames for chunked body responses — route to pending request.
			// TODO: implement chunked response reassembly on relay side.
			continue
		}

		var msg tunnel.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		switch msg.Type {
		case tunnel.TypeResponse:
			tc.mu.Lock()
			if ch, ok := tc.pending[msg.ID]; ok {
				ch <- &msg
				delete(tc.pending, msg.ID)
			}
			tc.mu.Unlock()

		case tunnel.TypePing:
			pong := tunnel.Message{Type: tunnel.TypePong, Timestamp: msg.Timestamp}
			data, _ := json.Marshal(pong)
			tc.conn.Write(ctx, websocket.MessageText, data)

		case tunnel.TypeClose:
			tc.conn.Close(websocket.StatusNormalClosure, "client requested close")
			return
		}
	}
}

// handleProxy routes inbound HTTP requests to the appropriate tunnel.
func (r *Relay) handleProxy(w http.ResponseWriter, req *http.Request) {
	// Extract subdomain from Host header.
	subdomain := extractSubdomain(req.Host, r.config.Domain)
	if subdomain == "" {
		http.Error(w, "no tunnel specified", http.StatusBadRequest)
		return
	}

	// Look up tunnel (skip nil reservations from in-progress connections).
	r.mu.RLock()
	tc, ok := r.tunnels[subdomain]
	r.mu.RUnlock()
	if !ok || tc == nil {
		http.Error(w, "tunnel not connected", http.StatusBadGateway)
		return
	}

	// Acquire concurrency slot.
	select {
	case tc.sem <- struct{}{}:
		defer func() { <-tc.sem }()
	default:
		http.Error(w, "too many concurrent requests", http.StatusTooManyRequests)
		return
	}

	// Check body size.
	if req.ContentLength > r.config.Limits.MaxBodySize {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// Read request body (bounded).
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(req.Body, r.config.Limits.MaxBodySize+1))
		req.Body.Close()
		if err != nil {
			http.Error(w, "error reading body", http.StatusBadRequest)
			return
		}
		if int64(len(body)) > r.config.Limits.MaxBodySize {
			http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
			return
		}
	}

	// Build headers.
	headers := make(map[string]string)
	for k := range req.Header {
		headers[k] = req.Header.Get(k)
	}
	// Add forwarded headers.
	clientIP := extractClientIP(req)
	headers["X-Forwarded-For"] = clientIP
	headers["X-Real-IP"] = clientIP
	headers["X-Forwarded-Proto"] = "https"

	// Generate request ID.
	tc.mu.Lock()
	tc.reqCount++
	reqID := fmt.Sprintf("r-%d-%d", time.Now().UnixMilli(), tc.reqCount)
	responseCh := make(chan *tunnel.Message, 1)
	tc.pending[reqID] = responseCh
	tc.mu.Unlock()

	// Build tunnel request message.
	reqMsg := tunnel.Message{
		Type:    tunnel.TypeRequest,
		ID:      reqID,
		Method:  req.Method,
		Path:    req.URL.RequestURI(),
		Headers: headers,
	}
	if len(body) > 0 {
		reqMsg.Body = body
	}

	data, _ := json.Marshal(reqMsg)
	writeCtx, cancel := context.WithTimeout(req.Context(), 5*time.Second)
	err := tc.conn.Write(writeCtx, websocket.MessageText, data)
	cancel()
	if err != nil {
		tc.mu.Lock()
		delete(tc.pending, reqID)
		tc.mu.Unlock()
		http.Error(w, "tunnel write error", http.StatusBadGateway)
		return
	}

	// Wait for response.
	select {
	case resp, ok := <-responseCh:
		if !ok || resp == nil {
			// Channel closed — tunnel disconnected.
			http.Error(w, "tunnel disconnected", http.StatusBadGateway)
			return
		}
		for k, v := range resp.Headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(resp.Status)
		if len(resp.Body) > 0 {
			w.Write(resp.Body)
		} else if resp.BodyB64 != "" {
			decoded, _ := base64.StdEncoding.DecodeString(resp.BodyB64)
			w.Write(decoded)
		}
	case <-time.After(30 * time.Second):
		tc.mu.Lock()
		delete(tc.pending, reqID)
		tc.mu.Unlock()
		http.Error(w, "tunnel response timeout", http.StatusGatewayTimeout)
	case <-req.Context().Done():
		tc.mu.Lock()
		delete(tc.pending, reqID)
		tc.mu.Unlock()
	}
}

// handleHealth returns relay status.
func (r *Relay) handleHealth(w http.ResponseWriter, req *http.Request) {
	r.mu.RLock()
	count := len(r.tunnels)
	r.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"tunnels": count,
		"limit":   r.config.Limits.MaxTunnels,
	})
}

// TunnelCount returns the number of connected tunnels.
func (r *Relay) TunnelCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tunnels)
}

// allocateSubdomain generates a stable subdomain from an instance ID.
func allocateSubdomain(instanceID string) string {
	h := sha256.Sum256([]byte(instanceID))
	return fmt.Sprintf("%x", h[:4]) // 8 hex chars
}

// extractSubdomain extracts the subdomain from a Host header.
func extractSubdomain(host, domain string) string {
	// Strip port.
	if idx := strings.LastIndex(host, ":"); idx > 0 {
		host = host[:idx]
	}

	if !strings.HasSuffix(host, "."+domain) {
		return ""
	}

	subdomain := strings.TrimSuffix(host, "."+domain)
	if subdomain == "" || strings.Contains(subdomain, ".") {
		return ""
	}
	return subdomain
}

// extractClientIP returns the client's IP address from RemoteAddr.
func extractClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = ip[:idx]
	}
	return ip
}
