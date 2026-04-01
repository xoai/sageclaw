package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestCacheManager_CreateAndReuse(t *testing.T) {
	var createCalls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			atomic.AddInt64(&createCalls, 1)
			json.NewEncoder(w).Encode(map[string]string{"name": "cachedContents/abc123"})
			return
		}
		if r.Method == "DELETE" {
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(404)
	}))
	defer server.Close()

	cm := NewCacheManager("test-key", server.URL)
	ctx := context.Background()
	parts := []canonical.SystemPart{
		{Content: "Base prompt", Cacheable: true},
		{Content: "Dynamic", Cacheable: false},
	}

	// First call: creates cache.
	id := cm.EnsureCache(ctx, parts, "gemini-2.0-flash")
	if id != "cachedContents/abc123" {
		t.Errorf("expected cache ID, got %q", id)
	}
	if atomic.LoadInt64(&createCalls) != 1 {
		t.Errorf("expected 1 create call, got %d", createCalls)
	}

	// Second call with same parts: reuses cache.
	id2 := cm.EnsureCache(ctx, parts, "gemini-2.0-flash")
	if id2 != "cachedContents/abc123" {
		t.Errorf("expected same cache ID, got %q", id2)
	}
	if atomic.LoadInt64(&createCalls) != 1 {
		t.Errorf("expected still 1 create call (reuse), got %d", createCalls)
	}
}

func TestCacheManager_RecreateOnChange(t *testing.T) {
	var createCalls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			n := atomic.AddInt64(&createCalls, 1)
			json.NewEncoder(w).Encode(map[string]string{"name": "cachedContents/" + string(rune('a'+n))})
			return
		}
		w.WriteHeader(200) // DELETE
	}))
	defer server.Close()

	cm := NewCacheManager("test-key", server.URL)
	ctx := context.Background()

	parts1 := []canonical.SystemPart{{Content: "Version 1", Cacheable: true}}
	cm.EnsureCache(ctx, parts1, "gemini-2.0-flash")
	if atomic.LoadInt64(&createCalls) != 1 {
		t.Fatal("expected 1 create")
	}

	// Change content → new hash → recreate.
	parts2 := []canonical.SystemPart{{Content: "Version 2", Cacheable: true}}
	cm.EnsureCache(ctx, parts2, "gemini-2.0-flash")
	if atomic.LoadInt64(&createCalls) != 2 {
		t.Errorf("expected 2 creates (content changed), got %d", createCalls)
	}
}

func TestCacheManager_NoCacheableContent(t *testing.T) {
	cm := NewCacheManager("test-key", "http://unused")
	ctx := context.Background()

	parts := []canonical.SystemPart{
		{Content: "Dynamic only", Cacheable: false},
	}
	id := cm.EnsureCache(ctx, parts, "gemini-2.0-flash")
	if id != "" {
		t.Errorf("expected empty ID for no cacheable content, got %q", id)
	}
}

func TestCacheManager_APIError_Fallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal"}`))
	}))
	defer server.Close()

	cm := NewCacheManager("test-key", server.URL)
	ctx := context.Background()

	parts := []canonical.SystemPart{{Content: "Base", Cacheable: true}}
	id := cm.EnsureCache(ctx, parts, "gemini-2.0-flash")
	if id != "" {
		t.Errorf("expected empty ID on API error, got %q", id)
	}
}

func TestCacheManager_Close(t *testing.T) {
	var deleteCalls int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			atomic.AddInt64(&deleteCalls, 1)
		}
		json.NewEncoder(w).Encode(map[string]string{"name": "cachedContents/to-delete"})
	}))
	defer server.Close()

	cm := NewCacheManager("test-key", server.URL)
	ctx := context.Background()

	parts := []canonical.SystemPart{{Content: "Base", Cacheable: true}}
	cm.EnsureCache(ctx, parts, "gemini-2.0-flash")

	cm.Close()

	// Give background goroutine time to fire delete.
	// Close() calls deleteCache synchronously, so it should be immediate.
	if atomic.LoadInt64(&deleteCalls) != 1 {
		t.Errorf("expected 1 delete call on Close, got %d", deleteCalls)
	}

	// After close, cacheID should be empty.
	if cm.cacheID != "" {
		t.Error("cacheID should be empty after Close")
	}
}
