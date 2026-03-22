package rpc

import (
	"encoding/json"
	"net/http"
)

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	// Count sessions, memories, agents.
	var sessionCount, memoryCount, agentCount int
	s.store.DB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	s.store.DB().QueryRow("SELECT COUNT(*) FROM memories").Scan(&memoryCount)
	s.store.DB().QueryRow("SELECT COUNT(*) FROM agents").Scan(&agentCount)

	writeJSON(w, map[string]any{
		"version":  "0.4.0-dev",
		"sessions": sessionCount,
		"memories": memoryCount,
		"agents":   agentCount,
		"auth":     s.auth != nil && s.auth.IsSetup(),
	})
}

func (s *Server) handleAgentsList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, COALESCE(system_prompt,''), model, max_tokens, tools, config FROM agents ORDER BY id`)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	var agents []map[string]any
	for rows.Next() {
		var id, name, prompt, model, tools, config string
		var maxTokens int
		rows.Scan(&id, &name, &prompt, &model, &maxTokens, &tools, &config)
		agents = append(agents, map[string]any{
			"id": id, "name": name, "system_prompt": prompt,
			"model": model, "max_tokens": maxTokens,
			"tools": tools, "config": config,
		})
	}
	if agents == nil {
		agents = []map[string]any{}
	}
	writeJSON(w, agents)
}

func (s *Server) handleAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var params struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
		MaxTokens    int    `json:"max_tokens"`
		Tools        string `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if params.ID == "" || params.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id and name are required"})
		return
	}
	if params.MaxTokens == 0 {
		params.MaxTokens = 8192
	}
	if params.Model == "" {
		params.Model = "strong"
	}
	if params.Tools == "" {
		params.Tools = "[]"
	}

	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO agents (id, name, system_prompt, model, max_tokens, tools) VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, system_prompt=excluded.system_prompt,
		 model=excluded.model, max_tokens=excluded.max_tokens, tools=excluded.tools, updated_at=datetime('now')`,
		params.ID, params.Name, params.SystemPrompt, params.Model, params.MaxTokens, params.Tools)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": params.ID, "status": "ok"})
}

func (s *Server) handleAgentsUpdate(w http.ResponseWriter, r *http.Request) {
	// Extract ID from URL path: /api/agents/{id}
	id := extractPathParam(r.URL.Path, "/api/agents/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "agent ID required"})
		return
	}

	var params struct {
		Name         string `json:"name"`
		SystemPrompt string `json:"system_prompt"`
		Model        string `json:"model"`
		MaxTokens    int    `json:"max_tokens"`
		Tools        string `json:"tools"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if params.MaxTokens == 0 {
		params.MaxTokens = 8192
	}
	if params.Tools == "" {
		params.Tools = "[]"
	}

	_, err := s.store.DB().ExecContext(r.Context(),
		`UPDATE agents SET name=?, system_prompt=?, model=?, max_tokens=?, tools=?, updated_at=datetime('now') WHERE id=?`,
		params.Name, params.SystemPrompt, params.Model, params.MaxTokens, params.Tools, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "updated"})
}

func (s *Server) handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/agents/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "agent ID required"})
		return
	}

	result, err := s.store.DB().ExecContext(r.Context(), `DELETE FROM agents WHERE id = ?`, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "agent not found"})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

func extractPathParam(path, prefix string) string {
	if len(path) <= len(prefix) {
		return ""
	}
	return path[len(prefix):]
}

func (s *Server) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "auth not configured"})
		return
	}

	var params struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if err := s.auth.ChangePassword(params.OldPassword, params.NewPassword); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "password changed"})
}

func (s *Server) handleSettingsExport(w http.ResponseWriter, r *http.Request) {
	// Export agents as JSON.
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, COALESCE(system_prompt,''), model, max_tokens, tools FROM agents`)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	agents := map[string]any{}
	for rows.Next() {
		var id, name, prompt, model, tools string
		var maxTokens int
		rows.Scan(&id, &name, &prompt, &model, &maxTokens, &tools)
		agents[id] = map[string]any{
			"name": name, "system_prompt": prompt,
			"model": model, "max_tokens": maxTokens, "tools": tools,
		}
	}

	export := map[string]any{
		"version": "0.4.0",
		"agents":  agents,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=sageclaw-config.json")
	json.NewEncoder(w).Encode(export)
}

func (s *Server) handleSettingsImport(w http.ResponseWriter, r *http.Request) {
	var importData struct {
		Agents map[string]struct {
			Name         string `json:"name"`
			SystemPrompt string `json:"system_prompt"`
			Model        string `json:"model"`
			MaxTokens    int    `json:"max_tokens"`
			Tools        string `json:"tools"`
		} `json:"agents"`
	}

	if err := json.NewDecoder(r.Body).Decode(&importData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid JSON"})
		return
	}

	imported := 0
	for id, agent := range importData.Agents {
		maxTokens := agent.MaxTokens
		if maxTokens == 0 {
			maxTokens = 8192
		}
		model := agent.Model
		if model == "" {
			model = "strong"
		}
		tools := agent.Tools
		if tools == "" {
			tools = "[]"
		}

		_, err := s.store.DB().ExecContext(r.Context(),
			`INSERT INTO agents (id, name, system_prompt, model, max_tokens, tools) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, system_prompt=excluded.system_prompt,
			 model=excluded.model, max_tokens=excluded.max_tokens, tools=excluded.tools, updated_at=datetime('now')`,
			id, agent.Name, agent.SystemPrompt, model, maxTokens, tools)
		if err == nil {
			imported++
		}
	}

	writeJSON(w, map[string]any{"status": "imported", "agents": imported})
}
