// FTS5 BM25 Benchmark — ADR-014 Go/No-Go Gate
//
// Targets:
//   - Search < 10ms for 10k entries
//   - Write throughput > 50 writes/second
//
// If these targets are not met, evaluate mattn/go-sqlite3 (CGo) as fallback.
package fts5

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

var sampleWords = []string{
	"algorithm", "binary", "cache", "database", "encryption",
	"framework", "goroutine", "handler", "interface", "json",
	"kubernetes", "latency", "middleware", "network", "optimization",
	"pipeline", "query", "router", "scheduler", "transaction",
	"unicode", "validation", "websocket", "xml", "yaml",
	"authentication", "buffer", "concurrency", "deployment", "endpoint",
	"function", "gateway", "hashing", "indexing", "javascript",
	"kernel", "logging", "memory", "namespace", "orchestration",
	"protocol", "queue", "replication", "serialization", "thread",
	"upstream", "virtualization", "worker", "encoding", "zookeeper",
}

var sampleTags = []string{
	"go", "python", "rust", "javascript", "database",
	"devops", "frontend", "backend", "security", "networking",
	"architecture", "testing", "performance", "debugging", "api",
}

func generateContent(rng *rand.Rand, wordCount int) string {
	words := make([]string, wordCount)
	for i := range words {
		words[i] = sampleWords[rng.Intn(len(sampleWords))]
	}
	result := ""
	for i, w := range words {
		if i > 0 {
			result += " "
		}
		result += w
	}
	return result
}

func generateTags(rng *rand.Rand, count int) []string {
	tags := make([]string, count)
	for i := range tags {
		tags[i] = sampleTags[rng.Intn(len(sampleTags))]
	}
	return tags
}

func seedBenchMemories(b *testing.B, e *Engine, count int) {
	b.Helper()
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	for i := 0; i < count; i++ {
		content := generateContent(rng, 20+rng.Intn(30))
		title := generateContent(rng, 3+rng.Intn(5))
		tags := generateTags(rng, 1+rng.Intn(3))
		if _, err := e.Write(ctx, content, title, tags); err != nil {
			b.Fatalf("seeding memory %d: %v", i, err)
		}
	}
}

func BenchmarkFTS5Search_10k(b *testing.B) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		b.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	engine := New(store)
	seedBenchMemories(b, engine, 10000)

	ctx := context.Background()
	queries := []string{
		"goroutine concurrency",
		"database query optimization",
		"kubernetes deployment",
		"authentication middleware",
		"websocket handler",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		q := queries[i%len(queries)]
		_, err := engine.Search(ctx, q, memory.SearchOptions{Limit: 10})
		if err != nil {
			b.Fatalf("search failed: %v", err)
		}
	}
}

func BenchmarkFTS5Write(b *testing.B) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		b.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	engine := New(store)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(42))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		content := generateContent(rng, 20+rng.Intn(30)) + fmt.Sprintf(" unique_%d", i)
		title := generateContent(rng, 3)
		tags := generateTags(rng, 2)
		if _, err := engine.Write(ctx, content, title, tags); err != nil {
			b.Fatalf("write failed: %v", err)
		}
	}
}

// TestFTS5Benchmark_GoNoGo runs the benchmark targets as pass/fail tests.
func TestFTS5Benchmark_GoNoGo(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	engine := New(store)
	ctx := context.Background()

	// Seed 10k memories.
	t.Log("Seeding 10,000 memories...")
	rng := rand.New(rand.NewSource(42))
	start := time.Now()
	for i := 0; i < 10000; i++ {
		content := generateContent(rng, 20+rng.Intn(30))
		title := generateContent(rng, 3+rng.Intn(5))
		tags := generateTags(rng, 1+rng.Intn(3))
		if _, err := engine.Write(ctx, content, title, tags); err != nil {
			t.Fatalf("seeding memory %d: %v", i, err)
		}
	}
	seedDuration := time.Since(start)
	writeRate := float64(10000) / seedDuration.Seconds()
	t.Logf("Seed complete: %.0f writes/sec (%.2fs total)", writeRate, seedDuration.Seconds())

	// GO/NO-GO: Write throughput > 50/s
	if writeRate < 50 {
		t.Fatalf("FAIL: write throughput %.0f/s < 50/s target", writeRate)
	}
	t.Logf("PASS: write throughput %.0f/s >= 50/s target", writeRate)

	// Search benchmark.
	queries := []string{
		"goroutine concurrency",
		"database query optimization",
		"kubernetes deployment",
		"authentication middleware",
		"websocket handler protocol",
	}

	var totalDuration time.Duration
	iterations := 100
	for i := 0; i < iterations; i++ {
		q := queries[i%len(queries)]
		searchStart := time.Now()
		results, err := engine.Search(ctx, q, memory.SearchOptions{Limit: 10})
		elapsed := time.Since(searchStart)
		totalDuration += elapsed
		if err != nil {
			t.Fatalf("search failed: %v", err)
		}
		_ = results
	}

	avgSearch := totalDuration / time.Duration(iterations)
	t.Logf("Average search latency: %v (%d iterations)", avgSearch, iterations)

	// GO/NO-GO: Search < 10ms for 10k entries
	if avgSearch > 10*time.Millisecond {
		t.Fatalf("FAIL: average search %v > 10ms target", avgSearch)
	}
	t.Logf("PASS: average search %v <= 10ms target", avgSearch)
}
