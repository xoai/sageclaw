package tunnel

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
	"nhooyr.io/websocket"
)

// testDB creates an in-memory SQLite database with the settings table.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS settings (
			key         TEXT PRIMARY KEY,
			value       TEXT NOT NULL,
			updated_at  TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

// startLocalServer starts an HTTP server on a random free port.
func startLocalServer(t *testing.T, handler http.Handler) (int, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: handler}
	go srv.Serve(l)
	return port, func() { srv.Close() }
}

// startRelayStub starts a WebSocket relay stub that sends a ready message
// and optionally forwards a request, returning the response.
func startRelayStub(t *testing.T, opts relayOpts) (wsURL string, cleanup func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if opts.onHeaders != nil {
			opts.onHeaders(r.Header)
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")

		// Send ready.
		readyMsg := Message{
			Type:      TypeReady,
			URL:       opts.publicURL,
			Subdomain: opts.subdomain,
		}
		data, _ := json.Marshal(readyMsg)
		conn.Write(r.Context(), websocket.MessageText, data)

		if opts.readyCh != nil {
			opts.readyCh <- struct{}{}
		}

		// Optionally send a request after a short delay.
		if opts.sendReq != nil {
			time.Sleep(100 * time.Millisecond)
			data, _ := json.Marshal(opts.sendReq)
			conn.Write(r.Context(), websocket.MessageText, data)
		}

		// Read messages and dispatch responses.
		for {
			_, msgData, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			var msg Message
			if json.Unmarshal(msgData, &msg) != nil {
				continue
			}
			switch msg.Type {
			case TypeResponse:
				if opts.responseCh != nil {
					opts.responseCh <- &msg
				}
			case TypePing:
				// Reply with pong.
				pong := Message{Type: TypePong, Timestamp: msg.Timestamp}
				data, _ := json.Marshal(pong)
				conn.Write(r.Context(), websocket.MessageText, data)
			}
		}
	}))

	wsURL = "ws" + strings.TrimPrefix(server.URL, "http")
	return wsURL, server.Close
}

type relayOpts struct {
	publicURL  string
	subdomain  string
	readyCh    chan struct{}
	sendReq    *Message
	responseCh chan *Message
	onHeaders  func(http.Header)
}

// --- Protocol Tests ---

func TestProtocolEncodeDecode(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{
			name: "ready",
			msg:  Message{Type: TypeReady, URL: "https://abc.sageclaw.io", Subdomain: "abc"},
		},
		{
			name: "request",
			msg: Message{
				Type:    TypeRequest,
				ID:      "req-1",
				Method:  "POST",
				Path:    "/webhook/whatsapp",
				Headers: map[string]string{"Content-Type": "application/json"},
				Body:    []byte(`{"test": true}`),
			},
		},
		{
			name: "response",
			msg: Message{
				Type:    TypeResponse,
				ID:      "req-1",
				Status:  200,
				Headers: map[string]string{"Content-Type": "application/json"},
				Body:    []byte(`{"ok": true}`),
			},
		},
		{
			name: "ping",
			msg:  Message{Type: TypePing, Timestamp: time.Now().UnixMilli()},
		},
		{
			name: "error",
			msg:  Message{Type: TypeError, Code: 401, Message: "unauthorized"},
		},
		{
			name: "close",
			msg:  Message{Type: TypeClose, Reason: "shutdown"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := tt.msg.Encode()
			if err != nil {
				t.Fatalf("encode: %v", err)
			}

			decoded, err := DecodeMessage(data)
			if err != nil {
				t.Fatalf("decode: %v", err)
			}

			if decoded.Type != tt.msg.Type {
				t.Errorf("type: got %q, want %q", decoded.Type, tt.msg.Type)
			}
		})
	}
}

func TestDecodeMessageMissingType(t *testing.T) {
	_, err := DecodeMessage([]byte(`{"id": "test"}`))
	if err == nil {
		t.Fatal("expected error for missing type")
	}
}

func TestDecodeMessageBadJSON(t *testing.T) {
	_, err := DecodeMessage([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

// --- Instance ID Tests ---

func TestEnsureInstanceID(t *testing.T) {
	db := testDB(t)

	id1, err := EnsureInstanceID(db)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if id1 == "" {
		t.Fatal("expected non-empty instance ID")
	}

	id2, err := EnsureInstanceID(db)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if id1 != id2 {
		t.Errorf("instance ID changed: %q vs %q", id1, id2)
	}
}

// --- Token Tests ---

func TestEnsureToken(t *testing.T) {
	db := testDB(t)

	tok1, err := EnsureToken(db)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(tok1) != tokenBytes*2 {
		t.Errorf("token length: got %d, want %d", len(tok1), tokenBytes*2)
	}

	tok2, err := EnsureToken(db)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if tok1 != tok2 {
		t.Errorf("token changed: %q vs %q", tok1, tok2)
	}
}

func TestRotateToken(t *testing.T) {
	db := testDB(t)

	tok1, _ := EnsureToken(db)
	tok2, err := RotateToken(db)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if tok1 == tok2 {
		t.Error("rotated token should differ from original")
	}

	tok3, _ := EnsureToken(db)
	if tok2 != tok3 {
		t.Error("rotated token not persisted")
	}
}

// --- Config Tests ---

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}.Defaults()
	if cfg.Mode != "managed" {
		t.Errorf("mode: got %q, want %q", cfg.Mode, "managed")
	}
	if cfg.RelayURL != DefaultRelayURL {
		t.Errorf("relay URL: got %q, want %q", cfg.RelayURL, DefaultRelayURL)
	}
}

func TestConfigEnabled(t *testing.T) {
	tests := []struct {
		mode    string
		enabled bool
	}{
		{"", true},
		{"managed", true},
		{"self-hosted", true},
		{"disabled", false},
	}
	for _, tt := range tests {
		cfg := Config{Mode: tt.mode}
		if cfg.Enabled() != tt.enabled {
			t.Errorf("mode %q: enabled=%v, want %v", tt.mode, cfg.Enabled(), tt.enabled)
		}
	}
}

// --- Client Connect/Disconnect Tests ---

func TestClientConnectAndReady(t *testing.T) {
	db := testDB(t)

	readyCh := make(chan struct{}, 1)
	var gotToken, gotInstanceID string

	wsURL, cleanup := startRelayStub(t, relayOpts{
		publicURL: "https://test.sageclaw.io",
		subdomain: "test",
		readyCh:   readyCh,
		onHeaders: func(h http.Header) {
			gotToken = h.Get("X-Tunnel-Token")
			gotInstanceID = h.Get("X-Instance-ID")
		},
	})
	defer cleanup()

	statusCh := make(chan Status, 5)
	client, err := NewClient(db, Config{Mode: "managed", RelayURL: wsURL, LocalPort: 9999}, func(s Status) {
		statusCh <- s
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Wait for ready.
	select {
	case <-readyCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for ready")
	}

	// Wait for URL to be set in status.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case s := <-statusCh:
			if s.URL == "https://test.sageclaw.io" {
				goto verified
			}
		case <-deadline:
			t.Fatal("timeout waiting for URL in status")
		}
	}
verified:

	if gotToken == "" {
		t.Error("relay did not receive token")
	}
	if gotInstanceID == "" {
		t.Error("relay did not receive instance ID")
	}

	status := client.GetStatus()
	if !status.Running {
		t.Error("expected running=true")
	}

	if err := client.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	status = client.GetStatus()
	if status.Running {
		t.Error("expected running=false after stop")
	}
}

// --- Request Forwarding Tests ---

func TestClientRequestForwarding(t *testing.T) {
	db := testDB(t)

	// Local HTTP server that the tunnel forwards to.
	localPort, localCleanup := startLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"path": %q, "method": %q}`, r.URL.Path, r.Method)
	}))
	defer localCleanup()

	readyCh := make(chan struct{}, 1)
	responseCh := make(chan *Message, 1)

	wsURL, cleanup := startRelayStub(t, relayOpts{
		publicURL:  "https://test.sageclaw.io",
		subdomain:  "test",
		readyCh:    readyCh,
		responseCh: responseCh,
		sendReq: &Message{
			Type:    TypeRequest,
			ID:      "req-001",
			Method:  "GET",
			Path:    "/api/status",
			Headers: map[string]string{"Accept": "application/json"},
		},
	})
	defer cleanup()

	client, err := NewClient(db, Config{Mode: "managed", RelayURL: wsURL, LocalPort: localPort}, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client.Start(ctx)
	defer client.Stop()

	select {
	case <-readyCh:
	case <-ctx.Done():
		t.Fatal("timeout waiting for ready")
	}

	select {
	case resp := <-responseCh:
		if resp.ID != "req-001" {
			t.Errorf("response ID: %q", resp.ID)
		}
		if resp.Status != 200 {
			t.Errorf("response status: %d", resp.Status)
		}
		body := string(resp.Body)
		if !strings.Contains(body, `"/api/status"`) {
			t.Errorf("response body missing path: %s", body)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for response")
	}
}

// --- Base64 Body Test ---

func TestClientBase64BodyForwarding(t *testing.T) {
	db := testDB(t)

	localPort, localCleanup := startLocalServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(200)
		w.Write([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})
	}))
	defer localCleanup()

	readyCh := make(chan struct{}, 1)
	responseCh := make(chan *Message, 1)

	wsURL, cleanup := startRelayStub(t, relayOpts{
		publicURL:  "https://test.sageclaw.io",
		subdomain:  "test",
		readyCh:    readyCh,
		responseCh: responseCh,
		sendReq:    &Message{Type: TypeRequest, ID: "req-bin", Method: "GET", Path: "/image.png"},
	})
	defer cleanup()

	client, err := NewClient(db, Config{Mode: "managed", RelayURL: wsURL, LocalPort: localPort}, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client.Start(ctx)
	defer client.Stop()

	select {
	case <-readyCh:
	case <-ctx.Done():
		t.Fatal("timeout")
	}

	select {
	case resp := <-responseCh:
		if resp.BodyB64 == "" {
			t.Fatal("expected base64 body for binary content")
		}
		decoded, err := base64.StdEncoding.DecodeString(resp.BodyB64)
		if err != nil {
			t.Fatalf("decode base64: %v", err)
		}
		if len(decoded) < 2 || decoded[0] != 0x89 || decoded[1] != 0x50 {
			t.Error("decoded body doesn't match PNG header")
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for response")
	}
}

// --- isTextContent Tests ---

func TestIsTextContent(t *testing.T) {
	tests := []struct {
		ct   string
		text bool
	}{
		{"text/html", true},
		{"application/json", true},
		{"application/xml", true},
		{"application/vnd.api+json", true},
		{"image/png", false},
		{"application/octet-stream", false},
		{"", false},
	}
	for _, tt := range tests {
		headers := map[string]string{"Content-Type": tt.ct}
		if got := isTextContent(headers); got != tt.text {
			t.Errorf("isTextContent(%q) = %v, want %v", tt.ct, got, tt.text)
		}
	}
}

// --- OnReady Callback Test ---

func TestClientOnReadyCallback(t *testing.T) {
	db := testDB(t)
	readyURL := make(chan string, 1)

	wsURL, cleanup := startRelayStub(t, relayOpts{
		publicURL: "https://callback.sageclaw.io",
		subdomain: "cb",
	})
	defer cleanup()

	client, err := NewClient(db, Config{Mode: "managed", RelayURL: wsURL, LocalPort: 9999}, nil)
	if err != nil {
		t.Fatal(err)
	}

	client.OnReady(func(url string) {
		readyURL <- url
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client.Start(ctx)
	defer client.Stop()

	select {
	case url := <-readyURL:
		if url != "https://callback.sageclaw.io" {
			t.Errorf("OnReady URL: %q", url)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for OnReady callback")
	}
}

// --- Double Start/Stop Tests ---

func TestClientDoubleStart(t *testing.T) {
	db := testDB(t)

	wsURL, cleanup := startRelayStub(t, relayOpts{
		publicURL: "https://test.sageclaw.io",
		subdomain: "test",
	})
	defer cleanup()

	client, _ := NewClient(db, Config{Mode: "managed", RelayURL: wsURL, LocalPort: 9999}, nil)

	ctx := context.Background()
	client.Start(ctx)
	defer client.Stop()

	err := client.Start(ctx)
	if err == nil {
		t.Error("expected error on double start")
	}
}

func TestClientStopWhenNotRunning(t *testing.T) {
	db := testDB(t)

	client, _ := NewClient(db, Config{Mode: "managed", RelayURL: "ws://localhost:1", LocalPort: 9999}, nil)

	err := client.Stop()
	if err == nil {
		t.Error("expected error when stopping non-running client")
	}
}
