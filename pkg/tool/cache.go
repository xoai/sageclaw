package tool

import (
	"sync"
	"time"
)

// ToolCache provides per-channel, TTL-based caching for tool results.
// Used by web_fetch (15min TTL) and web_search (60min TTL).
type ToolCache struct {
	mu         sync.RWMutex
	entries    map[string]*cacheEntry
	ttl        time.Duration
	maxEntries int
}

type cacheEntry struct {
	value     string
	channel   string
	createdAt time.Time
	accessedAt time.Time
}

// NewToolCache creates a cache with the given TTL and max entries.
// When maxEntries is reached, the least-recently-accessed entry is evicted.
func NewToolCache(ttl time.Duration, maxEntries int) *ToolCache {
	return &ToolCache{
		entries:    make(map[string]*cacheEntry),
		ttl:        ttl,
		maxEntries: maxEntries,
	}
}

// cacheKey builds a composite key from channel + tool-specific key.
func cacheKey(channel, key string) string {
	return channel + "\x00" + key
}

// Get returns a cached value if it exists and is not expired.
// Returns ("", false) on miss. Channel isolation prevents cross-channel poisoning.
func (c *ToolCache) Get(channel, key string) (string, bool) {
	c.mu.RLock()
	e, ok := c.entries[cacheKey(channel, key)]
	c.mu.RUnlock()

	if !ok {
		return "", false
	}
	if time.Since(e.createdAt) > c.ttl {
		c.mu.Lock()
		delete(c.entries, cacheKey(channel, key))
		c.mu.Unlock()
		return "", false
	}
	if e.channel != channel {
		return "", false
	}

	c.mu.Lock()
	e.accessedAt = time.Now()
	c.mu.Unlock()

	return e.value, true
}

// Set stores a value in the cache. Evicts the LRU entry if at capacity.
func (c *ToolCache) Set(channel, key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	ck := cacheKey(channel, key)

	// Update existing entry.
	if _, ok := c.entries[ck]; ok {
		c.entries[ck] = &cacheEntry{
			value:      value,
			channel:    channel,
			createdAt:  time.Now(),
			accessedAt: time.Now(),
		}
		return
	}

	// Evict LRU if at capacity.
	if len(c.entries) >= c.maxEntries {
		c.evictLRU()
	}

	c.entries[ck] = &cacheEntry{
		value:      value,
		channel:    channel,
		createdAt:  time.Now(),
		accessedAt: time.Now(),
	}
}

// evictLRU removes the least-recently-accessed entry. Must be called with mu held.
func (c *ToolCache) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, e := range c.entries {
		if first || e.accessedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.accessedAt
			first = false
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// Len returns the number of entries in the cache.
func (c *ToolCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// StartCleanup runs a background goroutine that removes expired entries
// at the given interval. Returns a stop function.
func (c *ToolCache) StartCleanup(interval time.Duration) func() {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				c.removeExpired()
			case <-done:
				return
			}
		}
	}()
	return func() { close(done) }
}

func (c *ToolCache) removeExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for k, e := range c.entries {
		if now.Sub(e.createdAt) > c.ttl {
			delete(c.entries, k)
		}
	}
}
