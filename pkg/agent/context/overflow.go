package context

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// OverflowPlaceholder is returned when a persisted file has been evicted or is missing.
const OverflowPlaceholder = "[Overflow evicted — result no longer available]"

// OverflowManager persists oversized tool results to disk so they can be
// removed from the in-memory context window. File layout:
//
//	{baseDir}/{sessionID}/{toolCallID}.txt
type OverflowManager struct {
	baseDir string

	mu      sync.Mutex            // protects sessions map
	sessions map[string]*sync.Mutex // per-session mutexes
}

// NewOverflowManager creates an OverflowManager rooted at baseDir.
// The directory is created lazily on first Persist call.
func NewOverflowManager(baseDir string) *OverflowManager {
	return &OverflowManager{
		baseDir:  baseDir,
		sessions: make(map[string]*sync.Mutex),
	}
}

// sessionMu returns the per-session mutex, creating it if needed.
func (om *OverflowManager) sessionMu(sessionID string) *sync.Mutex {
	om.mu.Lock()
	defer om.mu.Unlock()
	m, ok := om.sessions[sessionID]
	if !ok {
		m = &sync.Mutex{}
		om.sessions[sessionID] = m
	}
	return m
}

// sessionDir returns the directory path for a session's overflow files.
func (om *OverflowManager) sessionDir(sessionID string) string {
	return filepath.Join(om.baseDir, sessionID)
}

// Persist writes content to disk and returns the file path.
// The session directory is created if it doesn't exist.
func (om *OverflowManager) Persist(sessionID, toolCallID, content string) (string, error) {
	mu := om.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()

	dir := om.sessionDir(sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("overflow: mkdir %s: %w", dir, err)
	}

	path := filepath.Join(dir, toolCallID+".txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("overflow: write %s: %w", path, err)
	}
	return path, nil
}

// Read retrieves persisted content. Returns OverflowPlaceholder if the file
// is missing (e.g. after quota eviction).
func (om *OverflowManager) Read(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return OverflowPlaceholder, nil
		}
		return "", fmt.Errorf("overflow: read %s: %w", path, err)
	}
	return string(data), nil
}

// CleanSession removes all overflow files for a session.
func (om *OverflowManager) CleanSession(sessionID string) error {
	mu := om.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()

	dir := om.sessionDir(sessionID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("overflow: clean %s: %w", dir, err)
	}

	// Remove the session mutex entry.
	om.mu.Lock()
	delete(om.sessions, sessionID)
	om.mu.Unlock()

	return nil
}

// fileInfo holds metadata for quota eviction sorting.
type fileInfo struct {
	path    string
	size    int64
	modTime int64 // Unix nanoseconds
}

// EnforceQuota evicts oldest overflow files for a session until total size
// is under maxBytes. When a file is evicted, any in-history message whose
// OverflowPath annotation matches the evicted path gets its content replaced
// with OverflowPlaceholder.
//
// Pass history to update annotations on eviction. May be nil if no
// annotation update is needed.
func (om *OverflowManager) EnforceQuota(sessionID string, maxBytes int64, history []canonical.Message) error {
	mu := om.sessionMu(sessionID)
	mu.Lock()
	defer mu.Unlock()

	dir := om.sessionDir(sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to evict
		}
		return fmt.Errorf("overflow: list %s: %w", dir, err)
	}

	// Gather file info and total size.
	var files []fileInfo
	var totalSize int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fi := fileInfo{
			path:    filepath.Join(dir, e.Name()),
			size:    info.Size(),
			modTime: info.ModTime().UnixNano(),
		}
		files = append(files, fi)
		totalSize += fi.size
	}

	if totalSize <= maxBytes {
		return nil
	}

	// Sort oldest first for eviction.
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime < files[j].modTime
	})

	// Build a set of evicted paths for history annotation update.
	evicted := make(map[string]bool)

	for _, f := range files {
		if totalSize <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil && !os.IsNotExist(err) {
			continue // skip files we can't remove
		}
		totalSize -= f.size
		evicted[f.path] = true
	}

	// Update history annotations for evicted files.
	if history != nil {
		updateEvictedAnnotations(history, evicted)
	}

	return nil
}

// updateEvictedAnnotations replaces content of messages whose OverflowPath
// was evicted with the placeholder text.
func updateEvictedAnnotations(history []canonical.Message, evicted map[string]bool) {
	for i := range history {
		ann := history[i].Annotations
		if ann == nil || ann.OverflowPath == "" {
			continue
		}
		if evicted[ann.OverflowPath] {
			// Replace all tool_result content with placeholder.
			for j := range history[i].Content {
				if history[i].Content[j].ToolResult != nil {
					history[i].Content[j].ToolResult.Content = OverflowPlaceholder
				}
			}
		}
	}
}
