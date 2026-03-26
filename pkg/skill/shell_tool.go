package skill

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const shellToolTimeout = 30 * time.Second

// sensitiveEnvPrefixes lists environment variable prefixes to strip
// from shell tool execution to prevent credential leakage.
var sensitiveEnvPrefixes = []string{
	"API_KEY", "SECRET", "TOKEN", "PASSWORD", "CREDENTIAL",
	"AWS_", "AZURE_", "GCP_", "ANTHROPIC_", "OPENAI_",
	"GITHUB_TOKEN", "GITLAB_TOKEN", "ENCRYPTION_KEY",
}

// ExecuteShellTool runs a shell script with JSON input on stdin.
// Scripts run with sanitized environment and restricted working directory.
func ExecuteShellTool(ctx context.Context, scriptPath string, input json.RawMessage) (*canonical.ToolResult, error) {
	execCtx, cancel := context.WithTimeout(ctx, shellToolTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", scriptPath)
	cmd.Stdin = bytes.NewReader(input)

	// Restrict working directory to the skill's own directory.
	cmd.Dir = filepath.Dir(scriptPath)

	// Sanitize environment — strip sensitive variables.
	cmd.Env = sanitizeEnv(os.Environ())

	output, err := cmd.CombinedOutput()
	result := string(output)

	// Limit output size to 1MB.
	const maxOutput = 1 << 20
	if len(result) > maxOutput {
		result = result[:maxOutput] + "\n... [truncated at 1MB]"
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

// sanitizeEnv removes sensitive environment variables from the list.
func sanitizeEnv(env []string) []string {
	var safe []string
	for _, e := range env {
		key := e
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key = e[:idx]
		}
		keyUpper := strings.ToUpper(key)
		sensitive := false
		for _, prefix := range sensitiveEnvPrefixes {
			if strings.HasPrefix(keyUpper, prefix) || strings.Contains(keyUpper, prefix) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			safe = append(safe, e)
		}
	}
	return safe
}
