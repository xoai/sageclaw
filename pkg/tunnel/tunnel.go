// Package tunnel manages Cloudflare Tunnel (cloudflared) for exposing
// SageClaw's webhook endpoints to the internet.
//
// Users on personal PCs need this for WhatsApp and Zalo channels which
// require webhook URLs. Telegram and Discord don't need tunnels (they
// use long polling and Gateway WebSocket respectively).
package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Status represents the tunnel's current state.
type Status struct {
	Running   bool   `json:"running"`
	URL       string `json:"url"`       // e.g. https://abc123.trycloudflare.com
	LocalPort int    `json:"local_port"` // e.g. 9090
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	Mode      string `json:"mode"` // "quick" (trycloudflare) or "named" (user's tunnel)
}

// WebhookURLs returns the webhook paths for each channel.
func (s Status) WebhookURLs() map[string]string {
	if s.URL == "" {
		return nil
	}
	return map[string]string{
		"whatsapp": s.URL + "/webhook/whatsapp",
		"zalo":     s.URL + "/webhook/zalo",
	}
}

// Tunnel manages a cloudflared process.
type Tunnel struct {
	mu        sync.RWMutex
	cmd       *exec.Cmd
	cancel    context.CancelFunc
	status    Status
	localPort int
	onChange  func(Status) // callback when status changes
}

// New creates a new Tunnel manager.
func New(localPort int, onChange func(Status)) *Tunnel {
	return &Tunnel{
		localPort: localPort,
		onChange:  onChange,
	}
}

// Detect checks if cloudflared is installed and returns version info.
func Detect() (installed bool, version string, path string) {
	// Check common locations.
	names := []string{"cloudflared"}
	if runtime.GOOS == "windows" {
		names = append(names, "cloudflared.exe")
	}

	for _, name := range names {
		p, err := exec.LookPath(name)
		if err != nil {
			continue
		}

		// Get version.
		out, err := exec.Command(p, "version").CombinedOutput()
		if err != nil {
			continue
		}

		ver := strings.TrimSpace(string(out))
		// Extract just the version number.
		if idx := strings.Index(ver, "cloudflared version"); idx >= 0 {
			ver = strings.TrimSpace(ver[idx+len("cloudflared version"):])
			if sp := strings.IndexByte(ver, ' '); sp > 0 {
				ver = ver[:sp]
			}
		}

		return true, ver, p
	}

	return false, "", ""
}

// InstallHint returns platform-specific install instructions.
func InstallHint() string {
	switch runtime.GOOS {
	case "darwin":
		return "brew install cloudflared"
	case "linux":
		return "curl -L https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 -o /usr/local/bin/cloudflared && chmod +x /usr/local/bin/cloudflared"
	case "windows":
		return "winget install Cloudflare.cloudflared  (or download from https://github.com/cloudflare/cloudflared/releases)"
	default:
		return "See https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/downloads/"
	}
}

// GetStatus returns the current tunnel status.
func (t *Tunnel) GetStatus() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

// StartQuick starts a quick tunnel using trycloudflare.com (no account needed).
// This gives a random URL like https://abc-xyz-123.trycloudflare.com
func (t *Tunnel) StartQuick(ctx context.Context) error {
	t.mu.Lock()
	if t.status.Running {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already running at %s", t.status.URL)
	}
	t.mu.Unlock()

	installed, _, cfPath := Detect()
	if !installed {
		return fmt.Errorf("cloudflared not found. Install: %s", InstallHint())
	}

	tunnelCtx, cancel := context.WithCancel(ctx)

	// Quick tunnel: cloudflared tunnel --url http://localhost:PORT
	cmd := exec.CommandContext(tunnelCtx, cfPath, "tunnel", "--url",
		fmt.Sprintf("http://localhost:%d", t.localPort))

	// Capture stderr for the URL.
	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("creating pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting cloudflared: %w", err)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.cancel = cancel
	t.status = Status{
		Running:   true,
		LocalPort: t.localPort,
		Mode:      "quick",
		StartedAt: time.Now().Format(time.RFC3339),
	}
	t.mu.Unlock()
	t.notify()

	// Parse URL from cloudflared output (it prints to stderr).
	go func() {
		urlRe := regexp.MustCompile(`https://[a-z0-9-]+\.trycloudflare\.com`)
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			log.Printf("tunnel: %s", line)

			if match := urlRe.FindString(line); match != "" {
				t.mu.Lock()
				t.status.URL = match
				t.mu.Unlock()
				t.notify()
				log.Printf("tunnel: public URL = %s", match)
			}
		}
	}()

	// Monitor process exit.
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		t.status.Running = false
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			t.status.Error = err.Error()
		}
		t.mu.Unlock()
		t.notify()
		log.Printf("tunnel: cloudflared exited")
	}()

	return nil
}

// StartNamed starts a named tunnel (requires cloudflared login + tunnel create).
func (t *Tunnel) StartNamed(ctx context.Context, tunnelName string) error {
	t.mu.Lock()
	if t.status.Running {
		t.mu.Unlock()
		return fmt.Errorf("tunnel already running")
	}
	t.mu.Unlock()

	installed, _, cfPath := Detect()
	if !installed {
		return fmt.Errorf("cloudflared not found. Install: %s", InstallHint())
	}

	tunnelCtx, cancel := context.WithCancel(ctx)

	cmd := exec.CommandContext(tunnelCtx, cfPath, "tunnel", "run", tunnelName)

	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting cloudflared: %w", err)
	}

	t.mu.Lock()
	t.cmd = cmd
	t.cancel = cancel
	t.status = Status{
		Running:   true,
		LocalPort: t.localPort,
		Mode:      "named",
		StartedAt: time.Now().Format(time.RFC3339),
	}
	t.mu.Unlock()
	t.notify()

	// Log output.
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("tunnel: %s", scanner.Text())
		}
	}()

	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		t.status.Running = false
		if err != nil {
			t.status.Error = err.Error()
		}
		t.mu.Unlock()
		t.notify()
	}()

	return nil
}

// Stop gracefully stops the tunnel.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.status.Running {
		return fmt.Errorf("tunnel not running")
	}

	if t.cancel != nil {
		t.cancel()
	}

	t.status.Running = false
	t.status.URL = ""
	t.status.Error = ""
	t.notify()

	return nil
}

func (t *Tunnel) notify() {
	if t.onChange != nil {
		t.onChange(t.status)
	}
}
