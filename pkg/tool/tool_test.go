package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/memory/fts5"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestRegistry(t *testing.T) (*Registry, *security.Sandbox, *sqlite.Store) {
	t.Helper()
	dir := t.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	engine := fts5.New(store)
	reg := NewRegistry()
	RegisterFS(reg, sb)
	RegisterExec(reg, dir, nil)
	RegisterWeb(reg, nil)
	RegisterMemory(reg, engine)
	RegisterCron(reg, store)
	RegisterSpawnTools(reg, nil)

	return reg, sb, store
}

// --- Registry tests ---

func TestRegistry_RegisterAndList(t *testing.T) {
	reg, _, _ := newTestRegistry(t)
	names := reg.Names()
	if len(names) < 8 {
		t.Fatalf("expected at least 8 tools, got %d: %v", len(names), names)
	}
}

func TestRegistry_Execute_Unknown(t *testing.T) {
	reg := NewRegistry()
	_, err := reg.Execute(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

// --- FS tools tests ---

func TestFS_ReadFile(t *testing.T) {
	reg, sb, _ := newTestRegistry(t)
	ctx := context.Background()

	// Create a test file.
	testFile := filepath.Join(sb.Root(), "test.txt")
	os.WriteFile(testFile, []byte("hello world"), 0644)

	result, err := reg.Execute(ctx, "read_file", json.RawMessage(`{"path":"test.txt"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if result.Content != "hello world" {
		t.Fatalf("expected 'hello world', got: %s", result.Content)
	}
}

func TestFS_WriteFile(t *testing.T) {
	reg, sb, _ := newTestRegistry(t)
	ctx := context.Background()

	result, err := reg.Execute(ctx, "write_file", json.RawMessage(`{"path":"new.txt","content":"new content"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify file was created.
	data, err := os.ReadFile(filepath.Join(sb.Root(), "new.txt"))
	if err != nil {
		t.Fatalf("reading file: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("expected 'new content', got: %s", data)
	}
}

func TestFS_ListDirectory(t *testing.T) {
	reg, sb, _ := newTestRegistry(t)
	ctx := context.Background()

	os.WriteFile(filepath.Join(sb.Root(), "a.txt"), []byte("a"), 0644)
	os.WriteFile(filepath.Join(sb.Root(), "b.txt"), []byte("b"), 0644)
	os.MkdirAll(filepath.Join(sb.Root(), "subdir"), 0755)

	result, err := reg.Execute(ctx, "list_directory", json.RawMessage(`{"path":"."}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "a.txt") || !strings.Contains(result.Content, "subdir/") {
		t.Fatalf("expected listing with a.txt and subdir/, got: %s", result.Content)
	}
}

func TestFS_ReadOutsideSandbox(t *testing.T) {
	reg, _, _ := newTestRegistry(t)
	ctx := context.Background()

	result, err := reg.Execute(ctx, "read_file", json.RawMessage(`{"path":"../../etc/passwd"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for path traversal")
	}
}

// --- Memory tools tests ---

func TestMemory_SearchTool(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	engine := fts5.New(store)
	engine.Write(context.Background(), "Go channels enable concurrency", "Go channels", []string{"go"})

	reg := NewRegistry()
	RegisterMemory(reg, engine)

	result, err := reg.Execute(context.Background(), "memory_search",
		json.RawMessage(`{"query":"goroutines concurrency"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Go channels") {
		t.Fatalf("expected Go channels in results, got: %s", result.Content)
	}
}

func TestMemory_GetTool(t *testing.T) {
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	defer store.Close()

	engine := fts5.New(store)
	id, _ := engine.Write(context.Background(), "test memory content", "test", nil)

	reg := NewRegistry()
	RegisterMemory(reg, engine)

	result, err := reg.Execute(context.Background(), "memory_get",
		json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "test memory content") {
		t.Fatalf("expected content in result, got: %s", result.Content)
	}
}

// --- Cron tools tests ---

func TestCron_CreateListDelete(t *testing.T) {
	_, _, store := newTestRegistry(t)
	reg := NewRegistry()
	RegisterCron(reg, store)
	ctx := context.Background()

	// Insert a default agent to satisfy foreign key.
	store.DB().ExecContext(ctx, `INSERT INTO agents (id, name, model) VALUES ('default', 'test', 'test-model')`)

	// Create.
	result, err := reg.Execute(ctx, "cron_create",
		json.RawMessage(`{"schedule":"@hourly","prompt":"check status"}`))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "Cron job created") {
		t.Fatalf("expected creation message, got: %s", result.Content)
	}

	// List.
	result, err = reg.Execute(ctx, "cron_list", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if !strings.Contains(result.Content, "@hourly") {
		t.Fatalf("expected job in list, got: %s", result.Content)
	}

	// Get the ID from the store to delete.
	jobs, _ := store.ListCronJobs(ctx)
	if len(jobs) == 0 {
		t.Fatal("expected at least one job")
	}

	// Delete.
	result, err = reg.Execute(ctx, "cron_delete",
		json.RawMessage(`{"id":"`+jobs[0].ID+`"}`))
	if err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}

	// Verify deleted.
	result, _ = reg.Execute(ctx, "cron_list", json.RawMessage(`{}`))
	if !strings.Contains(result.Content, "No cron jobs") {
		t.Fatalf("expected no jobs, got: %s", result.Content)
	}
}

func TestCron_InvalidSchedule(t *testing.T) {
	_, _, store := newTestRegistry(t)
	reg := NewRegistry()
	RegisterCron(reg, store)

	result, err := reg.Execute(context.Background(), "cron_create",
		json.RawMessage(`{"schedule":"invalid","prompt":"test"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for invalid schedule")
	}
}

// --- Spawn tests ---

func TestSpawn_NilSpawner(t *testing.T) {
	reg := NewRegistry()
	RegisterSpawnTools(reg, nil)

	result, err := reg.Execute(context.Background(), "spawn",
		json.RawMessage(`{"task":"summarize this file"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error with nil spawner, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "not available") {
		t.Fatalf("expected 'not available', got: %s", result.Content)
	}
}

func TestSpawn_RejectsAgentID(t *testing.T) {
	reg := NewRegistry()
	RegisterSpawnTools(reg, nil)

	result, err := reg.Execute(context.Background(), "spawn",
		json.RawMessage(`{"task":"do something","agent_id":"other-agent"}`))
	if err != nil {
		t.Fatalf("executing: %v", err)
	}
	if !strings.Contains(result.Content, "delegate") {
		t.Fatalf("expected guidance to use delegate, got: %s", result.Content)
	}
}

// --- SSRF tests ---

func TestSSRF_PrivateIP(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/secret",
		"http://192.168.1.1/admin",
		"http://10.0.0.1/internal",
	}
	for _, url := range tests {
		err := checkSSRF(url)
		if err == nil {
			t.Errorf("expected SSRF block for %s", url)
		}
	}
}

// --- Interface compliance ---

var _ memory.MemoryEngine = (*fts5.Engine)(nil)
