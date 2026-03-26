package audio

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStore_SaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	data := []byte("fake audio data")
	path, err := store.Save("session-123", "msg-456", data, "ogg")
	if err != nil {
		t.Fatalf("Save: %v", err)
	}

	expected := filepath.Join(dir, "session-123", "msg-456.ogg")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}

	loaded, err := store.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(loaded) != string(data) {
		t.Errorf("loaded data mismatch: got %q, want %q", loaded, data)
	}
}

func TestStore_SaveEmpty(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	_, err := store.Save("s", "m", nil, "ogg")
	if err != ErrEmptyInput {
		t.Errorf("expected ErrEmptyInput, got %v", err)
	}
}

func TestStore_Exists(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	data := []byte("audio")
	path, _ := store.Save("s", "m", data, "ogg")

	if !store.Exists(path) {
		t.Error("Exists returned false for saved file")
	}
	if store.Exists(filepath.Join(dir, "nonexistent.ogg")) {
		t.Error("Exists returned true for nonexistent file")
	}
}

func TestStore_Cleanup(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Save a file.
	path, _ := store.Save("s1", "old", []byte("old audio"), "ogg")

	// Backdate the file to 10 days ago.
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(path, oldTime, oldTime)

	// Save a recent file.
	store.Save("s2", "new", []byte("new audio"), "ogg")

	// Cleanup files older than 5 days.
	deleted, err := store.Cleanup(5)
	if err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	// Old file should be gone.
	if store.Exists(path) {
		t.Error("old file still exists after cleanup")
	}

	// New file should remain.
	newPath := filepath.Join(dir, "s2", "new.ogg")
	if !store.Exists(newPath) {
		t.Error("new file was incorrectly deleted")
	}
}

func TestStore_CleanupZeroDays(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	store.Save("s", "m", []byte("audio"), "ogg")

	deleted, err := store.Cleanup(0)
	if err != nil {
		t.Fatalf("Cleanup(0): %v", err)
	}
	if deleted != 0 {
		t.Errorf("Cleanup(0) deleted %d files, want 0", deleted)
	}
}

func TestStore_CleanupRemovesEmptyDirs(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)

	// Save and backdate.
	path, _ := store.Save("empty-session", "m", []byte("data"), "ogg")
	oldTime := time.Now().AddDate(0, 0, -10)
	os.Chtimes(path, oldTime, oldTime)

	store.Cleanup(5)

	// Session directory should be removed.
	sessionDir := filepath.Join(dir, "empty-session")
	if _, err := os.Stat(sessionDir); err == nil {
		t.Error("empty session directory still exists after cleanup")
	}
}

func TestStore_DefaultPath(t *testing.T) {
	store := NewStore("")
	if store.BasePath() != defaultStoragePath {
		t.Errorf("default path = %q, want %q", store.BasePath(), defaultStoragePath)
	}
}
