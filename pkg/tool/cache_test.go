package tool

import (
	"testing"
	"time"
)

func TestToolCache_GetSet(t *testing.T) {
	c := NewToolCache(time.Minute, 100)
	c.Set("ch1", "key1", "value1")

	val, ok := c.Get("ch1", "key1")
	if !ok || val != "value1" {
		t.Fatalf("expected value1, got %q (ok=%v)", val, ok)
	}
}

func TestToolCache_Miss(t *testing.T) {
	c := NewToolCache(time.Minute, 100)
	_, ok := c.Get("ch1", "missing")
	if ok {
		t.Fatal("expected cache miss")
	}
}

func TestToolCache_TTLExpiry(t *testing.T) {
	c := NewToolCache(1*time.Millisecond, 100)
	c.Set("ch1", "key1", "value1")
	time.Sleep(5 * time.Millisecond)

	_, ok := c.Get("ch1", "key1")
	if ok {
		t.Fatal("expected cache miss after TTL expiry")
	}
}

func TestToolCache_ChannelIsolation(t *testing.T) {
	c := NewToolCache(time.Minute, 100)
	c.Set("ch1", "key1", "value1")

	// Different channel should not see ch1's data.
	_, ok := c.Get("ch2", "key1")
	if ok {
		t.Fatal("expected cache miss for different channel")
	}
}

func TestToolCache_LRUEviction(t *testing.T) {
	c := NewToolCache(time.Minute, 3)
	c.Set("ch1", "a", "1")
	c.Set("ch1", "b", "2")
	c.Set("ch1", "c", "3")

	// Access "a" to make it recently used.
	c.Get("ch1", "a")

	// Adding a 4th entry should evict the LRU (which is "b").
	c.Set("ch1", "d", "4")

	if _, ok := c.Get("ch1", "b"); ok {
		t.Fatal("expected 'b' to be evicted (LRU)")
	}
	if val, ok := c.Get("ch1", "a"); !ok || val != "1" {
		t.Fatal("expected 'a' to survive (recently accessed)")
	}
	if val, ok := c.Get("ch1", "d"); !ok || val != "4" {
		t.Fatal("expected 'd' to exist (just added)")
	}
}

func TestToolCache_Len(t *testing.T) {
	c := NewToolCache(time.Minute, 100)
	c.Set("ch1", "a", "1")
	c.Set("ch1", "b", "2")
	if c.Len() != 2 {
		t.Fatalf("expected len 2, got %d", c.Len())
	}
}

func TestToolCache_Update(t *testing.T) {
	c := NewToolCache(time.Minute, 100)
	c.Set("ch1", "key", "old")
	c.Set("ch1", "key", "new")

	val, ok := c.Get("ch1", "key")
	if !ok || val != "new" {
		t.Fatalf("expected updated value 'new', got %q", val)
	}
	if c.Len() != 1 {
		t.Fatalf("update should not increase count, got %d", c.Len())
	}
}
