package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSkills(t *testing.T) {
	dir := t.TempDir()

	// Create a skill directory with SKILL.md.
	memDir := filepath.Join(dir, "memory")
	os.MkdirAll(memDir, 0755)
	os.WriteFile(filepath.Join(memDir, "SKILL.md"), []byte(`---
name: memory
description: Knowledge capture and retrieval
version: "1.0.0"
tools: [memory_search, memory_get]
---

# Memory Skill

Use memory to persist knowledge.
`), 0644)

	// Create another skill.
	slDir := filepath.Join(dir, "self-learning")
	os.MkdirAll(slDir, 0755)
	os.WriteFile(filepath.Join(slDir, "SKILL.md"), []byte(`---
name: self-learning
description: Learn from mistakes
version: "1.0.0"
---

# Self-Learning Skill
`), 0644)

	// Create a directory without SKILL.md (should be skipped).
	os.MkdirAll(filepath.Join(dir, "empty"), 0755)

	skills, err := LoadSkills(dir)
	if err != nil {
		t.Fatalf("loading skills: %v", err)
	}

	if len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(skills))
	}

	// Find memory skill.
	var mem *Skill
	for i := range skills {
		if skills[i].Name == "memory" {
			mem = &skills[i]
			break
		}
	}
	if mem == nil {
		t.Fatal("memory skill not found")
	}
	if mem.Description != "Knowledge capture and retrieval" {
		t.Fatalf("wrong description: %s", mem.Description)
	}
	if mem.Version != "1.0.0" {
		t.Fatalf("wrong version: %s", mem.Version)
	}
	if len(mem.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(mem.Tools), mem.Tools)
	}
	if mem.Tools[0] != "memory_search" {
		t.Fatalf("expected memory_search, got %s", mem.Tools[0])
	}
}

func TestLoadSkills_NonexistentDir(t *testing.T) {
	skills, err := LoadSkills("/nonexistent/path")
	if err != nil {
		t.Fatalf("expected nil error for nonexistent dir, got: %v", err)
	}
	if len(skills) != 0 {
		t.Fatalf("expected empty result, got %d", len(skills))
	}
}

func TestParseSkillMd_NoFrontmatter(t *testing.T) {
	s := parseSkillMd("# Just a heading\n\nSome content.")
	if s.Name != "" {
		t.Fatalf("expected empty name, got %s", s.Name)
	}
	if s.Content == "" {
		t.Fatal("expected content to be preserved")
	}
}
