package skill

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Skill represents a loaded skill definition.
type Skill struct {
	Name         string
	Description  string
	Version      string
	Tools        []string // Tool names referenced in SKILL.md frontmatter.
	BundledTools []BundledTool
	Content      string // Full SKILL.md content.
	Path         string // Directory path.
}

// BundledTool is a shell-based tool bundled with a skill.
type BundledTool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	ScriptPath  string
}

// LoadSkills scans a directory for skills (directories containing SKILL.md).
func LoadSkills(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading skills directory: %w", err)
	}

	var skills []Skill
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillPath := filepath.Join(dir, entry.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue // No SKILL.md in this directory.
		}

		skill := parseSkillMd(string(data))
		skill.Path = filepath.Join(dir, entry.Name())
		if skill.Name == "" {
			skill.Name = entry.Name()
		}

		// Scan for bundled tools.
		skill.BundledTools = loadBundledTools(skill.Path)

		skills = append(skills, skill)
	}

	return skills, nil
}

// loadBundledTools scans a skill's tools/ subdirectory for YAML+SH tool pairs.
func loadBundledTools(skillDir string) []BundledTool {
	toolsDir := filepath.Join(skillDir, "tools")
	entries, err := os.ReadDir(toolsDir)
	if err != nil {
		return nil
	}

	var tools []BundledTool
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		baseName := strings.TrimSuffix(entry.Name(), ".yaml")
		scriptPath := filepath.Join(toolsDir, baseName+".sh")
		if _, err := os.Stat(scriptPath); err != nil {
			continue // No matching script.
		}

		// Parse the YAML schema.
		schemaData, err := os.ReadFile(filepath.Join(toolsDir, entry.Name()))
		if err != nil {
			continue
		}

		bt := parseBundledToolYAML(string(schemaData), scriptPath)
		if bt.Name == "" {
			bt.Name = baseName
		}
		tools = append(tools, bt)
	}

	return tools
}

// parseBundledToolYAML extracts tool metadata from a simple YAML file.
// Supports: name, description, input_schema (as JSON pass-through).
func parseBundledToolYAML(content, scriptPath string) BundledTool {
	bt := BundledTool{ScriptPath: scriptPath}

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			bt.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			bt.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}

	// For simplicity, use a minimal schema. Full YAML→JSON would need a dep.
	bt.Schema = json.RawMessage(`{"type":"object","properties":{"input":{"type":"string"}}}`)
	return bt
}

// parseSkillMd extracts metadata from a SKILL.md file's YAML frontmatter.
func parseSkillMd(content string) Skill {
	s := Skill{Content: content}

	// Extract frontmatter between --- markers.
	if !strings.HasPrefix(content, "---") {
		return s
	}

	end := strings.Index(content[3:], "---")
	if end == -1 {
		return s
	}

	frontmatter := content[3 : end+3]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			s.Name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			s.Description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		} else if strings.HasPrefix(line, "version:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "version:"))
			s.Version = strings.Trim(v, `"'`)
		} else if strings.HasPrefix(line, "tools:") {
			toolsStr := strings.TrimSpace(strings.TrimPrefix(line, "tools:"))
			toolsStr = strings.Trim(toolsStr, "[]")
			for _, t := range strings.Split(toolsStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					s.Tools = append(s.Tools, t)
				}
			}
		}
	}

	return s
}
