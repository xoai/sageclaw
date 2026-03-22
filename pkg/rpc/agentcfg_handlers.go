package rpc

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/xoai/sageclaw/pkg/agentcfg"
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
			"status":   status,
			"source":   cfg.Source,
			"has_soul": cfg.Soul != "",
			"has_behavior": cfg.Behavior != "",
			"has_bootstrap": cfg.Bootstrap != "",
			"tools_count":  len(cfg.Tools.Enabled),
			"heartbeat_count": len(cfg.Heartbeat.Schedules),
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

	writeJSON(w, cfg)
}

// handleAgentCreateV2 creates a new agent from a full config.
func (s *Server) handleAgentCreateV2(w http.ResponseWriter, r *http.Request) {
	var cfg agentcfg.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if cfg.ID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id required"})
		return
	}

	// Prevent path traversal.
	if !validateAgentID(cfg.ID) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid agent ID"})
		return
	}

	dir := filepath.Join(s.agentsDir, cfg.ID)

	// Check if already exists.
	if _, err := os.Stat(filepath.Join(dir, "identity.yaml")); err == nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": "agent already exists"})
		return
	}

	// Apply defaults for missing fields.
	if cfg.Identity.Name == "" {
		cfg.Identity.Name = cfg.ID
	}
	if cfg.Identity.Model == "" {
		cfg.Identity.Model = "strong"
	}
	if cfg.Identity.MaxTokens == 0 {
		cfg.Identity.MaxTokens = 8192
	}
	cfg.Source = "file"

	if err := agentcfg.SaveAgent(&cfg, dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": cfg.ID, "status": "created"})
}

// handleAgentUpdateV2 updates an agent's full config.
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

	var cfg agentcfg.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	cfg.ID = id
	cfg.Source = "file"

	if err := agentcfg.SaveAgent(&cfg, dir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
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
