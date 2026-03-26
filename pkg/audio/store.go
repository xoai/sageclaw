package audio

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultStoragePath = "data/audio"

// Store manages audio file persistence.
type Store struct {
	basePath string
}

// NewStore creates a new audio file store.
// If basePath is empty, defaults to "data/audio".
func NewStore(basePath string) *Store {
	if basePath == "" {
		basePath = defaultStoragePath
	}
	return &Store{basePath: basePath}
}

// Save writes audio data to disk and returns the file path.
// Files are organized as: {basePath}/{sessionID}/{msgID}.{ext}
func (s *Store) Save(sessionID, msgID string, data []byte, ext string) (string, error) {
	if len(data) == 0 {
		return "", ErrEmptyInput
	}

	dir := filepath.Join(s.basePath, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("audio store: mkdir %s: %w", dir, err)
	}

	filePath := filepath.Join(dir, msgID+"."+ext)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return "", fmt.Errorf("audio store: write %s: %w", filePath, err)
	}

	return filePath, nil
}

// Load reads audio data from disk.
// The path must resolve to a location within the store's base directory.
func (s *Store) Load(filePath string) ([]byte, error) {
	// Defense-in-depth: ensure the resolved path is under basePath.
	absBase, _ := filepath.Abs(s.basePath)
	absFile, _ := filepath.Abs(filePath)
	if !strings.HasPrefix(absFile, absBase) {
		return nil, fmt.Errorf("audio store: path %s is outside base directory", filePath)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("audio store: read %s: %w", filePath, err)
	}
	return data, nil
}

// Cleanup deletes audio files older than maxAgeDays.
// If maxAgeDays is 0, no cleanup is performed (keep forever).
// Returns the number of files deleted.
func (s *Store) Cleanup(maxAgeDays int) (int, error) {
	if maxAgeDays <= 0 {
		return 0, nil
	}

	cutoff := time.Now().AddDate(0, 0, -maxAgeDays)
	deleted := 0

	err := filepath.Walk(s.basePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip inaccessible files.
		}
		if info.IsDir() {
			return nil
		}

		if info.ModTime().Before(cutoff) {
			if removeErr := os.Remove(path); removeErr == nil {
				deleted++
			}
		}
		return nil
	})

	// Clean up empty session directories.
	s.removeEmptyDirs()

	if err != nil {
		return deleted, fmt.Errorf("audio store: cleanup walk: %w", err)
	}
	return deleted, nil
}

// Exists returns true if the audio file exists on disk.
func (s *Store) Exists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// BasePath returns the store's base directory.
func (s *Store) BasePath() string {
	return s.basePath
}

// removeEmptyDirs removes empty session directories under basePath.
func (s *Store) removeEmptyDirs() {
	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dirPath := filepath.Join(s.basePath, entry.Name())
		subEntries, err := os.ReadDir(dirPath)
		if err == nil && len(subEntries) == 0 {
			os.Remove(dirPath)
		}
	}
}
