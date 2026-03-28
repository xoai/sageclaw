package tunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"

	"nhooyr.io/websocket"
)

// testRelay is an in-process relay stub for testing the tunnel client.
// It accepts WebSocket connections, forwards HTTP requests, and returns responses.
type testRelay struct {
	mu       sync.Mutex
	server   *httptest.Server
	conns    []*websocket.Conn
	url      string // ws://localhost:PORT/connect
	subdomain string
	publicURL string

	// onRequest is called for each incoming request to the relay's public endpoint.
	// If nil, requests are forwarded to the connected tunnel client.
	onRequest func(w http.ResponseWriter, r *http.Request)
}

// newTestRelay creates a test relay that accepts tunnel connections.
func newTestRelay(subdomain string) *testRelay {
	tr := &testRelay{
		subdomain: subdomain,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/connect", tr.handleConnect)
	mux.HandleFunc("/", tr.handlePublic)

	tr.server = httptest.NewServer(mux)
	tr.url = "ws" + strings.TrimPrefix(tr.server.URL, "http") + "/connect"
	tr.publicURL = fmt.Sprintf("https://%s.sageclaw.io", subdomain)

	return tr
}

func (tr *testRelay) handleConnect(w http.ResponseWriter, r *http.Request) {
	// Verify auth headers.
	token := r.Header.Get("X-Tunnel-Token")
	if token == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		return
	}

	tr.mu.Lock()
	tr.conns = append(tr.conns, conn)
	tr.mu.Unlock()

	// Send ready message.
	ready := &Message{
		Type:      TypeReady,
		URL:       tr.publicURL,
		Subdomain: tr.subdomain,
	}
	data, _ := json.Marshal(ready)
	conn.Write(r.Context(), websocket.MessageText, data)

	// Keep connection alive — read messages and handle pings.
	for {
		_, _, err := conn.Read(r.Context())
		if err != nil {
			return
		}
	}
}

func (tr *testRelay) handlePublic(w http.ResponseWriter, r *http.Request) {
	if tr.onRequest != nil {
		tr.onRequest(w, r)
		return
	}

	// Forward to connected tunnel client.
	tr.mu.Lock()
	if len(tr.conns) == 0 {
		tr.mu.Unlock()
		http.Error(w, "no tunnel connected", http.StatusBadGateway)
		return
	}
	conn := tr.conns[len(tr.conns)-1]
	tr.mu.Unlock()

	// Read request body.
	body, _ := io.ReadAll(io.LimitReader(r.Body, MaxBodySize))

	// Build headers map.
	headers := make(map[string]string)
	for k := range r.Header {
		headers[k] = r.Header.Get(k)
	}

	reqMsg := &Message{
		Type:    TypeRequest,
		ID:      fmt.Sprintf("req-%d", nextID()),
		Method:  r.Method,
		Path:    r.URL.Path,
		Headers: headers,
		Body:    body,
	}
	data, _ := json.Marshal(reqMsg)
	conn.Write(r.Context(), websocket.MessageText, data)

	// Wait for response (simplified — in real relay this would be async).
	// For test purposes, the response comes back on the same connection.
	_, respData, err := conn.Read(r.Context())
	if err != nil {
		http.Error(w, "tunnel read error", http.StatusBadGateway)
		return
	}

	var respMsg Message
	if err := json.Unmarshal(respData, &respMsg); err != nil {
		http.Error(w, "bad response", http.StatusBadGateway)
		return
	}

	for k, v := range respMsg.Headers {
		w.Header().Set(k, v)
	}
	w.WriteHeader(respMsg.Status)
	w.Write(respMsg.Body)
}

func (tr *testRelay) close() {
	tr.mu.Lock()
	for _, c := range tr.conns {
		c.Close(websocket.StatusNormalClosure, "test done")
	}
	tr.mu.Unlock()
	tr.server.Close()
}

// sendToClient sends a message to the most recently connected tunnel client.
func (tr *testRelay) sendToClient(ctx context.Context, msg *Message) error {
	tr.mu.Lock()
	if len(tr.conns) == 0 {
		tr.mu.Unlock()
		return fmt.Errorf("no connections")
	}
	conn := tr.conns[len(tr.conns)-1]
	tr.mu.Unlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.Write(ctx, websocket.MessageText, data)
}

// readFromClient reads a message from the most recently connected tunnel client.
func (tr *testRelay) readFromClient(ctx context.Context) (*Message, error) {
	tr.mu.Lock()
	if len(tr.conns) == 0 {
		tr.mu.Unlock()
		return nil, fmt.Errorf("no connections")
	}
	conn := tr.conns[len(tr.conns)-1]
	tr.mu.Unlock()

	_, data, err := conn.Read(ctx)
	if err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// getFreePort returns a free TCP port.
func getFreePort() int {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

var (
	idMu  sync.Mutex
	idSeq int
)

func nextID() int {
	idMu.Lock()
	defer idMu.Unlock()
	idSeq++
	return idSeq
}
