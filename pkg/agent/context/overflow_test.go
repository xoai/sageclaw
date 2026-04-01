package context

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestOverflowManager_PersistAndRead(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	content := "hello world, this is a large tool result"
	path, err := om.Persist("sess1", "tc_001", content)
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}

	// Verify path layout.
	wantPath := filepath.Join(dir, "sess1", "tc_001.txt")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}

	// Read back.
	got, err := om.Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if got != content {
		t.Errorf("Read = %q, want %q", got, content)
	}
}

func TestOverflowManager_ReadMissing(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	got, err := om.Read(filepath.Join(dir, "nonexistent", "tc_999.txt"))
	if err != nil {
		t.Fatalf("Read missing: %v", err)
	}
	if got != OverflowPlaceholder {
		t.Errorf("Read missing = %q, want placeholder", got)
	}
}

func TestOverflowManager_CleanSession(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Persist two files.
	om.Persist("sess1", "tc_001", "aaa")
	om.Persist("sess1", "tc_002", "bbb")

	// Clean.
	if err := om.CleanSession("sess1"); err != nil {
		t.Fatalf("CleanSession: %v", err)
	}

	// Directory should be gone.
	if _, err := os.Stat(filepath.Join(dir, "sess1")); !os.IsNotExist(err) {
		t.Error("session dir still exists after CleanSession")
	}
}

func TestOverflowManager_CleanSession_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Should not error on missing session.
	if err := om.CleanSession("nosuch"); err != nil {
		t.Fatalf("CleanSession nonexistent: %v", err)
	}
}

func TestOverflowManager_EnforceQuota(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Write 3 files of known sizes with different timestamps.
	sess := "sess1"
	om.Persist(sess, "old", strings.Repeat("A", 1000))
	// Force different mod times so oldest-first eviction is deterministic.
	time.Sleep(10 * time.Millisecond)
	om.Persist(sess, "mid", strings.Repeat("B", 500))
	time.Sleep(10 * time.Millisecond)
	om.Persist(sess, "new", strings.Repeat("C", 500))

	// Total = 2000 bytes. Set quota to 1200 — should evict "old" (1000 bytes).
	// Build history with matching annotations.
	history := []canonical.Message{
		{
			Role: "tool",
			Content: []canonical.Content{
				{ToolResult: &canonical.ToolResult{ToolCallID: "old", Content: "preview..."}},
			},
			Annotations: &canonical.MessageAnnotations{
				OverflowPath: filepath.Join(dir, sess, "old.txt"),
			},
		},
		{
			Role: "tool",
			Content: []canonical.Content{
				{ToolResult: &canonical.ToolResult{ToolCallID: "mid", Content: "preview..."}},
			},
			Annotations: &canonical.MessageAnnotations{
				OverflowPath: filepath.Join(dir, sess, "mid.txt"),
			},
		},
	}

	err := om.EnforceQuota(sess, 1200, history)
	if err != nil {
		t.Fatalf("EnforceQuota: %v", err)
	}

	// "old.txt" should be deleted.
	if _, err := os.Stat(filepath.Join(dir, sess, "old.txt")); !os.IsNotExist(err) {
		t.Error("old.txt should have been evicted")
	}

	// "mid.txt" and "new.txt" should still exist.
	if _, err := os.Stat(filepath.Join(dir, sess, "mid.txt")); err != nil {
		t.Error("mid.txt should still exist")
	}
	if _, err := os.Stat(filepath.Join(dir, sess, "new.txt")); err != nil {
		t.Error("new.txt should still exist")
	}

	// History annotation for "old" should have placeholder content.
	if history[0].Content[0].ToolResult.Content != OverflowPlaceholder {
		t.Errorf("evicted message content = %q, want placeholder", history[0].Content[0].ToolResult.Content)
	}
	// "mid" should be unchanged.
	if history[1].Content[0].ToolResult.Content != "preview..." {
		t.Errorf("non-evicted message content changed: %q", history[1].Content[0].ToolResult.Content)
	}
}

func TestOverflowManager_EnforceQuota_UnderBudget(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	om.Persist("sess1", "tc_001", strings.Repeat("X", 100))

	// Quota is larger than total — nothing should be evicted.
	err := om.EnforceQuota("sess1", 10000, nil)
	if err != nil {
		t.Fatalf("EnforceQuota: %v", err)
	}

	// File should still exist.
	if _, err := os.Stat(filepath.Join(dir, "sess1", "tc_001.txt")); err != nil {
		t.Error("file should not have been evicted")
	}
}

func TestOverflowManager_EnforceQuota_NoSession(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	// Non-existent session should not error.
	if err := om.EnforceQuota("nosuch", 100, nil); err != nil {
		t.Fatalf("EnforceQuota missing session: %v", err)
	}
}

func TestOverflowManager_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			content := strings.Repeat("X", 100)
			_, err := om.Persist("sess1", "tc_"+string(rune('A'+n)), content)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent Persist error: %v", err)
	}
}

func TestOverflowManager_MultipleSessionsIsolated(t *testing.T) {
	dir := t.TempDir()
	om := NewOverflowManager(dir)

	om.Persist("sess1", "tc_001", "aaa")
	om.Persist("sess2", "tc_001", "bbb")

	// Clean sess1 should not affect sess2.
	om.CleanSession("sess1")

	got, err := om.Read(filepath.Join(dir, "sess2", "tc_001.txt"))
	if err != nil {
		t.Fatalf("Read sess2: %v", err)
	}
	if got != "bbb" {
		t.Errorf("sess2 content = %q after cleaning sess1", got)
	}
}
