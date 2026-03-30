package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

// RegisterEdit registers the search-and-replace file edit tool.
func RegisterEdit(reg *Registry, sandbox *security.Sandbox) {
	reg.RegisterWithGroup("edit", "Edit a file using search-and-replace",
		json.RawMessage(`{"type":"object","properties":{`+
			`"path":{"type":"string","description":"File path relative to workspace"},`+
			`"old_string":{"type":"string","description":"Text to find in the file"},`+
			`"new_string":{"type":"string","description":"Replacement text"},`+
			`"replace_all":{"type":"boolean","description":"Replace all occurrences (default: false)"}`+
			`},"required":["path","old_string","new_string"]}`),
		GroupFS, RiskModerate, "builtin", editFile(sandbox))
}

func editFile(sandbox *security.Sandbox) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Path       string `json:"path"`
			OldString  string `json:"old_string"`
			NewString  string `json:"new_string"`
			ReplaceAll bool   `json:"replace_all"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		if params.OldString == "" {
			return errorResult("old_string cannot be empty"), nil
		}
		if params.OldString == params.NewString {
			return errorResult("old_string and new_string are identical — no change needed"), nil
		}

		resolved, err := sandbox.Resolve(params.Path)
		if err != nil {
			return errorResult("access denied: " + err.Error()), nil
		}

		data, err := os.ReadFile(resolved)
		if err != nil {
			return errorResult("read failed: " + err.Error()), nil
		}

		content := string(data)
		count := strings.Count(content, params.OldString)

		if count == 0 {
			// Show a snippet to help the agent understand the file content.
			snippet := content
			if len(snippet) > 500 {
				snippet = snippet[:500] + "..."
			}
			return errorResult(fmt.Sprintf(
				"old_string not found in %s. File starts with:\n%s", params.Path, snippet)), nil
		}

		if count > 1 && !params.ReplaceAll {
			return errorResult(fmt.Sprintf(
				"old_string found %d times in %s. Use replace_all=true to replace all, or provide a more specific old_string.",
				count, params.Path)), nil
		}

		// Perform replacement.
		var newContent string
		if params.ReplaceAll {
			newContent = strings.ReplaceAll(content, params.OldString, params.NewString)
		} else {
			newContent = strings.Replace(content, params.OldString, params.NewString, 1)
		}

		if err := os.WriteFile(resolved, []byte(newContent), 0644); err != nil {
			return errorResult("write failed: " + err.Error()), nil
		}

		// Find line numbers of changes for the confirmation message.
		var changedLines []int
		lines := strings.Split(newContent, "\n")
		newLines := strings.Split(params.NewString, "\n")
		for i, line := range lines {
			for _, nl := range newLines {
				if strings.Contains(line, nl) && nl != "" {
					changedLines = append(changedLines, i+1)
					break
				}
			}
		}

		msg := fmt.Sprintf("Edited %s: replaced %d occurrence(s)", params.Path, count)
		if len(changedLines) > 0 && len(changedLines) <= 10 {
			lineStrs := make([]string, len(changedLines))
			for i, l := range changedLines {
				lineStrs[i] = fmt.Sprintf("%d", l)
			}
			msg += fmt.Sprintf(" at line(s) %s", strings.Join(lineStrs, ", "))
		}

		return &canonical.ToolResult{Content: msg}, nil
	}
}
