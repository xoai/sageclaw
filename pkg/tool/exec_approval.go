package tool

import (
	"context"
	"path/filepath"
	"strings"
)

// ExecSecurityMode controls how the exec tool handles command approval.
type ExecSecurityMode string

const (
	// ExecDeny blocks all command execution.
	ExecDeny ExecSecurityMode = "deny"
	// ExecSafeOnly auto-approves safe commands, blocks everything else.
	ExecSafeOnly ExecSecurityMode = "safe-only"
	// ExecAsk auto-approves safe commands, prompts the user for others.
	ExecAsk ExecSecurityMode = "ask"
)

// ParseExecSecurityMode converts a string to ExecSecurityMode. Defaults to safe-only.
func ParseExecSecurityMode(s string) ExecSecurityMode {
	switch ExecSecurityMode(s) {
	case ExecDeny, ExecSafeOnly, ExecAsk:
		return ExecSecurityMode(s)
	default:
		return ExecSafeOnly
	}
}

// DefaultSafeBinaries are commands auto-approved in safe-only and ask modes.
// Only simple invocations (no pipes, redirects, etc.) are allowed.
var DefaultSafeBinaries = map[string]bool{
	// Navigation & inspection
	"ls": true, "cat": true, "head": true, "tail": true,
	"echo": true, "pwd": true, "whoami": true, "date": true,
	"wc": true, "sort": true, "uniq": true, "grep": true,
	"find": true, "which": true, "file": true, "stat": true,
	"tree": true, "diff": true, "less": true, "more": true,
	"basename": true, "dirname": true, "realpath": true,
	"env": false, // Explicitly NOT safe (env dump)

	// Version control
	"git": true,

	// Build & dev tools
	"go": true, "node": true, "npm": true, "npx": true,
	"python": true, "python3": true, "pip": true, "pip3": true,
	"make": true, "cargo": true, "rustc": true,
	"tsc": true, "bun": true, "deno": true, "pnpm": true, "yarn": true,

	// Text processing (read-only)
	"awk": true, "sed": true, "cut": true, "tr": true,
	"jq": true, "yq": true, "xargs": true,
}

// shellMetachars are characters/sequences that make a command "non-simple."
// A non-simple command always requires approval regardless of binary.
var shellMetachars = []string{
	"|", ";", "&&", "||", "`", "$(", ">", "<", "(", ")",
	"\n", "\r",
}

// IsSimpleCommand returns true if the command has no shell metacharacters
// (pipes, redirects, subshells, command chaining, etc.).
func IsSimpleCommand(cmd string) bool {
	for _, meta := range shellMetachars {
		if strings.Contains(cmd, meta) {
			return false
		}
	}
	return true
}

// IsSafeCommand checks if a command uses a safe binary and has no shell metacharacters.
func IsSafeCommand(cmd string, allowlist map[string]bool) bool {
	if !IsSimpleCommand(cmd) {
		return false
	}
	fields := strings.Fields(strings.TrimSpace(cmd))
	if len(fields) == 0 {
		return false
	}
	binary := filepath.Base(fields[0])
	safe, listed := allowlist[binary]
	if !listed {
		return false
	}
	return safe // Explicitly false entries (like "env") are not safe.
}

// MergeAllowlists merges custom entries on top of the defaults.
// Custom entries override defaults (e.g., custom can set "rm": true to allow it).
func MergeAllowlists(base, custom map[string]bool) map[string]bool {
	merged := make(map[string]bool, len(base)+len(custom))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range custom {
		merged[k] = v
	}
	return merged
}

// ExecApprover is called when a command in "ask" mode needs user approval.
// Returns true if approved, false if denied. Error for system failures.
type ExecApprover func(ctx context.Context, command string) (approved bool, err error)

// ExecConfig holds per-agent exec security configuration.
// Passed via context so the shared tool registry can enforce per-agent policy.
type ExecConfig struct {
	Mode      ExecSecurityMode
	Allowlist map[string]bool // Merged allowlist (defaults + custom). nil = use DefaultSafeBinaries.
	Approver  ExecApprover    // For "ask" mode: callback to request user approval.
}

// EffectiveAllowlist returns the allowlist to use, defaulting to DefaultSafeBinaries.
func (c ExecConfig) EffectiveAllowlist() map[string]bool {
	if c.Allowlist != nil {
		return c.Allowlist
	}
	return DefaultSafeBinaries
}

// execConfigKey is the context key for per-agent exec security config.
type execConfigKey struct{}

// WithExecConfig returns a context with ExecConfig attached.
func WithExecConfig(ctx context.Context, cfg ExecConfig) context.Context {
	return context.WithValue(ctx, execConfigKey{}, &cfg)
}

// ExecConfigFromCtx retrieves the ExecConfig from context.
// Returns nil if not set (tool uses permissive default).
func ExecConfigFromCtx(ctx context.Context) *ExecConfig {
	cfg, _ := ctx.Value(execConfigKey{}).(*ExecConfig)
	return cfg
}
