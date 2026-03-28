package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/tunnel"
	"nhooyr.io/websocket"
)

// --- Helper: in-memory token store ---

type memTokenStore struct {
	tokens map[string]string // token → subdomain
}

func newMemTokenStore() *memTokenStore {
	return &memTokenStore{tokens: make(map[string]string)}
}

func (m *memTokenStore) Validate(token string) (string, bool) {
	sub, ok := m.tokens[token]
	return sub, ok
}

func (m *memTokenStore) Register(token, subdomain, instanceID string) error {
	m.tokens[token] = subdomain
	return nil
}

func (m *memTokenStore) Touch(token string) error { return nil }
func (m *memTokenStore) Cleanup() error           { return nil }

// --- Subdomain Allocation ---

func TestAllocateSubdomain(t *testing.T) {
	sub1 := allocateSubdomain("instance-abc-123")
	if len(sub1) != 8 {
		t.Errorf("subdomain length: %d, want 8", len(sub1))
	}

	// Same instance ID → same subdomain.
	sub2 := allocateSubdomain("instance-abc-123")
	if sub1 != sub2 {
		t.Errorf("not deterministic: %q vs %q", sub1, sub2)
	}

	// Different instance ID → different subdomain.
	sub3 := allocateSubdomain("instance-xyz-456")
	if sub1 == sub3 {
		t.Error("different instances should get different subdomains")
	}
}

// --- Subdomain Extraction ---

func TestExtractSubdomain(t *testing.T) {
	tests := []struct {
		host, domain, want string
	}{
		{"a1b2c3d4.sageclaw.io", "sageclaw.io", "a1b2c3d4"},
		{"a1b2c3d4.sageclaw.io:443", "sageclaw.io", "a1b2c3d4"},
		{"sageclaw.io", "sageclaw.io", ""},
		{"other.example.com", "sageclaw.io", ""},
		{"sub.deep.sageclaw.io", "sageclaw.io", ""}, // nested subdomain rejected
		{"", "sageclaw.io", ""},
	}
	for _, tt := range tests {
		got := extractSubdomain(tt.host, tt.domain)
		if got != tt.want {
			t.Errorf("extractSubdomain(%q, %q) = %q, want %q", tt.host, tt.domain, got, tt.want)
		}
	}
}

// --- Client IP Extraction ---

func TestExtractClientIP(t *testing.T) {
	r := &http.Request{RemoteAddr: "192.168.1.1:54321"}
	ip := extractClientIP(r)
	if ip != "192.168.1.1" {
		t.Errorf("got %q, want 192.168.1.1", ip)
	}
}

// --- Config Defaults ---

func TestConfigDefaults(t *testing.T) {
	cfg := Config{}.Defaults()
	if cfg.Listen != ":8080" {
		t.Errorf("listen: %q", cfg.Listen)
	}
	if cfg.Auth.Mode != "token" {
		t.Errorf("auth mode: %q", cfg.Auth.Mode)
	}
	if cfg.Limits.MaxTunnels != 100 {
		t.Errorf("max tunnels: %d", cfg.Limits.MaxTunnels)
	}
	if cfg.Limits.MaxBodySize != 10<<20 {
		t.Errorf("max body: %d", cfg.Limits.MaxBodySize)
	}
}

// --- Shared Secret Token Store ---

func TestSharedSecretTokenStore(t *testing.T) {
	store := NewSharedSecretTokenStore("my-secret")

	_, ok := store.Validate("my-secret")
	if !ok {
		t.Error("should validate correct secret")
	}

	_, ok = store.Validate("wrong-secret")
	if ok {
		t.Error("should reject wrong secret")
	}
}

// --- Health Endpoint ---

func TestHealthEndpoint(t *testing.T) {
	r := New(Config{Domain: "test.io"}.Defaults(), newMemTokenStore())
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["status"] != "ok" {
		t.Errorf("status: %v", result["status"])
	}
	if int(result["tunnels"].(float64)) != 0 {
		t.Errorf("tunnels: %v", result["tunnels"])
	}
}

// --- Tunnel Connect + Ready ---

func TestTunnelConnectAndReady(t *testing.T) {
	tokens := newMemTokenStore()
	r := New(Config{Domain: "test.io"}.Defaults(), tokens)
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/connect"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	headers := http.Header{
		"X-Tunnel-Token":     {"test-token-123"},
		"X-Instance-ID":      {"instance-abc"},
		"X-Protocol-Version": {"1"},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Read ready message.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	var msg tunnel.Message
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if msg.Type != tunnel.TypeReady {
		t.Errorf("type: %q, want %q", msg.Type, tunnel.TypeReady)
	}
	if msg.Subdomain == "" {
		t.Error("missing subdomain")
	}
	if !strings.Contains(msg.URL, ".test.io") {
		t.Errorf("URL missing domain: %q", msg.URL)
	}

	// Verify tunnel count.
	if r.TunnelCount() != 1 {
		t.Errorf("tunnel count: %d, want 1", r.TunnelCount())
	}
}

// --- Missing Auth ---

func TestTunnelConnectMissingAuth(t *testing.T) {
	r := New(Config{Domain: "test.io"}.Defaults(), newMemTokenStore())
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/connect"

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// No auth headers.
	_, resp, err := websocket.Dial(ctx, wsURL, nil)
	if err == nil {
		t.Fatal("expected error for missing auth")
	}
	if resp != nil && resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status: %d, want 401", resp.StatusCode)
	}
}

// --- Proxy to non-existent tunnel ---

func TestProxyNoTunnel(t *testing.T) {
	r := New(Config{Domain: "test.io"}.Defaults(), newMemTokenStore())
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	// Request with a subdomain Host header.
	req, _ := http.NewRequest("GET", srv.URL+"/api/status", nil)
	req.Host = "nonexistent.test.io"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status: %d, want 502", resp.StatusCode)
	}
}

// --- End-to-End: Client + Relay ---

func TestEndToEndRequestForwarding(t *testing.T) {
	tokens := newMemTokenStore()
	r := New(Config{Domain: "test.io"}.Defaults(), tokens)
	relaySrv := httptest.NewServer(r.Handler())
	defer relaySrv.Close()

	wsURL := "ws" + strings.TrimPrefix(relaySrv.URL, "http") + "/connect"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect as a tunnel client.
	headers := http.Header{
		"X-Tunnel-Token":     {"e2e-token"},
		"X-Instance-ID":      {"e2e-instance"},
		"X-Protocol-Version": {"1"},
	}

	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "done")

	// Read ready.
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read ready: %v", err)
	}
	var readyMsg tunnel.Message
	json.Unmarshal(data, &readyMsg)
	subdomain := readyMsg.Subdomain

	// Start a goroutine to handle requests from the relay.
	go func() {
		for {
			_, reqData, err := conn.Read(ctx)
			if err != nil {
				return
			}
			var reqMsg tunnel.Message
			if json.Unmarshal(reqData, &reqMsg) != nil || reqMsg.Type != tunnel.TypeRequest {
				continue
			}

			// Respond.
			respMsg := tunnel.Message{
				Type:    tunnel.TypeResponse,
				ID:      reqMsg.ID,
				Status:  200,
				Headers: map[string]string{"Content-Type": "application/json"},
				Body:    []byte(fmt.Sprintf(`{"forwarded": true, "path": %q}`, reqMsg.Path)),
			}
			data, _ := json.Marshal(respMsg)
			conn.Write(ctx, websocket.MessageText, data)
		}
	}()

	// Give the tunnel a moment to register.
	time.Sleep(100 * time.Millisecond)

	// Send a request through the relay proxy.
	req, _ := http.NewRequest("GET", relaySrv.URL+"/api/test", nil)
	req.Host = subdomain + ".test.io"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("status: %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"forwarded": true`) {
		t.Errorf("unexpected body: %s", body)
	}
	if !strings.Contains(string(body), `"/api/test"`) {
		t.Errorf("path not forwarded: %s", body)
	}
}

// --- Tunnel Limit ---

func TestTunnelLimit(t *testing.T) {
	cfg := Config{Domain: "test.io"}.Defaults()
	cfg.Limits.MaxTunnels = 1

	tokens := newMemTokenStore()
	r := New(cfg, tokens)
	srv := httptest.NewServer(r.Handler())
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/connect"

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First connection succeeds.
	headers1 := http.Header{
		"X-Tunnel-Token":     {"token-1"},
		"X-Instance-ID":      {"instance-1"},
		"X-Protocol-Version": {"1"},
	}
	conn1, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers1})
	if err != nil {
		t.Fatalf("first dial: %v", err)
	}
	defer conn1.Close(websocket.StatusNormalClosure, "done")

	// Read ready.
	conn1.Read(ctx)
	time.Sleep(50 * time.Millisecond)

	// Second connection with different instance should fail (at capacity).
	headers2 := http.Header{
		"X-Tunnel-Token":     {"token-2"},
		"X-Instance-ID":      {"instance-2"},
		"X-Protocol-Version": {"1"},
	}
	_, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: headers2})
	if err == nil {
		t.Error("expected error for tunnel limit")
	}
	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status: %d, want 503", resp.StatusCode)
	}
}
