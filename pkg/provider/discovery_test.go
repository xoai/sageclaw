package provider

import (
	"context"
	"fmt"
	"testing"
)

type mockListerProvider struct {
	stubProvider
	models []ModelInfo
	err    error
}

func (p *mockListerProvider) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.models, nil
}

func TestDiscoverAll_Success(t *testing.T) {
	providers := map[string]Provider{
		"anthropic": &mockListerProvider{
			stubProvider: stubProvider{name: "anthropic"},
			models: []ModelInfo{
				{ID: "anthropic/claude-sonnet-4", Provider: "anthropic", ModelID: "claude-sonnet-4"},
			},
		},
		"gemini": &mockListerProvider{
			stubProvider: stubProvider{name: "gemini"},
			models: []ModelInfo{
				{ID: "gemini/gemini-2.5-flash", Provider: "gemini", ModelID: "gemini-2.5-flash"},
				{ID: "gemini/gemini-2.5-pro", Provider: "gemini", ModelID: "gemini-2.5-pro"},
			},
		},
	}

	results := DiscoverAll(context.Background(), providers)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	total := TotalDiscovered(results)
	if total != 3 {
		t.Fatalf("expected 3 total models, got %d", total)
	}
}

func TestDiscoverAll_PartialFailure(t *testing.T) {
	providers := map[string]Provider{
		"anthropic": &mockListerProvider{
			stubProvider: stubProvider{name: "anthropic"},
			models:       []ModelInfo{{ID: "anthropic/claude-sonnet-4"}},
		},
		"gemini": &mockListerProvider{
			stubProvider: stubProvider{name: "gemini"},
			err:          fmt.Errorf("HTTP 401: invalid API key"),
		},
	}

	results := DiscoverAll(context.Background(), providers)
	if len(results) != 2 {
		t.Fatalf("expected 2 results (1 success + 1 error), got %d", len(results))
	}

	total := TotalDiscovered(results)
	if total != 1 {
		t.Fatalf("expected 1 model from successful provider, got %d", total)
	}

	// Verify the error result exists.
	var errCount int
	for _, r := range results {
		if r.Err != nil {
			errCount++
		}
	}
	if errCount != 1 {
		t.Fatalf("expected 1 error result, got %d", errCount)
	}
}

func TestDiscoverAll_NonListerSkipped(t *testing.T) {
	providers := map[string]Provider{
		"stub": &stubProvider{name: "stub"}, // Does NOT implement ModelLister.
		"anthropic": &mockListerProvider{
			stubProvider: stubProvider{name: "anthropic"},
			models:       []ModelInfo{{ID: "anthropic/claude-sonnet-4"}},
		},
	}

	results := DiscoverAll(context.Background(), providers)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (stub skipped), got %d", len(results))
	}
	if results[0].Provider != "anthropic" {
		t.Fatalf("expected anthropic result, got %s", results[0].Provider)
	}
}

func TestDiscoverAll_Empty(t *testing.T) {
	results := DiscoverAll(context.Background(), map[string]Provider{})
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty providers, got %d", len(results))
	}
	if TotalDiscovered(results) != 0 {
		t.Fatal("expected 0 total for empty results")
	}
}
