package agentcfg

import (
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher watches the agents directory for changes and triggers reloads.
type Watcher struct {
	baseDir  string
	onChange func(agentID string) // Called when an agent's files change.
	watcher  *fsnotify.Watcher
	done     chan struct{}
	mu       sync.Mutex

	// Debounce: multiple file changes in quick succession → one reload.
	pending map[string]time.Time
}

// NewWatcher creates a file watcher for the agents directory.
func NewWatcher(baseDir string, onChange func(agentID string)) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	w := &Watcher{
		baseDir:  baseDir,
		onChange: onChange,
		watcher:  fsw,
		done:     make(chan struct{}),
		pending:  make(map[string]time.Time),
	}

	// Watch the base directory for new agent folders.
	if err := fsw.Add(baseDir); err != nil {
		fsw.Close()
		return nil, err
	}

	// Watch each existing agent subdirectory.
	agents, _ := LoadAll(baseDir)
	for _, cfg := range agents {
		if cfg.Dir != "" {
			fsw.Add(cfg.Dir)
		}
	}

	return w, nil
}

// Start begins watching for file changes.
func (w *Watcher) Start() {
	go w.loop()
	go w.debounceLoop()
}

// Stop stops the watcher.
func (w *Watcher) Stop() {
	close(w.done)
	w.watcher.Close()
}

func (w *Watcher) loop() {
	for {
		select {
		case <-w.done:
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			// Determine which agent was affected.
			agentID := w.extractAgentID(event.Name)
			if agentID == "" {
				// Might be a new agent folder created.
				if event.Has(fsnotify.Create) {
					// Try to watch the new directory.
					w.watcher.Add(event.Name)
				}
				continue
			}

			// Debounce: mark this agent as needing reload.
			w.mu.Lock()
			w.pending[agentID] = time.Now()
			w.mu.Unlock()

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("agentcfg watcher error: %v", err)
		}
	}
}

func (w *Watcher) debounceLoop() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.mu.Lock()
			now := time.Now()
			for id, lastChange := range w.pending {
				// Wait 300ms after last change before triggering reload.
				if now.Sub(lastChange) > 300*time.Millisecond {
					delete(w.pending, id)
					go func(agentID string) {
						log.Printf("agentcfg: reloading %s (file changed)", agentID)
						w.onChange(agentID)
					}(id)
				}
			}
			w.mu.Unlock()
		}
	}
}

// extractAgentID determines which agent a file path belongs to.
// Path: /path/to/agents/myagent/soul.md → "myagent"
func (w *Watcher) extractAgentID(path string) string {
	rel, err := filepath.Rel(w.baseDir, path)
	if err != nil {
		return ""
	}

	// rel should be like "myagent/soul.md" or "myagent"
	parts := strings.SplitN(filepath.ToSlash(rel), "/", 2)
	if len(parts) == 0 {
		return ""
	}

	agentID := parts[0]
	if agentID == "." || agentID == ".." {
		return ""
	}

	return agentID
}
