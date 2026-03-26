package skillstore

import (
	"context"
	"log"
	"sync"
	"time"
)

// Updater periodically checks installed skills for available updates.
type Updater struct {
	store    *Store
	interval time.Duration

	mu      sync.RWMutex
	updates map[string]UpdateInfo // keyed by skill name

	cancel context.CancelFunc
}

// NewUpdater creates a background update checker.
// interval controls how often to check (default: 24h).
func NewUpdater(store *Store, interval time.Duration) *Updater {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	return &Updater{
		store:    store,
		interval: interval,
		updates:  make(map[string]UpdateInfo),
	}
}

// Start begins periodic update checking in the background.
func (u *Updater) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	u.cancel = cancel

	go func() {
		// Initial check after a short delay (don't block startup).
		select {
		case <-time.After(5 * time.Minute):
		case <-ctx.Done():
			return
		}

		u.check(ctx)

		ticker := time.NewTicker(u.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				u.check(ctx)
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates the background checker.
func (u *Updater) Stop() {
	if u.cancel != nil {
		u.cancel()
	}
}

// Available returns the current list of available updates.
func (u *Updater) Available() []UpdateInfo {
	u.mu.RLock()
	defer u.mu.RUnlock()

	result := make([]UpdateInfo, 0, len(u.updates))
	for _, info := range u.updates {
		result = append(result, info)
	}
	return result
}

// HasUpdate returns true if a specific skill has an available update.
func (u *Updater) HasUpdate(name string) bool {
	u.mu.RLock()
	defer u.mu.RUnlock()
	_, ok := u.updates[name]
	return ok
}

// ClearUpdate removes a skill from the updates list (after updating).
func (u *Updater) ClearUpdate(name string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	delete(u.updates, name)
}

func (u *Updater) check(ctx context.Context) {
	updates, err := u.store.CheckUpdates(ctx)
	if err != nil {
		log.Printf("skillstore: update check failed: %v", err)
		return
	}

	u.mu.Lock()
	u.updates = make(map[string]UpdateInfo)
	for _, info := range updates {
		u.updates[info.Name] = info
	}
	u.mu.Unlock()

	if len(updates) > 0 {
		log.Printf("skillstore: %d skill update(s) available", len(updates))
	}
}
