package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// RegisterSkillLoader registers the load_skill tool for on-demand skill loading.
// Skills listed in the system prompt manifest can be fully loaded at runtime
// instead of eagerly injected into the system prompt.
func RegisterSkillLoader(reg *Registry, skillsDir string) {
	reg.RegisterWithGroup("load_skill", "Load the full content of a skill by name. "+
		"Use this when you need a skill's detailed instructions. "+
		"The skill manifest in your system prompt lists available skills with descriptions.",
		json.RawMessage(`{
			"type": "object",
			"properties": {
				"name": {
					"type": "string",
					"description": "The skill name to load (e.g. 'jtbd', 'usability-testing')."
				}
			},
			"required": ["name"]
		}`),
		GroupCore, RiskSafe, "",
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			var params struct {
				Name string `json:"name"`
			}
			if err := json.Unmarshal(input, &params); err != nil {
				return &canonical.ToolResult{Content: "Invalid input: " + err.Error(), IsError: true}, nil
			}

			name := params.Name
			if name == "" {
				return &canonical.ToolResult{Content: "Skill name is required.", IsError: true}, nil
			}

			// Sanitize name to prevent path traversal.
			name = filepath.Base(name)
			if strings.Contains(name, "..") {
				return &canonical.ToolResult{Content: "Invalid skill name.", IsError: true}, nil
			}

			skillPath := filepath.Join(skillsDir, name, "SKILL.md")
			data, err := os.ReadFile(skillPath)
			if err != nil {
				return &canonical.ToolResult{
					Content: fmt.Sprintf("Skill %q not found. Check the skill manifest for available names.", name),
					IsError: true,
				}, nil
			}

			content := string(data)
			// Cap at 512KB to match read_file limit.
			if len(content) > maxOutputBytes {
				content = content[:maxOutputBytes] + "\n... [truncated]"
			}

			return &canonical.ToolResult{
				Content: fmt.Sprintf("## Skill: %s\n\n%s", name, content),
			}, nil
		},
	)
}
