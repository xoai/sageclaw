package security

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// --- Sandbox tests ---

func newTestSandbox(t *testing.T) (*Sandbox, string) {
	t.Helper()
	dir := t.TempDir()
	sb, err := NewSandbox(dir)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	return sb, dir
}

func TestSandbox_ResolveRelative(t *testing.T) {
	sb, dir := newTestSandbox(t)
	got, err := sb.Resolve("subdir/file.txt")
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	expected := filepath.Join(dir, "subdir", "file.txt")
	if got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}

func TestSandbox_ResolveAbsoluteInside(t *testing.T) {
	sb, dir := newTestSandbox(t)
	inside := filepath.Join(dir, "inside.txt")
	got, err := sb.Resolve(inside)
	if err != nil {
		t.Fatalf("resolving: %v", err)
	}
	if got != inside {
		t.Fatalf("expected %s, got %s", inside, got)
	}
}

func TestSandbox_RejectTraversal(t *testing.T) {
	sb, _ := newTestSandbox(t)
	_, err := sb.Resolve("../../../etc/passwd")
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("expected ErrOutsideWorkspace, got: %v", err)
	}
}

func TestSandbox_RejectAbsoluteOutside(t *testing.T) {
	sb, dir := newTestSandbox(t)
	// Use an absolute path guaranteed to be outside the sandbox.
	// On Windows, /etc/passwd is relative, so we use a sibling of the temp dir.
	outside := filepath.Join(filepath.Dir(dir), "definitely-outside", "file.txt")
	_, err := sb.Resolve(outside)
	if !errors.Is(err, ErrOutsideWorkspace) {
		t.Fatalf("expected ErrOutsideWorkspace for %s, got: %v", outside, err)
	}
}

func TestSandbox_RejectSymlinkEscape(t *testing.T) {
	sb, dir := newTestSandbox(t)

	// Create a symlink inside workspace pointing outside.
	outside := t.TempDir()
	link := filepath.Join(dir, "escape")
	err := os.Symlink(outside, link)
	if err != nil {
		t.Skip("symlinks not supported on this filesystem")
	}

	_, err = sb.Resolve("escape/file.txt")
	if !errors.Is(err, ErrSymlinkEscape) {
		t.Fatalf("expected ErrSymlinkEscape, got: %v", err)
	}
}

func TestSandbox_Root(t *testing.T) {
	sb, dir := newTestSandbox(t)
	if sb.Root() != dir {
		t.Fatalf("expected root %s, got %s", dir, sb.Root())
	}
}

// --- Deny pattern tests ---

func TestCheckCommand_Denied(t *testing.T) {
	denied := []string{
		"rm -rf /",
		"rm -f /etc/passwd",
		"mkfs.ext4 /dev/sda1",
		"dd if=/dev/zero of=/dev/sda",
		":() { :|:& };:",
		"shutdown -h now",
		"reboot",
		"init 0",
		"chmod 777 /etc",
		"> /dev/sda",
		"mv /etc /tmp",
	}
	for _, cmd := range denied {
		if err := CheckCommand(cmd); !errors.Is(err, ErrDeniedCommand) {
			t.Errorf("expected deny for %q, got: %v", cmd, err)
		}
	}
}

func TestCheckCommand_Allowed(t *testing.T) {
	allowed := []string{
		"ls -la",
		"cat file.txt",
		"go build ./...",
		"npm install",
		"rm temp.txt",
		"echo hello",
		"git status",
		"python script.py",
		"curl https://example.com",
	}
	for _, cmd := range allowed {
		if err := CheckCommand(cmd); err != nil {
			t.Errorf("expected allow for %q, got: %v", cmd, err)
		}
	}
}

// --- Scrub tests ---

func TestScrub_APIKeys(t *testing.T) {
	tests := []struct {
		input    string
		contains string
	}{
		{"api_key=sk-1234567890abcdef1234567890", "[REDACTED]"},
		{"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJ0ZXN0IjoiZGF0YSJ9", "[REDACTED]"},
		{"token: ghp_ABCDEFghijklmnopqrstuvwxyz1234567890", "[REDACTED]"},
		{"AKIAIOSFODNN7EXAMPLE some text", "[REDACTED]"},
		{"sk-ant-api03-long-key-here-1234567890", "[REDACTED]"},
		{"password=mysecretpass123", "[REDACTED]"},
	}
	for _, tt := range tests {
		got := Scrub(tt.input)
		if got == tt.input {
			t.Errorf("expected scrubbing for %q, got unchanged", tt.input)
		}
	}
}

func TestScrub_SafeText(t *testing.T) {
	safe := []string{
		"hello world",
		"The function returns 42",
		"go build ./cmd/sageclaw",
		"SELECT * FROM users WHERE id = 1",
	}
	for _, text := range safe {
		got := Scrub(text)
		if got != text {
			t.Errorf("expected no change for %q, got %q", text, got)
		}
	}
}
