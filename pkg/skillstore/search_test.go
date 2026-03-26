package skillstore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchClient(t *testing.T) {
	// Mock skills.sh API.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query().Get("q")
		resp := searchResponse{
			Query: q,
			Skills: []SearchResult{
				{ID: "github/test/skill-1", SkillID: "skill-1", Name: "Test Skill", Source: "test/repo", Installs: 42},
			},
			Count: 1,
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	client := NewSearchClient(srv.URL, nil)

	// First call — should hit the server.
	results, err := client.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SkillID != "skill-1" {
		t.Errorf("expected skill-1, got %s", results[0].SkillID)
	}
	if results[0].Installs != 42 {
		t.Errorf("expected 42 installs, got %d", results[0].Installs)
	}

	// Second call — should hit cache.
	results2, err := client.Search(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("Cached search failed: %v", err)
	}
	if len(results2) != 1 {
		t.Fatalf("expected 1 cached result, got %d", len(results2))
	}
}

func TestSearchClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewSearchClient(srv.URL, nil)
	_, err := client.Search(context.Background(), "test", 10)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
