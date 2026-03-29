package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
)

func newMCPEntry(id, name, category string, stars int) store.MCPRegistryEntry {
	return store.MCPRegistryEntry{
		ID:           id,
		Name:         name,
		Category:     category,
		Connection:   `{"type":"stdio","command":"npx","args":["-y","test"]}`,
		ConfigSchema: `{"API_KEY":{"type":"string","required":true}}`,
		Stars:        stars,
		Tags:         []string{"test"},
		Source:       "curated",
	}
}

func TestMCPUpsertAndGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	entry := newMCPEntry("brave-search", "Brave Search", "web-search", 22000)
	if err := s.UpsertMCPEntry(ctx, entry); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetMCPEntry(ctx, "brave-search")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Brave Search" {
		t.Errorf("name = %q, want %q", got.Name, "Brave Search")
	}
	if got.Stars != 22000 {
		t.Errorf("stars = %d, want 22000", got.Stars)
	}
	if got.Source != "curated" {
		t.Errorf("source = %q, want curated", got.Source)
	}
	if got.Status != "available" {
		t.Errorf("status = %q, want available", got.Status)
	}

	// Upsert again with updated stars.
	entry.Stars = 25000
	if err := s.UpsertMCPEntry(ctx, entry); err != nil {
		t.Fatalf("upsert update: %v", err)
	}
	got, _ = s.GetMCPEntry(ctx, "brave-search")
	if got.Stars != 25000 {
		t.Errorf("stars after update = %d, want 25000", got.Stars)
	}
}

func TestMCPListByCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("github", "GitHub", "developer", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("git", "Git", "developer", 22000))

	entries, err := s.ListMCPEntries(ctx, store.MCPFilter{Category: "developer"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("developer count = %d, want 2", len(entries))
	}
}

func TestMCPSetStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))

	// Install (set to installing).
	s.SetMCPStatus(ctx, "brave", "installing", "")
	got, _ := s.GetMCPEntry(ctx, "brave")
	if got.Status != "installing" {
		t.Errorf("status = %q, want installing", got.Status)
	}
	if got.Installed {
		t.Error("installing should not be installed")
	}

	// Connected.
	s.SetMCPStatus(ctx, "brave", "connected", "")
	got, _ = s.GetMCPEntry(ctx, "brave")
	if got.Status != "connected" {
		t.Errorf("status = %q, want connected", got.Status)
	}
	if !got.Installed {
		t.Error("connected should be installed")
	}
	if !got.Enabled {
		t.Error("connected should be enabled")
	}
	if got.InstalledAt == nil {
		t.Error("installed_at should be set")
	}

	// Disable.
	s.SetMCPStatus(ctx, "brave", "disabled", "")
	got, _ = s.GetMCPEntry(ctx, "brave")
	if got.Status != "disabled" {
		t.Errorf("status = %q, want disabled", got.Status)
	}
	if !got.Installed {
		t.Error("disabled should still be installed")
	}
	if got.Enabled {
		t.Error("disabled should not be enabled")
	}

	// Failed.
	s.SetMCPStatus(ctx, "brave", "failed", "connection timeout")
	got, _ = s.GetMCPEntry(ctx, "brave")
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.StatusError != "connection timeout" {
		t.Errorf("status_error = %q, want 'connection timeout'", got.StatusError)
	}
	if got.Installed {
		t.Error("failed should not be installed")
	}

	// Back to available.
	s.SetMCPStatus(ctx, "brave", "available", "")
	got, _ = s.GetMCPEntry(ctx, "brave")
	if got.Status != "available" {
		t.Errorf("status = %q, want available", got.Status)
	}
	if got.Installed {
		t.Error("available should not be installed")
	}
}

func TestMCPListByStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("github", "GitHub", "developer", 22000))
	s.SetMCPStatus(ctx, "brave", "connected", "")

	entries, err := s.ListMCPEntries(ctx, store.MCPFilter{Status: []string{"connected"}})
	if err != nil {
		t.Fatalf("list by status: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("connected count = %d, want 1", len(entries))
	}
	if entries[0].ID != "brave" {
		t.Errorf("connected entry = %s, want brave", entries[0].ID)
	}

	// Multi-status filter.
	s.SetMCPStatus(ctx, "github", "failed", "timeout")
	entries, _ = s.ListMCPEntries(ctx, store.MCPFilter{Status: []string{"connected", "failed"}})
	if len(entries) != 2 {
		t.Errorf("multi-status count = %d, want 2", len(entries))
	}
}

func TestMCPSearch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave Search", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("github", "GitHub", "developer", 22000))

	results, err := s.SearchMCPEntries(ctx, "brave", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("search results = %d, want 1", len(results))
	}
	if results[0].ID != "brave" {
		t.Errorf("search result = %s, want brave", results[0].ID)
	}
}

func TestMCPAgentAssignment(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.SetMCPAgents(ctx, "brave", []string{"default", "researcher"})

	got, _ := s.GetMCPEntry(ctx, "brave")
	if len(got.AgentIDs) != 2 {
		t.Errorf("agent_ids count = %d, want 2", len(got.AgentIDs))
	}
	if got.AgentIDs[0] != "default" || got.AgentIDs[1] != "researcher" {
		t.Errorf("agent_ids = %v, want [default researcher]", got.AgentIDs)
	}
}

func TestMCPDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.DeleteMCPEntry(ctx, "brave")

	_, err := s.GetMCPEntry(ctx, "brave")
	if err == nil {
		t.Error("should return error after delete")
	}
}

func TestMCPDeleteCredential(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Store a credential first.
	key := make([]byte, 32)
	s.StoreCredential(ctx, "mcp:brave:API_KEY", []byte("test-key"), key)

	// Verify it exists.
	val, err := s.GetCredential(ctx, "mcp:brave:API_KEY", key)
	if err != nil || string(val) != "test-key" {
		t.Fatalf("credential should exist, got err=%v", err)
	}

	// Delete it.
	s.DeleteMCPCredential(ctx, "mcp:brave:API_KEY")

	// Verify it's gone.
	_, err = s.GetCredential(ctx, "mcp:brave:API_KEY", key)
	if err == nil {
		t.Error("credential should be deleted")
	}
}

func TestMCPCountByCategory(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("fetch", "Fetch", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("github", "GitHub", "developer", 22000))
	s.SetMCPStatus(ctx, "brave", "connected", "")
	s.SetMCPStatus(ctx, "github", "connected", "")

	counts, err := s.CountMCPByCategory(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if counts["web-search"] != 1 {
		t.Errorf("web-search installed = %d, want 1", counts["web-search"])
	}
	if counts["developer"] != 1 {
		t.Errorf("developer installed = %d, want 1", counts["developer"])
	}
}

func TestMCPCountInstalled(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	s.UpsertMCPEntry(ctx, newMCPEntry("brave", "Brave", "web-search", 22000))
	s.UpsertMCPEntry(ctx, newMCPEntry("github", "GitHub", "developer", 22000))
	s.SetMCPStatus(ctx, "brave", "connected", "")

	count, err := s.CountMCPInstalled(ctx)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("installed count = %d, want 1", count)
	}
}

func TestMCPSeedVersion(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Default version is 0.
	v, err := s.GetMCPSeedVersion(ctx)
	if err != nil {
		t.Fatalf("get seed version: %v", err)
	}
	if v != 0 {
		t.Errorf("initial version = %d, want 0", v)
	}

	// Set version.
	if err := s.SetMCPSeedVersion(ctx, 2); err != nil {
		t.Fatalf("set seed version: %v", err)
	}

	v, _ = s.GetMCPSeedVersion(ctx)
	if v != 2 {
		t.Errorf("version = %d, want 2", v)
	}

	// Update version.
	s.SetMCPSeedVersion(ctx, 3)
	v, _ = s.GetMCPSeedVersion(ctx)
	if v != 3 {
		t.Errorf("version = %d, want 3", v)
	}
}

// Suppress unused import.
var _ = time.Now
