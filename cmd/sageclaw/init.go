package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runInit(templateName, dir string) error {
	if dir == "" {
		dir = "."
	}

	// Create base directories.
	dirs := []string{"configs", "skills", "bin"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}

	// Copy template files if specified.
	if templateName != "" {
		// Look for templates in known locations.
		templateDir := ""
		for _, candidate := range []string{
			filepath.Join("templates", templateName),
			filepath.Join(dir, "templates", templateName),
		} {
			if info, err := os.Stat(candidate); err == nil && info.IsDir() {
				templateDir = candidate
				break
			}
		}

		if templateDir == "" {
			return fmt.Errorf("template %q not found. Ensure templates/%s/ exists", templateName, templateName)
		}

		entries, err := os.ReadDir(templateDir)
		if err != nil {
			return fmt.Errorf("reading template: %w", err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			data, err := os.ReadFile(filepath.Join(templateDir, entry.Name()))
			if err != nil {
				continue
			}

			destDir := dir
			if strings.HasSuffix(entry.Name(), ".yaml") {
				destDir = filepath.Join(dir, "configs")
			}

			dest := filepath.Join(destDir, entry.Name())
			if err := os.WriteFile(dest, data, 0644); err != nil {
				return fmt.Errorf("writing %s: %w", dest, err)
			}
			fmt.Printf("  created %s\n", dest)
		}
	} else {
		// Default single-agent config.
		defaultConfig := `agents:
  default:
    name: "SageClaw"
    tier: strong
    max_tokens: 8192
    system_prompt: |
      You are SageClaw, a personal AI agent.
    tools: [all]
    skills: [memory, self-learning]
`
		dest := filepath.Join(dir, "configs", "agents.yaml")
		os.WriteFile(dest, []byte(defaultConfig), 0644)
		fmt.Printf("  created %s\n", dest)
	}

	// Create .gitignore.
	gitignore := `bin/
*.exe
*.db
*.db-wal
*.db-shm
.env
`
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(gitignore), 0644)

	fmt.Println()
	fmt.Println("SageClaw project initialized!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Set your API key:  export ANTHROPIC_API_KEY=your-key")
	fmt.Println("  2. Run SageClaw:      sageclaw")
	fmt.Println()

	return nil
}
