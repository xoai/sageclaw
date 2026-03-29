package registry

import "testing"

func TestLoadCuratedIndex(t *testing.T) {
	idx, err := LoadCuratedIndex()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(idx.Servers) != 401 {
		t.Errorf("servers = %d, want 401", len(idx.Servers))
	}
	if len(idx.Categories) != 13 {
		t.Errorf("categories = %d, want 13", len(idx.Categories))
	}
	if idx.Version != 4 {
		t.Errorf("version = %d, want 4", idx.Version)
	}
}

func TestSearchByName(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	results := idx.Search("playwright")
	if len(results) == 0 {
		t.Fatal("search for 'playwright' should return results")
	}
	if results[0].ID != "microsoft-playwright-mcp" {
		t.Errorf("first result = %s, want microsoft-playwright-mcp", results[0].ID)
	}
}

func TestSearchByDescription(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	results := idx.Search("database")
	if len(results) == 0 {
		t.Fatal("search for 'database' should return results")
	}
}

func TestSearchEmpty(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	results := idx.Search("")
	if len(results) != 401 {
		t.Errorf("empty search should return all 401, got %d", len(results))
	}
}

func TestByCategory(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	devTools := idx.ByCategory("developer")
	if len(devTools) != 181 {
		t.Errorf("developer tools = %d, want 181", len(devTools))
	}

	webSearch := idx.ByCategory("web-search")
	if len(webSearch) == 0 {
		t.Error("web-search should have entries")
	}
}

func TestGet(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	s, ok := idx.Get("upstash-context7")
	if !ok {
		t.Fatal("upstash-context7 should exist")
	}
	if s.Connection.Type != "stdio" {
		t.Errorf("upstash-context7 connection type = %s, want stdio", s.Connection.Type)
	}
	if s.FallbackConnection == nil {
		t.Error("upstash-context7 should have fallback connection")
	}
	if s.FallbackConnection.Type != "http" {
		t.Errorf("upstash-context7 fallback type = %s, want http", s.FallbackConnection.Type)
	}

	_, ok = idx.Get("nonexistent")
	if ok {
		t.Error("nonexistent should not be found")
	}
}

func TestSearchByTag(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	results := idx.Search("search")
	if len(results) == 0 {
		t.Fatal("search for 'search' tag should return results")
	}
}

func TestNewFields(t *testing.T) {
	idx, _ := LoadCuratedIndex()

	// Playwright should have tools.
	s, ok := idx.Get("microsoft-playwright-mcp")
	if !ok {
		t.Fatal("microsoft-playwright-mcp should exist")
	}
	if len(s.Tools) == 0 {
		t.Error("playwright should have tools listed")
	}
	if !s.IsFeatured {
		t.Error("playwright should be featured")
	}
}
