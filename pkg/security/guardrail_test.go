package security

import (
	"context"
	"encoding/json"
	"testing"
)

func TestShellDenyProvider_BlocksDestructive(t *testing.T) {
	p := NewShellDenyProvider(nil)
	input := json.RawMessage(`{"command":"rm -rf /"}`)
	allow, reason := p.EvaluateToolCall(context.Background(), "exec", input)
	if allow {
		t.Error("should deny destructive command")
	}
	if reason == "" {
		t.Error("should provide denial reason")
	}
}

func TestShellDenyProvider_AllowsSafe(t *testing.T) {
	p := NewShellDenyProvider(nil)
	input := json.RawMessage(`{"command":"ls -la"}`)
	allow, _ := p.EvaluateToolCall(context.Background(), "exec", input)
	if !allow {
		t.Error("should allow safe command")
	}
}

func TestShellDenyProvider_SkipsNonExec(t *testing.T) {
	p := NewShellDenyProvider(nil)
	input := json.RawMessage(`{"path":"/etc/passwd"}`)
	allow, _ := p.EvaluateToolCall(context.Background(), "read_file", input)
	if !allow {
		t.Error("should allow non-exec tools")
	}
}

func TestToolAllowlistProvider(t *testing.T) {
	p := NewToolAllowlistProvider([]string{"read_file", "web_search"})

	allow, _ := p.EvaluateToolCall(context.Background(), "read_file", nil)
	if !allow {
		t.Error("read_file should be allowed")
	}

	allow, reason := p.EvaluateToolCall(context.Background(), "exec", nil)
	if allow {
		t.Error("exec should be denied")
	}
	if reason == "" {
		t.Error("should provide reason")
	}
}

func TestGuardrailChain_AllMustAllow(t *testing.T) {
	chain := NewGuardrailChain(
		NewShellDenyProvider(nil),
		NewToolAllowlistProvider([]string{"exec", "read_file"}),
	)

	// Safe exec command + exec is in allowlist → allowed.
	input := json.RawMessage(`{"command":"echo hello"}`)
	allow, _ := chain.Evaluate(context.Background(), "exec", input)
	if !allow {
		t.Error("safe exec should be allowed by both providers")
	}

	// Destructive command → denied by ShellDenyProvider.
	input = json.RawMessage(`{"command":"rm -rf /"}`)
	allow, _ = chain.Evaluate(context.Background(), "exec", input)
	if allow {
		t.Error("destructive exec should be denied")
	}

	// Tool not in allowlist → denied by ToolAllowlistProvider.
	allow, _ = chain.Evaluate(context.Background(), "web_fetch", nil)
	if allow {
		t.Error("web_fetch should be denied by allowlist")
	}
}

func TestGuardrailChain_EmptyAllowsAll(t *testing.T) {
	chain := NewGuardrailChain()
	allow, _ := chain.Evaluate(context.Background(), "anything", nil)
	if !allow {
		t.Error("empty chain should allow all")
	}
}
