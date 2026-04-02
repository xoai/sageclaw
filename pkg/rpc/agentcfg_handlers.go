package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/agentcfg"
	"gopkg.in/yaml.v3"
)

// handleAgentsListV2 returns all agents from file-based config.
func (s *Server) handleAgentsListV2(w http.ResponseWriter, r *http.Request) {
	agents, err := agentcfg.LoadAll(s.agentsDir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	var result []map[string]any
	for id, cfg := range agents {
		status := cfg.Identity.Status
		if status == "" {
			status = "active"
		}
		result = append(result, map[string]any{
			"id":       id,
			"name":     cfg.Identity.Name,
			"role":     cfg.Identity.Role,
			"model":    cfg.Identity.Model,
			"avatar":   cfg.Identity.Avatar,
			"tags":     cfg.Identity.Tags,
			"examples": agentcfg.ResolveExamples(cfg),
			"status":   status,
			"source":   cfg.Source,
			"has_soul": cfg.Soul != "",
			"has_behavior": cfg.Behavior != "",
			"has_bootstrap": cfg.Bootstrap != "",
			"tools_profile": cfg.Tools.Profile,
			"heartbeat_count": len(cfg.Heartbeat.Schedules),
			"channels_serve":  cfg.Channels.Serve,
			"channels_count":  len(cfg.Channels.Serve),
			"max_tokens":      cfg.Identity.MaxTokens,
		})
	}
	if result == nil {
		result = []map[string]any{}
	}
	writeJSON(w, result)
}

// validateAgentID checks for path traversal sequences in agent IDs.
func validateAgentID(id string) bool {
	return id != "" && !strings.Contains(id, "..") && !strings.Contains(id, "/") && !strings.Contains(id, "\\")
}

// handleAgentGetFull returns the complete agent config (all files).
// Returns soul/behavior/bootstrap as objects (parsed from YAML) so the
// frontend schema forms can populate fields.
func (s *Server) handleAgentGetFull(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v2/agents/")
	// Strip any trailing path segments (soul, behavior, etc.)
	if idx := strings.IndexByte(id, '/'); idx > 0 {
		id = id[:idx]
	}
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, id)
	cfg, err := agentcfg.LoadAgent(dir)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "agent not found: " + err.Error()})
		return
	}

	writeJSON(w, configToResponse(cfg))
}

// handleAgentExamples returns example prompts for a specific agent.
// Falls back to profile-based defaults if no custom examples are configured.
func (s *Server) handleAgentExamples(w http.ResponseWriter, r *http.Request) {
	// Path: /api/agents/{id}/examples
	id := extractPathParam(r.URL.Path, "/api/agents/")
	id = strings.TrimSuffix(id, "/examples")
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, id)
	cfg, err := agentcfg.LoadAgent(dir)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "agent not found"})
		return
	}

	writeJSON(w, map[string]any{
		"agent_id": id,
		"examples": agentcfg.ResolveExamples(cfg),
	})
}

// handleAgentQuickCreate creates an agent from a simplified config
// (as returned by the generate or preset endpoints).
// Accepts: { name, role, avatar, model, tool_profile, examples }
func (s *Server) handleAgentQuickCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name        string   `json:"name"`
		Role        string   `json:"role"`
		Avatar      string   `json:"avatar"`
		Model       string   `json:"model"`
		ToolProfile string   `json:"tool_profile"`
		Examples    []string `json:"examples"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "name required"})
		return
	}

	// Generate a filesystem-safe ID from the name.
	id := toAgentID(p.Name)
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "could not generate valid ID from name"})
		return
	}

	dir := filepath.Join(s.agentsDir, id)
	if _, err := os.Stat(filepath.Join(dir, "identity.yaml")); err == nil {
		// Append timestamp suffix to avoid collision.
		id = id + "-" + fmt.Sprintf("%d", time.Now().UnixMilli()%100000)
		dir = filepath.Join(s.agentsDir, id)
	}

	// Build config from simplified fields.
	cfg := agentcfg.Defaults(id)
	cfg.Identity.Name = p.Name
	cfg.Identity.Role = p.Role
	cfg.Identity.Avatar = p.Avatar
	if cfg.Identity.Avatar == "" {
		cfg.Identity.Avatar = agentcfg.AutoAvatar(p.Name, p.Role)
	}
	if p.Model != "" {
		cfg.Identity.Model = p.Model
	}
	if len(p.Examples) > 0 {
		cfg.Identity.Examples = p.Examples
	}
	if p.ToolProfile != "" {
		cfg.Tools.Profile = p.ToolProfile
	}

	if err := agentcfg.SaveAgent(&cfg, dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"id":     id,
		"name":   cfg.Identity.Name,
		"status": "created",
	})
}

// toAgentID converts a display name to a filesystem-safe agent ID.
func toAgentID(name string) string {
	id := strings.ToLower(name)
	id = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == ' ' || r == '_' {
			return '-'
		}
		return -1
	}, id)
	// Collapse multiple dashes.
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	id = strings.Trim(id, "-")
	if id == "" {
		id = "agent"
	}
	if len(id) > 50 {
		id = id[:50]
	}
	return id
}

// handleAgentCreateV2 creates a new agent from a full config.
// Accepts: { id: "name", config: { identity: {...}, soul: {...}, ... } }
func (s *Server) handleAgentCreateV2(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID     string         `json:"id"`
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if req.ID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id required"})
		return
	}
	if !validateAgentID(req.ID) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, req.ID)
	if _, err := os.Stat(filepath.Join(dir, "identity.yaml")); err == nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": "agent already exists"})
		return
	}

	cfg := configFromMap(req.ID, req.Config)

	if err := agentcfg.SaveAgent(cfg, dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": cfg.ID, "status": "created"})
}

// handleAgentUpdateV2 updates an agent's full config.
// Accepts: { config: { identity: {...}, soul: {...}, ... } }
func (s *Server) handleAgentUpdateV2(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v2/agents/")
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, id)
	if _, err := os.Stat(filepath.Join(dir, "identity.yaml")); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "agent not found"})
		return
	}

	var req struct {
		Config map[string]any `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	cfg := configFromMap(id, req.Config)

	if err := agentcfg.SaveAgent(cfg, dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Hot-reload: update the running agent loop with the new config.
	if s.loopPool != nil {
		runtimeCfg := agentcfg.ToRuntimeConfig(cfg)
		s.loopPool.UpdateConfig(id, runtimeCfg)
	}

	writeJSON(w, map[string]string{"id": id, "status": "updated"})
}

// handleAgentDeleteV2 deletes an agent's folder.
func (s *Server) handleAgentDeleteV2(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v2/agents/")
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, id)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "agent not found"})
		return
	}

	if err := os.RemoveAll(dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

// handleAgentGetFile returns a single file content (soul.md, behavior.md).
func (s *Server) handleAgentGetFile(w http.ResponseWriter, r *http.Request) {
	// Path: /api/v2/agents/{id}/{file}
	path := extractPathParam(r.URL.Path, "/api/v2/agents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid path"})
		return
	}

	id, fileName := parts[0], parts[1]
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}
	allowed := map[string]string{
		"soul":      "soul.md",
		"behavior":  "behavior.md",
		"bootstrap": "bootstrap.md",
		"tools":     "tools.yaml",
		"memory":    "memory.yaml",
		"heartbeat": "heartbeat.yaml",
		"channels":  "channels.yaml",
		"skills":    "skills.yaml",
		"voice":     "voice.yaml",
		"identity":  "identity.yaml",
	}

	diskFile, ok := allowed[fileName]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "unknown file: " + fileName})
		return
	}

	filePath := filepath.Join(s.agentsDir, id, diskFile)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]string{"content": ""})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"content": string(data)})
}

// handleAgentPutFile updates a single file content.
func (s *Server) handleAgentPutFile(w http.ResponseWriter, r *http.Request) {
	path := extractPathParam(r.URL.Path, "/api/v2/agents/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid path"})
		return
	}

	id, fileName := parts[0], parts[1]
	if !validateAgentID(id) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}
	allowed := map[string]string{
		"soul":      "soul.md",
		"behavior":  "behavior.md",
		"bootstrap": "bootstrap.md",
		"tools":     "tools.yaml",
		"memory":    "memory.yaml",
		"heartbeat": "heartbeat.yaml",
		"channels":  "channels.yaml",
		"skills":    "skills.yaml",
		"voice":     "voice.yaml",
		"identity":  "identity.yaml",
	}

	diskFile, ok := allowed[fileName]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "unknown file: " + fileName})
		return
	}

	var p struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	filePath := filepath.Join(s.agentsDir, id, diskFile)

	// Ensure agent dir exists.
	os.MkdirAll(filepath.Join(s.agentsDir, id), 0755)

	// Write atomically.
	tmp := filePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(p.Content), 0644); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	if err := os.Rename(tmp, filePath); err != nil {
		os.Remove(tmp)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"file": fileName, "status": "saved"})
}

// handleAgentTools returns available tools for the tool selector.
func (s *Server) handleAgentTools(w http.ResponseWriter, r *http.Request) {
	if s.toolRegistry == nil {
		writeJSON(w, []any{})
		return
	}

	defs := s.toolRegistry.List()
	var result []map[string]string
	for _, d := range defs {
		result = append(result, map[string]string{
			"name":        d.Name,
			"description": d.Description,
		})
	}
	writeJSON(w, result)
}

// configFromMap builds an AgentConfig from the frontend's config map.
// Soul, behavior, bootstrap are stored as YAML strings (the frontend sends objects).
func configFromMap(id string, m map[string]any) *agentcfg.AgentConfig {
	cfg := agentcfg.Defaults(id)

	// Identity: re-marshal through JSON to populate the typed struct.
	if raw, ok := m["identity"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Identity)
		}
	}

	// Soul, behavior, bootstrap: convert object to YAML string for file storage.
	cfg.Soul = mapToYAML(m["soul"])
	cfg.Behavior = mapToYAML(m["behavior"])
	cfg.Bootstrap = mapToYAML(m["bootstrap"])

	// Tools: re-marshal through JSON.
	if raw, ok := m["tools"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Tools)
		}
	}

	// Memory: re-marshal through JSON.
	if raw, ok := m["memory"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Memory)
		}
	}

	// Heartbeat: re-marshal through JSON.
	if raw, ok := m["heartbeat"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Heartbeat)
		}
	}

	// Channels: re-marshal through JSON.
	if raw, ok := m["channels"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Channels)
		}
	}

	// Skills: re-marshal through JSON.
	if raw, ok := m["skills"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Skills)
		}
	}

	// Voice: re-marshal through JSON.
	if raw, ok := m["voice"]; ok {
		if b, err := json.Marshal(raw); err == nil {
			json.Unmarshal(b, &cfg.Voice)
		}
	}

	// Apply defaults.
	if cfg.Identity.Name == "" {
		cfg.Identity.Name = id
	}
	if cfg.Identity.Model == "" {
		cfg.Identity.Model = "strong"
	}
	if cfg.Identity.MaxTokens == 0 {
		cfg.Identity.MaxTokens = 8192
	}
	if cfg.Identity.Avatar == "" {
		cfg.Identity.Avatar = agentcfg.AutoAvatar(cfg.Identity.Name, cfg.Identity.Role)
	}
	cfg.Source = "file"

	return &cfg
}

// mapToYAML converts a map/object to a YAML string. If already a string, returns as-is.
func mapToYAML(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case map[string]any:
		if len(val) == 0 {
			return ""
		}
		data, err := yaml.Marshal(val)
		if err != nil {
			return ""
		}
		return string(data)
	default:
		data, err := yaml.Marshal(v)
		if err != nil {
			return ""
		}
		return string(data)
	}
}

// configToResponse converts an AgentConfig to a response map.
// Soul, behavior, bootstrap YAML strings are parsed back to objects
// so the frontend schema forms can populate fields.
func configToResponse(cfg *agentcfg.AgentConfig) map[string]any {
	resp := map[string]any{
		"id":        cfg.ID,
		"source":    cfg.Source,
		"identity":  cfg.Identity,
		"tools":     cfg.Tools,
		"memory":    cfg.Memory,
		"heartbeat": cfg.Heartbeat,
		"channels":  cfg.Channels,
		"skills":    cfg.Skills,
		"voice":     cfg.Voice,
	}

	// Parse YAML strings back to objects for the frontend.
	resp["soul"] = yamlToMap(cfg.Soul)
	resp["behavior"] = yamlToMap(cfg.Behavior)
	resp["bootstrap"] = yamlToMap(cfg.Bootstrap)

	return resp
}

// yamlToMap parses a YAML string into a map. Returns empty map on failure.
func yamlToMap(s string) map[string]any {
	if s == "" {
		return map[string]any{}
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		return map[string]any{}
	}
	if m == nil {
		return map[string]any{}
	}
	return m
}
