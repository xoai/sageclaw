package rpc

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/xoai/sageclaw/pkg/store"
)

// handleMCPMarketCategories returns categories with total and installed counts.
func (s *Server) handleMCPMarketCategories(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		writeJSON(w, []any{})
		return
	}

	idx := s.mcpRegistry.CuratedIndex()
	installedCounts, _ := s.store.CountMCPByCategory(r.Context())

	type categoryResp struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Icon        string `json:"icon"`
		Description string `json:"description"`
		Total       int    `json:"total"`
		Installed   int    `json:"installed"`
	}

	cats := make([]categoryResp, len(idx.Categories))
	for i, c := range idx.Categories {
		cats[i] = categoryResp{
			ID:          c.ID,
			Name:        c.Name,
			Icon:        c.Icon,
			Description: c.Description,
			Total:       len(idx.ByCategory(c.ID)),
			Installed:   installedCounts[c.ID],
		}
	}
	writeJSON(w, cats)
}

// handleMCPMarketList returns filtered MCP entries.
func (s *Server) handleMCPMarketList(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		writeJSON(w, []any{})
		return
	}

	q := r.URL.Query()
	filter := store.MCPFilter{
		Category: q.Get("category"),
		Query:    q.Get("q"),
	}
	if q.Get("installed") == "true" {
		t := true
		filter.Installed = &t
	}

	entries, err := s.store.ListMCPEntries(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response with proper JSON fields.
	result := make([]map[string]any, len(entries))
	for i, e := range entries {
		connType := "stdio"
		var conn struct {
			Type string `json:"type"`
		}
		json.Unmarshal([]byte(e.Connection), &conn)
		if conn.Type != "" {
			connType = conn.Type
		}

		result[i] = map[string]any{
			"id":              e.ID,
			"name":            e.Name,
			"description":     e.Description,
			"category":        e.Category,
			"connection":      json.RawMessage(e.Connection),
			"github_url":      e.GitHubURL,
			"stars":           e.Stars,
			"tags":            e.Tags,
			"source":          e.Source,
			"installed":       e.Installed,
			"enabled":         e.Enabled,
			"status":          e.Status,
			"status_error":    e.StatusError,
			"agent_ids":       nonNilSlice(e.AgentIDs),
			"connection_type": connType,
			"needs_config":    e.ConfigSchema != "" && e.ConfigSchema != "{}" && e.ConfigSchema != "null",
		}
	}
	writeJSON(w, result)
}

// handleMCPMarketDetail returns a single MCP entry with detail.
func (s *Server) handleMCPMarketDetail(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/mcp/marketplace/detail/")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	entry, err := s.store.GetMCPEntry(r.Context(), id)
	if err != nil {
		http.Error(w, "MCP not found", http.StatusNotFound)
		return
	}

	// Check if credentials exist (without revealing them).
	hasCredentials := false
	if entry.Installed && entry.ConfigSchema != "" && entry.ConfigSchema != "{}" {
		hasCredentials = true
	}

	writeJSON(w, map[string]any{
		"id":              entry.ID,
		"name":            entry.Name,
		"description":     entry.Description,
		"category":        entry.Category,
		"connection":      json.RawMessage(entry.Connection),
		"fallback_conn":   nullableJSON(entry.FallbackConn),
		"config_schema":   nullableJSON(entry.ConfigSchema),
		"github_url":      entry.GitHubURL,
		"stars":           entry.Stars,
		"tags":            entry.Tags,
		"source":          entry.Source,
		"installed":       entry.Installed,
		"enabled":         entry.Enabled,
		"status":          entry.Status,
		"status_error":    entry.StatusError,
		"agent_ids":       nonNilSlice(entry.AgentIDs),
		"has_credentials": hasCredentials,
	})
}

// handleMCPMarketInstall installs an MCP server.
// Returns immediately — connection happens in background.
func (s *Server) handleMCPMarketInstall(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID     string            `json:"id"`
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if req.Config == nil {
		req.Config = make(map[string]string)
	}

	if err := s.mcpRegistry.Install(r.Context(), req.ID, req.Config); err != nil {
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "not found") {
			status = http.StatusNotFound
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "already") {
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "installing", "id": req.ID})
}

// handleMCPMarketRetry retries a failed MCP installation.
func (s *Server) handleMCPMarketRetry(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpRegistry.Retry(r.Context(), req.ID); err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "installing", "id": req.ID})
}

// handleMCPMarketEnable enables a disabled MCP (async).
func (s *Server) handleMCPMarketEnable(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpRegistry.Enable(r.Context(), req.ID); err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "installing", "id": req.ID})
}

// handleMCPMarketDisable disables an MCP.
func (s *Server) handleMCPMarketDisable(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpRegistry.Disable(r.Context(), req.ID); err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "disabled", "id": req.ID})
}

// handleMCPMarketRemove uninstalls an MCP.
func (s *Server) handleMCPMarketRemove(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpRegistry.Remove(r.Context(), req.ID); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "removed", "id": req.ID})
}

// handleMCPMarketTest tests an MCP connection.
func (s *Server) handleMCPMarketTest(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID     string            `json:"id"`
		Config map[string]string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}
	if req.Config == nil {
		req.Config = make(map[string]string)
	}

	result, err := s.mcpRegistry.Test(r.Context(), req.ID, req.Config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, result)
}

// handleMCPMarketSearch searches the registry.
func (s *Server) handleMCPMarketSearch(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		writeJSON(w, []any{})
		return
	}

	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, []any{})
		return
	}

	entries, err := s.store.SearchMCPEntries(r.Context(), q, 20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, entries)
}

// handleMCPMarketAssign assigns an MCP to agents.
func (s *Server) handleMCPMarketAssign(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		ID       string   `json:"id"`
		AgentIDs []string `json:"agent_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		http.Error(w, "id is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpRegistry.AssignAgents(r.Context(), req.ID, req.AgentIDs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"status": "assigned", "id": req.ID, "agent_ids": req.AgentIDs})
}

// handleMCPMarketUpdate downloads a fresh MCP index and re-seeds the registry.
func (s *Server) handleMCPMarketUpdate(w http.ResponseWriter, r *http.Request) {
	if s.mcpRegistry == nil {
		http.Error(w, "registry not initialized", http.StatusServiceUnavailable)
		return
	}

	v, err := s.mcpRegistry.UpdateIndex(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"status":     "updated",
		"version":    v.Version,
		"servers":    v.Servers,
		"updated_at": v.UpdatedAt,
	})
}

// nullableJSON returns json.RawMessage or nil for empty/null strings.
func nullableJSON(s string) json.RawMessage {
	if s == "" || s == "{}" || s == "null" {
		return nil
	}
	return json.RawMessage(s)
}

// nonNilSlice returns an empty slice instead of nil for JSON serialization.
func nonNilSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
