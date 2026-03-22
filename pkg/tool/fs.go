package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

// RegisterFS registers file system tools on the registry.
func RegisterFS(reg *Registry, sandbox *security.Sandbox) {
	reg.Register("read_file", "Read the contents of a file",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workspace"}},"required":["path"]}`),
		fsRead(sandbox))

	reg.Register("write_file", "Write content to a file",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workspace"},"content":{"type":"string","description":"Content to write"}},"required":["path","content"]}`),
		fsWrite(sandbox))

	reg.Register("list_directory", "List files and directories",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path relative to workspace"}},"required":["path"]}`),
		fsList(sandbox))
}

func fsRead(sandbox *security.Sandbox) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		resolved, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("access denied: " + err.Error()), nil
		}

		data, err := os.ReadFile(resolved)
		if err != nil {
			return errorResult("read failed: " + err.Error()), nil
		}

		// Truncate large files.
		content := string(data)
		if len(content) > 100_000 {
			content = content[:100_000] + "\n... [truncated at 100KB]"
		}

		return &canonical.ToolResult{Content: content}, nil
	}
}

func fsWrite(sandbox *security.Sandbox) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		resolved, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("access denied: " + err.Error()), nil
		}

		// Ensure parent directory exists.
		if err := os.MkdirAll(filepath.Dir(resolved), 0755); err != nil {
			return errorResult("mkdir failed: " + err.Error()), nil
		}

		if err := os.WriteFile(resolved, []byte(params.Content), 0644); err != nil {
			return errorResult("write failed: " + err.Error()), nil
		}

		return &canonical.ToolResult{Content: fmt.Sprintf("Wrote %d bytes to %s", len(params.Content), params.Path)}, nil
	}
}

func fsList(sandbox *security.Sandbox) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.Path == "" {
			params.Path = "."
		}

		resolved, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("access denied: " + err.Error()), nil
		}

		entries, err := os.ReadDir(resolved)
		if err != nil {
			return errorResult("list failed: " + err.Error()), nil
		}

		var lines []string
		for _, entry := range entries {
			suffix := ""
			if entry.IsDir() {
				suffix = "/"
			}
			info, _ := entry.Info()
			size := ""
			if info != nil && !entry.IsDir() {
				size = fmt.Sprintf(" (%d bytes)", info.Size())
			}
			lines = append(lines, entry.Name()+suffix+size)
		}

		if len(lines) == 0 {
			return &canonical.ToolResult{Content: "(empty directory)"}, nil
		}

		return &canonical.ToolResult{Content: strings.Join(lines, "\n")}, nil
	}
}

func errorResult(msg string) *canonical.ToolResult {
	return &canonical.ToolResult{Content: msg, IsError: true}
}
