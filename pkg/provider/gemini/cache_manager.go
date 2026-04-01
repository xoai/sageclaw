package gemini

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// CacheManager manages Gemini context caching for system prompts.
// It creates cached content objects via the Gemini API and reuses
// them when the system prompt hasn't changed (hash-based detection).
type CacheManager struct {
	apiKey  string
	baseURL string
	client  *http.Client

	mu        sync.Mutex
	cacheID   string // Current cached content resource name.
	cacheHash string // SHA-256 of the cached content.
}

// NewCacheManager creates a cache manager for Gemini context caching.
func NewCacheManager(apiKey, baseURL string) *CacheManager {
	return &CacheManager{
		apiKey:  apiKey,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// EnsureCache creates or reuses a cached content object for the given
// system parts. Returns the cache resource name for use in requests.
// Returns empty string if caching fails (caller falls back to inline).
func (cm *CacheManager) EnsureCache(ctx context.Context, parts []canonical.SystemPart, model string) string {
	// Concatenate cacheable parts.
	var cacheable string
	for _, p := range parts {
		if p.Cacheable && p.Content != "" {
			if cacheable != "" {
				cacheable += "\n\n"
			}
			cacheable += p.Content
		}
	}
	if cacheable == "" {
		return "" // Nothing to cache.
	}

	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(cacheable)))

	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Reuse if hash matches.
	if hash == cm.cacheHash && cm.cacheID != "" {
		return cm.cacheID
	}

	// Create new cached content using systemInstruction (not contents).
	cacheReq := map[string]any{
		"model": "models/" + model,
		"systemInstruction": map[string]any{
			"parts": []map[string]string{
				{"text": cacheable},
			},
		},
		"ttl": "3600s", // 1 hour TTL.
	}

	body, _ := json.Marshal(cacheReq)
	url := fmt.Sprintf("%s/cachedContents?key=%s", cm.baseURL, cm.apiKey)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		log.Printf("[gemini-cache] request creation failed: %v", err)
		return ""
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := cm.client.Do(req)
	if err != nil {
		log.Printf("[gemini-cache] API call failed: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		log.Printf("[gemini-cache] API returned %d: %s", resp.StatusCode, string(respBody))
		return "" // Fallback to inline.
	}

	var result struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		log.Printf("[gemini-cache] response decode failed: %v", err)
		return ""
	}

	// Delete old cache if we had one.
	if cm.cacheID != "" {
		go cm.deleteCache(cm.cacheID)
	}

	cm.cacheID = result.Name
	cm.cacheHash = hash
	log.Printf("[gemini-cache] created cache: %s (hash: %s...)", result.Name, hash[:8])
	return result.Name
}

// Close deletes the cached content object. Call on session end.
func (cm *CacheManager) Close() {
	cm.mu.Lock()
	id := cm.cacheID
	cm.cacheID = ""
	cm.cacheHash = ""
	cm.mu.Unlock()

	if id != "" {
		cm.deleteCache(id)
	}
}

func (cm *CacheManager) deleteCache(name string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/%s?key=%s", cm.baseURL, name, cm.apiKey)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return
	}
	resp, err := cm.client.Do(req)
	if err != nil {
		log.Printf("[gemini-cache] delete failed: %v", err)
		return
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		log.Printf("[gemini-cache] deleted cache: %s", name)
	}
}
