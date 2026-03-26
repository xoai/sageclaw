package skillstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LockFile tracks installed skills with their sources and integrity hashes.
type LockFile struct {
	Version int                  `json:"version"`
	Skills  map[string]LockEntry `json:"skills"`

	path string // file path for Save
}

// LockEntry records metadata for a single installed skill.
type LockEntry struct {
	Source      string `json:"source"`      // "owner/repo"
	SourceType string `json:"sourceType"`  // "github" | "local"
	SkillID    string `json:"skillId"`
	InstalledAt string `json:"installedAt"`
	UpdatedAt   string `json:"updatedAt"`
	TreeSHA     string `json:"treeSHA"`
	HasScripts  bool   `json:"hasScripts"`
}

// LoadLockFile reads the lock file from disk. Returns an empty lock file
// if the file doesn't exist.
func LoadLockFile(path string) (*LockFile, error) {
	lf := &LockFile{
		Version: 2,
		Skills:  make(map[string]LockEntry),
		path:    path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return lf, nil
		}
		return nil, fmt.Errorf("read lock file: %w", err)
	}

	if err := json.Unmarshal(data, lf); err != nil {
		return nil, fmt.Errorf("parse lock file: %w", err)
	}
	lf.path = path
	if lf.Skills == nil {
		lf.Skills = make(map[string]LockEntry)
	}

	return lf, nil
}

// Save writes the lock file to disk atomically.
func (lf *LockFile) Save(path string) error {
	if path == "" {
		path = lf.path
	}

	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal lock file: %w", err)
	}

	// Atomic write: write to temp file, then rename.
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".skills-lock-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close temp file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename lock file: %w", err)
	}

	return nil
}

// Add records a newly installed skill.
func (lf *LockFile) Add(name string, entry LockEntry) {
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.InstalledAt == "" {
		entry.InstalledAt = now
	}
	if entry.UpdatedAt == "" {
		entry.UpdatedAt = now
	}
	lf.Skills[name] = entry
}

// Remove deletes a skill entry.
func (lf *LockFile) Remove(name string) {
	delete(lf.Skills, name)
}

// Get returns the lock entry for a skill, or nil if not found.
func (lf *LockFile) Get(name string) *LockEntry {
	entry, ok := lf.Skills[name]
	if !ok {
		return nil
	}
	return &entry
}

// NeedsUpdate returns true if the current SHA differs from the stored SHA.
func (lf *LockFile) NeedsUpdate(name, currentSHA string) bool {
	entry, ok := lf.Skills[name]
	if !ok {
		return false
	}
	return entry.TreeSHA != "" && currentSHA != "" && entry.TreeSHA != currentSHA
}

// Names returns all installed skill names.
func (lf *LockFile) Names() []string {
	names := make([]string, 0, len(lf.Skills))
	for name := range lf.Skills {
		names = append(names, name)
	}
	return names
}
