package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const shellToolTimeout = 30 * time.Second

// ExecuteShellTool runs a shell script with JSON input on stdin.
func ExecuteShellTool(ctx context.Context, scriptPath string, input json.RawMessage) (*canonical.ToolResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, shellToolTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", scriptPath)
	cmd.Stdin = bytes.NewReader(input)

	output, err := cmd.CombinedOutput()
	result := string(output)

	if len(result) > 100_000 {
		result = result[:100_000] + "\n... [truncated]"
	}

	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return &canonical.ToolResult{Content: fmt.Sprintf("timeout after %v\n%s", shellToolTimeout, result), IsError: true}, nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("exit: %v\n%s", err, result), IsError: true}, nil
	}

	if result == "" {
		result = "(no output)"
	}

	return &canonical.ToolResult{Content: result}, nil
}

// MakeShellToolFunc creates a ToolFunc that executes a shell script.
func MakeShellToolFunc(scriptPath string) func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		return ExecuteShellTool(ctx, scriptPath, input)
	}
}
