// Package skillstore provides a Go SDK for the skills.sh marketplace,
// including search, GitHub-based skill fetching, and local skill management.
package skillstore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// SearchResult represents a single skill from the skills.sh search API.
type SearchResult struct {
	ID          string `json:"id"`
	SkillID     string `json:"skillId"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      string `json:"source"`
	Installs    int    `json:"installs"`
}

type searchResponse struct {
	Query    string         `json:"query"`
	Skills   []SearchResult `json:"skills"`
	Count    int            `json:"count"`
	Duration int            `json:"duration_ms"`
}

type cachedResult struct {
	results   []SearchResult
	expiresAt time.Time
}

// SearchClient queries the skills.sh marketplace API.
type SearchClient struct {
	baseURL    string
	httpClient *http.Client

	mu    sync.RWMutex
	cache map[string]cachedResult
}

// NewSearchClient creates a search client.
// baseURL defaults to "https://skills.sh" if empty.
func NewSearchClient(baseURL string, client *http.Client) *SearchClient {
	if baseURL == "" {
		baseURL = "https://skills.sh"
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &SearchClient{
		baseURL:    baseURL,
		httpClient: client,
		cache:      make(map[string]cachedResult),
	}
}

// Search queries the skills.sh API for skills matching the query.
func (c *SearchClient) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	cacheKey := query + ":" + strconv.Itoa(limit)

	// Check cache.
	c.mu.RLock()
	if cached, ok := c.cache[cacheKey]; ok && time.Now().Before(cached.expiresAt) {
		c.mu.RUnlock()
		return cached.results, nil
	}
	c.mu.RUnlock()

	// Build request.
	u, err := url.Parse(c.baseURL + "/api/search")
	if err != nil {
		return nil, fmt.Errorf("parse URL: %w", err)
	}
	q := u.Query()
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(limit))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// On network error, try to return stale cache.
		c.mu.RLock()
		if cached, ok := c.cache[cacheKey]; ok {
			c.mu.RUnlock()
			return cached.results, nil
		}
		c.mu.RUnlock()
		return nil, fmt.Errorf("search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search API returned %d", resp.StatusCode)
	}

	var sr searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	// Cache results for 1 hour.
	c.mu.Lock()
	c.cache[cacheKey] = cachedResult{
		results:   sr.Skills,
		expiresAt: time.Now().Add(1 * time.Hour),
	}
	c.mu.Unlock()

	return sr.Skills, nil
}
