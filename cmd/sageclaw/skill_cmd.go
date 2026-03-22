package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runSkillCommand(args []string) error {
	if len(args) == 0 {
		fmt.Println(`Usage: sageclaw skill <command>

Commands:
  install <git-url>   Install a skill from a git repository
  list                List installed skills
  remove <name>       Remove an installed skill`)
		return nil
	}

	skillsDir := envOrDefault("SAGECLAW_SKILLS_DIR", "skills")

	switch args[0] {
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw skill install <git-url>")
		}
		return skillInstall(skillsDir, args[1])
	case "list":
		return skillList(skillsDir)
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw skill remove <name>")
		}
		return skillRemove(skillsDir, args[1])
	default:
		return fmt.Errorf("unknown skill command: %s", args[0])
	}
}

func skillInstall(skillsDir, gitURL string) error {
	// Extract skill name from URL.
	name := gitURL
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	name = strings.TrimSuffix(name, ".git")

	destDir := filepath.Join(skillsDir, name)

	// Check if already exists.
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("skill %q already installed at %s. Remove it first: sageclaw skill remove %s", name, destDir, name)
	}

	// Ensure skills directory exists.
	os.MkdirAll(skillsDir, 0755)

	// Clone the repo.
	fmt.Printf("Installing skill %q from %s...\n", name, gitURL)
	cmd := exec.Command("git", "clone", "--depth", "1", gitURL, destDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %w", err)
	}

	// Verify SKILL.md exists.
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); err != nil {
		// Not a valid skill — clean up.
		os.RemoveAll(destDir)
		return fmt.Errorf("no SKILL.md found in repository. Not a valid SageClaw skill.")
	}

	// Remove .git directory (we don't need the history).
	os.RemoveAll(filepath.Join(destDir, ".git"))

	fmt.Printf("Skill %q installed to %s\n", name, destDir)
	fmt.Println("Restart SageClaw or send SIGHUP to load the new skill.")
	return nil
}

func skillList(skillsDir string) error {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No skills installed.")
			return nil
		}
		return err
	}

	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillMd := filepath.Join(skillsDir, entry.Name(), "SKILL.md")
		if _, err := os.Stat(skillMd); err == nil {
			// Check for bundled tools.
			toolCount := 0
			toolsDir := filepath.Join(skillsDir, entry.Name(), "tools")
			if toolEntries, err := os.ReadDir(toolsDir); err == nil {
				for _, te := range toolEntries {
					if strings.HasSuffix(te.Name(), ".yaml") {
						toolCount++
					}
				}
			}
			toolsLabel := ""
			if toolCount > 0 {
				toolsLabel = fmt.Sprintf(" (%d tools)", toolCount)
			}
			fmt.Printf("  %s%s\n", entry.Name(), toolsLabel)
			found = true
		}
	}

	if !found {
		fmt.Println("No skills installed.")
	}
	return nil
}

func skillRemove(skillsDir, name string) error {
	destDir := filepath.Join(skillsDir, name)

	if _, err := os.Stat(destDir); err != nil {
		return fmt.Errorf("skill %q not found", name)
	}

	fmt.Printf("Removing skill %q...\n", name)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("failed to remove: %w", err)
	}

	fmt.Printf("Skill %q removed.\n", name)
	fmt.Println("Restart SageClaw or send SIGHUP to unload the skill.")
	return nil
}
