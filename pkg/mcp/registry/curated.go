package registry

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

//go:embed mcp_index.json.gz
var embeddedIndexGz []byte

// LoadCuratedIndex decompresses and parses the embedded curated MCP index.
func LoadCuratedIndex() (*CuratedIndex, error) {
	gr, err := gzip.NewReader(bytes.NewReader(embeddedIndexGz))
	if err != nil {
		return nil, fmt.Errorf("decompressing curated index: %w", err)
	}
	defer gr.Close()

	data, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("reading curated index: %w", err)
	}

	var idx CuratedIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parsing curated index: %w", err)
	}
	return &idx, nil
}

// Search returns servers matching the query (case-insensitive substring
// match on name, description, and tags). Results sorted: exact ID match
// first, then name match, then others.
func (idx *CuratedIndex) Search(query string) []CuratedServer {
	if query == "" {
		return idx.Servers
	}
	q := strings.ToLower(query)

	var exact, nameMatch, other []CuratedServer
	for _, s := range idx.Servers {
		if strings.ToLower(s.ID) == q {
			exact = append(exact, s)
			continue
		}
		if strings.Contains(strings.ToLower(s.Name), q) {
			nameMatch = append(nameMatch, s)
			continue
		}
		if strings.Contains(strings.ToLower(s.Description), q) || matchTags(s.Tags, q) {
			other = append(other, s)
		}
	}

	result := make([]CuratedServer, 0, len(exact)+len(nameMatch)+len(other))
	result = append(result, exact...)
	result = append(result, nameMatch...)
	result = append(result, other...)
	return result
}

// ByCategory returns all servers in a given category.
func (idx *CuratedIndex) ByCategory(category string) []CuratedServer {
	var result []CuratedServer
	for _, s := range idx.Servers {
		if s.Category == category {
			result = append(result, s)
		}
	}
	return result
}

// Get returns a single server by ID.
func (idx *CuratedIndex) Get(id string) (*CuratedServer, bool) {
	for i := range idx.Servers {
		if idx.Servers[i].ID == id {
			return &idx.Servers[i], true
		}
	}
	return nil, false
}

func matchTags(tags []string, query string) bool {
	for _, tag := range tags {
		if strings.Contains(strings.ToLower(tag), query) {
			return true
		}
	}
	return false
}
