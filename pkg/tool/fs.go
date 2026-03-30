package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

// RegisterFS registers file system tools on the registry.
func RegisterFS(reg *Registry, sandbox *security.Sandbox) {
	reg.RegisterWithGroup("read_file", "Read the contents of a file",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workspace"}},"required":["path"]}`),
		GroupFS, RiskModerate, "builtin", fsRead(sandbox))

	reg.RegisterWithGroup("write_file", "Write content to a file",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path relative to workspace"},"content":{"type":"string","description":"Content to write"}},"required":["path","content"]}`),
		GroupFS, RiskModerate, "builtin", fsWrite(sandbox))

	reg.RegisterWithGroup("list_directory", "List files and directories",
		json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Directory path relative to workspace"}},"required":["path"]}`),
		GroupFS, RiskModerate, "builtin", fsList(sandbox))
}

// binaryFileExts lists extensions that are binary and shouldn't be read as text.
var binaryFileExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true, ".svg": true, ".tiff": true, ".tif": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".rar": true, ".zst": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true,
	".ppt": true, ".pptx": true, ".odt": true, ".ods": true, ".odp": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".bin": true,
	".o": true, ".a": true, ".lib": true,
	".class": true, ".jar": true, ".war": true,
	".wasm": true, ".pyc": true, ".pyo": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mkv": true, ".mov": true,
	".wav": true, ".flac": true, ".ogg": true, ".webm": true, ".m4a": true,
	".ttf": true, ".otf": true, ".woff": true, ".woff2": true, ".eot": true,
	".sqlite": true, ".db": true, ".mdb": true,
}

// isBinaryFileExt returns true if the file extension indicates a binary file.
func isBinaryFileExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return binaryFileExts[ext]
}

// BinaryFileExtensions returns a sorted list of known binary extensions (for testing).
func BinaryFileExtensions() []string {
	exts := make([]string, 0, len(binaryFileExts))
	for ext := range binaryFileExts {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

func fsRead(sandbox *security.Sandbox) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Check for binary file extension before reading.
		if isBinaryFileExt(params.Path) {
			ext := strings.ToLower(filepath.Ext(params.Path))
			hint := "Use execute_command to inspect binary files."
			if ext == ".pdf" || ext == ".doc" || ext == ".docx" {
				hint = "Use read_document for PDFs and documents."
			}
			return errorResult(fmt.Sprintf(
				"Binary file detected (%s). %s", ext, hint)), nil
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
		if len(content) > maxOutputBytes {
			content = content[:maxOutputBytes] + fmt.Sprintf("\n... [truncated at %dKB]", maxOutputBytes/1000)
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
