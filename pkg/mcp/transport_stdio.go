package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/xoai/sageclaw/pkg/security"
)

// stderrBuffer captures child process stderr (capped at 4KB).
type stderrBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (b *stderrBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.buf) < 4096 {
		remaining := 4096 - len(b.buf)
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.buf = append(b.buf, p...)
	}
	return len(p), nil
}

func (b *stderrBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}

// StdioTransport implements Transport over stdin/stdout of a child process.
type StdioTransport struct {
	command string
	args    []string
	env     map[string]string
	name    string

	cmd    *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	reader   *bufio.Scanner
	stderrBuf *stderrBuffer

	// StderrWriter receives a copy of stderr in real-time (optional).
	// Set before calling Connect. Used for streaming install progress.
	StderrWriter io.Writer

	mu      sync.Mutex
	nextID  atomic.Int64
	pending map[int64]chan JSONRPCResponse
	healthy atomic.Bool
}

// NewStdioTransport creates a stdio transport for a local MCP server process.
func NewStdioTransport(name, command string, args []string, env map[string]string) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		env:     env,
		name:    name,
		pending: make(map[int64]chan JSONRPCResponse),
	}
}

// dangerousEnvKeys are environment variable names that must not be set by users.
var dangerousEnvKeys = map[string]bool{
	"LD_PRELOAD":            true,
	"LD_LIBRARY_PATH":       true,
	"DYLD_INSERT_LIBRARIES": true,
	"BASH_ENV":              true,
	"ENV":                   true,
}

func (t *StdioTransport) Connect(ctx context.Context) error {
	// Validate command against security deny patterns.
	fullCmd := t.command + " " + strings.Join(t.args, " ")
	if err := security.CheckCommand(fullCmd, nil); err != nil {
		return fmt.Errorf("stdio %s: %w", t.name, err)
	}

	// Block dangerous environment variables.
	for k := range t.env {
		if dangerousEnvKeys[strings.ToUpper(k)] {
			return fmt.Errorf("stdio %s: env var %q is not allowed", t.name, k)
		}
	}

	t.cmd = exec.CommandContext(ctx, t.command, t.args...)

	// Inject environment variables — MERGE with parent env, don't replace.
	// Go's exec.Cmd.Env=nil inherits parent env. If we set any entries,
	// it replaces the entire env. So we must copy os.Environ() first.
	if len(t.env) > 0 {
		t.cmd.Env = os.Environ()
		for k, v := range t.env {
			t.cmd.Env = append(t.cmd.Env, k+"="+v)
		}
	}

	// Capture stderr so npx/node errors are visible in logs.
	t.stderrBuf = &stderrBuffer{}
	if t.StderrWriter != nil {
		t.cmd.Stderr = io.MultiWriter(t.stderrBuf, t.StderrWriter)
	} else {
		t.cmd.Stderr = t.stderrBuf
	}

	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdio %s: stdin pipe: %w", t.name, err)
	}

	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdio %s: stdout pipe: %w", t.name, err)
	}

	if err := t.cmd.Start(); err != nil {
		stderr := t.stderrBuf.String()
		if stderr != "" {
			return fmt.Errorf("stdio %s: start: %w\nstderr: %s", t.name, err, stderr)
		}
		return fmt.Errorf("stdio %s: start: %w", t.name, err)
	}

	t.reader = bufio.NewScanner(t.stdout)
	t.reader.Buffer(make([]byte, 1024*1024), 1024*1024)

	go t.readResponses()

	// MCP initialize handshake.
	initResp, err := t.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"clientInfo":      map[string]string{"name": "sageclaw", "version": "0.5.0"},
		"capabilities":    map[string]any{},
	})
	if err != nil {
		stderr := t.stderrBuf.String()
		if stderr != "" {
			return fmt.Errorf("stdio %s: initialize: %w\nstderr: %s", t.name, err, stderr)
		}
		return fmt.Errorf("stdio %s: initialize: %w", t.name, err)
	}

	log.Printf("mcp-client %s [stdio]: initialized (protocol: %v)", t.name, initResp.Result)

	// Send initialized notification.
	t.sendNotification(JSONRPCRequest{JSONRPC: "2.0", Method: "initialized"})

	t.healthy.Store(true)
	return nil
}

func (t *StdioTransport) Call(ctx context.Context, method string, params any) (*JSONRPCResponse, error) {
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

	respCh := make(chan JSONRPCResponse, 1)
	t.mu.Lock()
	t.pending[id] = respCh
	t.mu.Unlock()

	if err := t.send(req); err != nil {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, err
	}

	select {
	case resp := <-respCh:
		return &resp, nil
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (t *StdioTransport) Close() error {
	t.healthy.Store(false)
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
	}
	return nil
}

func (t *StdioTransport) Healthy() bool {
	return t.healthy.Load()
}

func (t *StdioTransport) send(req JSONRPCRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(t.stdin, "%s\n", data)
	return err
}

func (t *StdioTransport) sendNotification(req JSONRPCRequest) {
	t.send(req) //nolint: errcheck
}

func (t *StdioTransport) readResponses() {
	for t.reader.Scan() {
		var resp JSONRPCResponse
		if err := json.Unmarshal(t.reader.Bytes(), &resp); err != nil {
			continue
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
	t.healthy.Store(false)
}
