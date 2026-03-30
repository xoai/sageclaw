package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

const (
	defaultExecTimeout = 30 * time.Second
	maxExecTimeout     = 300 * time.Second
	maxOutputBytes     = 512_000 // ~128K tokens — GoClaw uses 1MB; 512KB balances detail vs context pressure
)

// RegisterExec registers the shell execution tool.
// disabledDenyGroups controls which shell deny groups are skipped (nil = all enabled).
// Per-agent exec security mode is read from context at runtime (see WithExecConfig).
func RegisterExec(reg *Registry, workdir string, disabledDenyGroups ...map[string]bool) {
	var disabled map[string]bool
	if len(disabledDenyGroups) > 0 {
		disabled = disabledDenyGroups[0]
	}
	reg.RegisterWithGroup("execute_command", "Execute a shell command",
		json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"timeout_ms":{"type":"integer","description":"Timeout in milliseconds (default 30000, max 300000)"}},"required":["command"]}`),
		GroupRuntime, RiskSensitive, "builtin", execCommand(workdir, disabled))
}

func execCommand(workdir string, disabledDenyGroups map[string]bool) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Command   string `json:"command"`
			TimeoutMs int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Reject NUL bytes (shell truncation attack prevention).
		if strings.ContainsRune(params.Command, 0) {
			return errorResult("command rejected: contains NUL byte"), nil
		}

		// Check deny patterns.
		if err := security.CheckCommand(params.Command, disabledDenyGroups); err != nil {
			return errorResult(err.Error()), nil
		}

		// Per-command exec approval (from agent context).
		if result := checkExecApproval(ctx, params.Command); result != nil {
			return result, nil
		}

		// Calculate timeout.
		timeout := defaultExecTimeout
		if params.TimeoutMs > 0 {
			timeout = time.Duration(params.TimeoutMs) * time.Millisecond
			if timeout > maxExecTimeout {
				timeout = maxExecTimeout
			}
		}

		execCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(execCtx, "sh", "-c", params.Command)
		cmd.Dir = workdir

		output, err := cmd.CombinedOutput()
		result := string(output)

		// Truncate large output.
		if len(result) > maxOutputBytes {
			result = result[:maxOutputBytes] + fmt.Sprintf("\n... [truncated at %dKB]", maxOutputBytes/1000)
		}

		if err != nil {
			if execCtx.Err() == context.DeadlineExceeded {
				return errorResult(fmt.Sprintf("command timed out after %v\n%s", timeout, result)), nil
			}
			return &canonical.ToolResult{
				Content: fmt.Sprintf("exit status: %v\n%s", err, result),
				IsError: true,
			}, nil
		}

		if strings.TrimSpace(result) == "" {
			result = fmt.Sprintf("(command completed successfully, no output)\n$ %s", params.Command)
		}

		return &canonical.ToolResult{Content: result}, nil
	}
}

// checkExecApproval enforces per-command exec security based on the ExecConfig in context.
// Returns nil to proceed, or an error ToolResult to block.
// If no ExecConfig is in context, all commands are allowed (backward compatible).
func checkExecApproval(ctx context.Context, command string) *canonical.ToolResult {
	cfg := ExecConfigFromCtx(ctx)
	if cfg == nil {
		return nil // No exec config = permissive (backward compatible).
	}

	switch cfg.Mode {
	case ExecDeny:
		return errorResult("command execution is disabled for this agent (exec_security: deny)")

	case ExecAsk:
		allowlist := cfg.EffectiveAllowlist()
		if IsSafeCommand(command, allowlist) {
			return nil // Safe commands auto-approved even in ask mode.
		}
		if cfg.Approver == nil {
			return errorResult("command requires approval but no approval channel is available. " +
				"This agent's exec_security is set to 'ask' but the session doesn't support interactive approval.")
		}
		approved, err := cfg.Approver(ctx, command)
		if err != nil {
			return errorResult(fmt.Sprintf("exec approval error: %v", err))
		}
		if !approved {
			return errorResult(fmt.Sprintf("command denied by user: %s", command))
		}
		return nil

	case ExecSafeOnly:
		allowlist := cfg.EffectiveAllowlist()
		if IsSafeCommand(command, allowlist) {
			return nil
		}
		return errorResult(fmt.Sprintf(
			"command blocked (exec_security: safe-only). Only simple commands using safe binaries are allowed. "+
				"Blocked command: %s. To allow this, change exec_security to 'ask' in agent settings.",
			command))

	default:
		return nil // Unknown mode = permissive (shouldn't happen).
	}
}
