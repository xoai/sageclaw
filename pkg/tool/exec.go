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
	maxOutputBytes     = 100_000
)

// RegisterExec registers the shell execution tool.
func RegisterExec(reg *Registry, workdir string) {
	reg.Register("execute_command", "Execute a shell command",
		json.RawMessage(`{"type":"object","properties":{"command":{"type":"string","description":"Shell command to execute"},"timeout_ms":{"type":"integer","description":"Timeout in milliseconds (default 30000, max 300000)"}},"required":["command"]}`),
		execCommand(workdir))
}

func execCommand(workdir string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Command   string `json:"command"`
			TimeoutMs int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Check deny patterns.
		if err := security.CheckCommand(params.Command); err != nil {
			return errorResult(err.Error()), nil
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
			result = result[:maxOutputBytes] + "\n... [truncated at 100KB]"
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
			result = "(no output)"
		}

		return &canonical.ToolResult{Content: result}, nil
	}
}
