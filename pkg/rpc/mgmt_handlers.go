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

func (s *Server) handleGetUtilityModel(w http.ResponseWriter, r *http.Request) {
	val, _ := s.store.GetSetting(r.Context(), "utility_model")
	if val == "" {
		val = "auto"
	}
	writeJSON(w, map[string]string{"model": val})
}

func (s *Server) handleSetUtilityModel(w http.ResponseWriter, r *http.Request) {
	var params struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if err := s.store.SetSetting(r.Context(), "utility_model", params.Model); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

func (s *Server) handleGetMechanismModels(w http.ResponseWriter, r *http.Request) {
	result := map[string]string{}
	for _, mech := range []string{"snip", "compact", "review"} {
		val, _ := s.store.GetSetting(r.Context(), "mechanism_model_"+mech)
		if val == "" {
			val = "auto"
		}
		result[mech] = val
	}
	writeJSON(w, result)
}

func (s *Server) handleSetMechanismModels(w http.ResponseWriter, r *http.Request) {
	var params map[string]string
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	for _, mech := range []string{"snip", "compact", "review"} {
		if val, ok := params[mech]; ok {
			if err := s.store.SetSetting(r.Context(), "mechanism_model_"+mech, val); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": err.Error()})
				return
			}
		}
	}
	writeJSON(w, map[string]string{"status": "saved"})
}

func (s *Server) handleSettingsExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := s.store.DB()

	export := map[string]any{
		"version": "0.5.0",
	}

	// --- Agents (full config) ---
	if rows, err := db.QueryContext(ctx,
		`SELECT id, name, COALESCE(system_prompt,''), model, max_tokens, tools, COALESCE(config,'{}') FROM agents`); err == nil {
		defer rows.Close()
		agents := map[string]any{}
		for rows.Next() {
			var id, name, prompt, model, tools, config string
			var maxTokens int
			rows.Scan(&id, &name, &prompt, &model, &maxTokens, &tools, &config)
			agents[id] = map[string]any{
				"name": name, "system_prompt": prompt,
				"model": model, "max_tokens": maxTokens,
				"tools": tools, "config": config,
			}
		}
		export["agents"] = agents
	}

	// --- Providers (type, name, base_url, config — API keys excluded) ---
	if rows, err := db.QueryContext(ctx,
		`SELECT id, type, name, base_url, COALESCE(config,'{}'), status FROM providers`); err == nil {
		defer rows.Close()
		providers := map[string]any{}
		for rows.Next() {
			var id, ptype, name, baseURL, config, status string
			rows.Scan(&id, &ptype, &name, &baseURL, &config, &status)
			providers[id] = map[string]any{
				"type": ptype, "name": name, "base_url": baseURL,
				"config": config, "status": status,
			}
		}
		export["providers"] = providers
	}

	// --- Combos (user-created only) ---
	if rows, err := db.QueryContext(ctx,
		`SELECT id, name, description, strategy, models FROM combos WHERE is_preset = 0`); err == nil {
		defer rows.Close()
		combos := map[string]any{}
		for rows.Next() {
			var id, name, desc, strategy, models string
			rows.Scan(&id, &name, &desc, &strategy, &models)
			combos[id] = map[string]any{
				"name": name, "description": desc,
				"strategy": strategy, "models": models,
			}
		}
		export["combos"] = combos
	}

	// --- Settings (all key-value pairs) ---
	if rows, err := db.QueryContext(ctx,
		`SELECT key, value FROM settings`); err == nil {
		defer rows.Close()
		settings := map[string]string{}
		for rows.Next() {
			var key, value string
			rows.Scan(&key, &value)
			// Skip password hash — security sensitive.
			if key == "password_hash" || key == "totp_secret" {
				continue
			}
			settings[key] = value
		}
		export["settings"] = settings
	}

	// --- Cron jobs ---
	if rows, err := db.QueryContext(ctx,
		`SELECT id, agent_id, schedule, prompt, COALESCE(session_id,''), enabled FROM cron_jobs`); err == nil {
		defer rows.Close()
		crons := map[string]any{}
		for rows.Next() {
			var id, agentID, schedule, prompt, sessionID string
			var enabled int
			rows.Scan(&id, &agentID, &schedule, &prompt, &sessionID, &enabled)
			crons[id] = map[string]any{
				"agent_id": agentID, "schedule": schedule,
				"prompt": prompt, "session_id": sessionID,
				"enabled": enabled == 1,
			}
		}
		export["cron_jobs"] = crons
	}

	// --- MCP servers (installed custom ones) ---
	if rows, err := db.QueryContext(ctx,
		`SELECT id, name, COALESCE(description,''), COALESCE(category,''), connection,
		 COALESCE(fallback_conn,''), source, installed, enabled, COALESCE(agent_ids,'[]')
		 FROM mcp_registry WHERE installed = 1`); err == nil {
		defer rows.Close()
		mcps := map[string]any{}
		for rows.Next() {
			var id, name, desc, category, conn, fallback, source, agentIDs string
			var installed, enabled int
			rows.Scan(&id, &name, &desc, &category, &conn, &fallback, &source, &installed, &enabled, &agentIDs)
			mcps[id] = map[string]any{
				"name": name, "description": desc, "category": category,
				"connection": conn, "fallback_conn": fallback,
				"source": source, "enabled": enabled == 1,
				"agent_ids": agentIDs,
			}
		}
		export["mcp_servers"] = mcps
	}

	// --- Installed skills (from skill store) ---
	if s.skillStore != nil {
		installed := s.skillStore.Installed()
		// Enrich with agent assignments.
		if s.agentsDir != "" {
			assignments := loadAllAgentSkillAssignments(s.agentsDir)
			for i, sk := range installed {
				for agentID, agentSkills := range assignments {
					for _, name := range agentSkills {
						if name == sk.Name {
							installed[i].Agents = append(installed[i].Agents, agentID)
						}
					}
				}
			}
		}
		skills := make([]map[string]any, 0, len(installed))
		for _, sk := range installed {
			skills = append(skills, map[string]any{
				"name":        sk.Name,
				"source":      sk.Source,
				"source_type": sk.SourceType,
				"agents":      sk.Agents,
			})
		}
		export["skills"] = skills
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=sageclaw-config.json")
	json.NewEncoder(w).Encode(export)
}

func (s *Server) handleSettingsImport(w http.ResponseWriter, r *http.Request) {
	var importData struct {
		Version string `json:"version"`
		Agents  map[string]struct {
			Name         string `json:"name"`
			SystemPrompt string `json:"system_prompt"`
			Model        string `json:"model"`
			MaxTokens    int    `json:"max_tokens"`
			Tools        string `json:"tools"`
			Config       string `json:"config"`
		} `json:"agents"`
		Providers map[string]struct {
			Type    string `json:"type"`
			Name    string `json:"name"`
			BaseURL string `json:"base_url"`
			Config  string `json:"config"`
			Status  string `json:"status"`
		} `json:"providers"`
		Combos map[string]struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Strategy    string `json:"strategy"`
			Models      string `json:"models"`
		} `json:"combos"`
		Settings map[string]string `json:"settings"`
		CronJobs map[string]struct {
			AgentID   string `json:"agent_id"`
			Schedule  string `json:"schedule"`
			Prompt    string `json:"prompt"`
			SessionID string `json:"session_id"`
			Enabled   bool   `json:"enabled"`
		} `json:"cron_jobs"`
		MCPServers map[string]struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			Category     string `json:"category"`
			Connection   string `json:"connection"`
			FallbackConn string `json:"fallback_conn"`
			Source       string `json:"source"`
			Enabled      bool   `json:"enabled"`
			AgentIDs     string `json:"agent_ids"`
		} `json:"mcp_servers"`
		Skills []struct {
			Name       string   `json:"name"`
			Source     string   `json:"source"`
			SourceType string   `json:"source_type"`
			Agents     []string `json:"agents"`
		} `json:"skills"`
	}

	if err := json.NewDecoder(r.Body).Decode(&importData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid JSON"})
		return
	}

	ctx := r.Context()
	db := s.store.DB()
	counts := map[string]int{}

	// --- Import agents ---
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
		config := agent.Config
		if config == "" {
			config = "{}"
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO agents (id, name, system_prompt, model, max_tokens, tools, config) VALUES (?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, system_prompt=excluded.system_prompt,
			 model=excluded.model, max_tokens=excluded.max_tokens, tools=excluded.tools, config=excluded.config, updated_at=datetime('now')`,
			id, agent.Name, agent.SystemPrompt, model, maxTokens, tools, config)
		if err == nil {
			counts["agents"]++
		}
	}

	// --- Import providers (without API keys — user must re-enter) ---
	for id, p := range importData.Providers {
		config := p.Config
		if config == "" {
			config = "{}"
		}
		status := p.Status
		if status == "" {
			status = "inactive"
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO providers (id, type, name, base_url, config, status) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, base_url=excluded.base_url,
			 config=excluded.config, updated_at=datetime('now')`,
			id, p.Type, p.Name, p.BaseURL, config, status)
		if err == nil {
			counts["providers"]++
		}
	}

	// --- Import combos ---
	for id, c := range importData.Combos {
		models := c.Models
		if models == "" {
			models = "[]"
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO combos (id, name, description, strategy, models, is_preset) VALUES (?, ?, ?, ?, ?, 0)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description,
			 strategy=excluded.strategy, models=excluded.models`,
			id, c.Name, c.Description, c.Strategy, models)
		if err == nil {
			counts["combos"]++
		}
	}

	// --- Import settings ---
	for key, value := range importData.Settings {
		// Skip security-sensitive keys.
		if key == "password_hash" || key == "totp_secret" {
			continue
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=datetime('now')`,
			key, value)
		if err == nil {
			counts["settings"]++
		}
	}

	// --- Import cron jobs ---
	for id, cj := range importData.CronJobs {
		enabled := 0
		if cj.Enabled {
			enabled = 1
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO cron_jobs (id, agent_id, schedule, prompt, session_id, enabled) VALUES (?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET schedule=excluded.schedule, prompt=excluded.prompt, enabled=excluded.enabled`,
			id, cj.AgentID, cj.Schedule, cj.Prompt, cj.SessionID, enabled)
		if err == nil {
			counts["cron_jobs"]++
		}
	}

	// --- Import MCP servers ---
	for id, m := range importData.MCPServers {
		enabled := 0
		if m.Enabled {
			enabled = 1
		}
		source := m.Source
		if source == "" {
			source = "custom"
		}
		agentIDs := m.AgentIDs
		if agentIDs == "" {
			agentIDs = "[]"
		}
		_, err := db.ExecContext(ctx,
			`INSERT INTO mcp_registry (id, name, description, category, connection, fallback_conn, source, installed, enabled, agent_ids)
			 VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET name=excluded.name, connection=excluded.connection,
			 enabled=excluded.enabled, agent_ids=excluded.agent_ids`,
			id, m.Name, m.Description, m.Category, m.Connection, m.FallbackConn, source, enabled, agentIDs)
		if err == nil {
			counts["mcp_servers"]++
		}
	}

	// --- Skills (record URLs for user to re-install) ---
	// Skills are filesystem-based — we can't inject them via DB.
	// Return the list so the frontend can prompt re-installation.
	counts["skills_to_install"] = len(importData.Skills)

	writeJSON(w, map[string]any{"status": "imported", "counts": counts})
}
