package skillstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLockFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "skills-lock.json")

	// Load non-existent — should return empty.
	lf, err := LoadLockFile(path)
	if err != nil {
		t.Fatalf("LoadLockFile: %v", err)
	}
	if lf.Version != 2 {
		t.Errorf("version = %d, want 2", lf.Version)
	}
	if len(lf.Skills) != 0 {
		t.Errorf("expected empty skills, got %d", len(lf.Skills))
	}

	// Add a skill.
	lf.Add("test-skill", LockEntry{
		Source:     "owner/repo",
		SourceType: "github",
		SkillID:    "test-skill",
		TreeSHA:    "abc123",
	})

	if len(lf.Skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(lf.Skills))
	}

	// Save.
	if err := lf.Save(path); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file not created: %v", err)
	}

	// Reload.
	lf2, err := LoadLockFile(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(lf2.Skills) != 1 {
		t.Fatalf("expected 1 skill after reload, got %d", len(lf2.Skills))
	}

	entry := lf2.Get("test-skill")
	if entry == nil {
		t.Fatal("Get returned nil")
	}
	if entry.Source != "owner/repo" {
		t.Errorf("source = %q, want owner/repo", entry.Source)
	}
	if entry.TreeSHA != "abc123" {
		t.Errorf("treeSHA = %q, want abc123", entry.TreeSHA)
	}

	// NeedsUpdate.
	if lf2.NeedsUpdate("test-skill", "abc123") {
		t.Error("NeedsUpdate should be false for same SHA")
	}
	if !lf2.NeedsUpdate("test-skill", "def456") {
		t.Error("NeedsUpdate should be true for different SHA")
	}

	// Remove.
	lf2.Remove("test-skill")
	if len(lf2.Skills) != 0 {
		t.Errorf("expected 0 skills after remove, got %d", len(lf2.Skills))
	}
}
