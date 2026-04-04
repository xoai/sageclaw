package rpc

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

// --- C1: Cron Job Management ---

func (s *Server) handleCronList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, agent_id, schedule, prompt, COALESCE(last_run,''), COALESCE(next_run,''), enabled, created_at
		 FROM cron_jobs ORDER BY created_at DESC`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var jobs []map[string]any
	for rows.Next() {
		var id, agentID, schedule, prompt, lastRun, nextRun, createdAt string
		var enabled int
		rows.Scan(&id, &agentID, &schedule, &prompt, &lastRun, &nextRun, &enabled, &createdAt)
		jobs = append(jobs, map[string]any{
			"id": id, "agent_id": agentID, "schedule": schedule,
			"prompt": prompt, "last_run": lastRun, "next_run": nextRun,
			"enabled": enabled == 1, "created_at": createdAt,
		})
	}
	if jobs == nil {
		jobs = []map[string]any{}
	}
	writeJSON(w, jobs)
}

func (s *Server) handleCronCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		AgentID  string `json:"agent_id"`
		Schedule string `json:"schedule"`
		Prompt   string `json:"prompt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Schedule == "" || p.Prompt == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "schedule and prompt are required"})
		return
	}
	if p.AgentID == "" {
		p.AgentID = "default"
	}

	id, err := s.store.CreateCronJob(r.Context(), p.AgentID, p.Schedule, p.Prompt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

func (s *Server) handleCronDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/cron/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "cron job ID required"})
		return
	}
	if err := s.store.DeleteCronJob(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

func (s *Server) handleCronTrigger(w http.ResponseWriter, r *http.Request) {
	// Extract ID from /api/cron/{id}/trigger
	path := r.URL.Path
	path = strings.TrimPrefix(path, "/api/cron/")
	path = strings.TrimSuffix(path, "/trigger")
	id := path
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "cron job ID required"})
		return
	}

	// Update last_run to now to mark it as triggered.
	if err := s.store.UpdateCronLastRun(r.Context(), id, time.Now()); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "triggered"})
}

// --- C2: Delegation Management ---

func (s *Server) handleDelegationLinks(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT dl.id, dl.source_id, dl.target_id, dl.direction, dl.max_concurrent,
			COALESCE(ds.active_count, 0) as active_count
		 FROM delegation_links dl
		 LEFT JOIN delegation_state ds ON dl.id = ds.link_id
		 ORDER BY dl.source_id`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var links []map[string]any
	for rows.Next() {
		var id, sourceID, targetID, direction string
		var maxConcurrent, activeCount int
		rows.Scan(&id, &sourceID, &targetID, &direction, &maxConcurrent, &activeCount)
		links = append(links, map[string]any{
			"id": id, "source_id": sourceID, "target_id": targetID,
			"direction": direction, "max_concurrent": maxConcurrent,
			"active_count": activeCount,
		})
	}
	if links == nil {
		links = []map[string]any{}
	}
	writeJSON(w, links)
}

func (s *Server) handleDelegationLinksCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Source       string `json:"source"`
		Target       string `json:"target"`
		Direction    string `json:"direction"`
		MaxConcurrent int   `json:"max_concurrent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Source == "" || p.Target == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "source and target required"})
		return
	}
	if p.Direction == "" {
		p.Direction = "sync"
	}
	if p.MaxConcurrent == 0 {
		p.MaxConcurrent = 1
	}

	id := fmt.Sprintf("link_%s_%s", p.Source, p.Target)
	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT OR IGNORE INTO delegation_links (id, source_id, target_id, direction, max_concurrent) VALUES (?, ?, ?, ?, ?)`,
		id, p.Source, p.Target, p.Direction, p.MaxConcurrent)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	// Initialize state.
	s.store.DB().ExecContext(r.Context(),
		`INSERT OR IGNORE INTO delegation_state (link_id, active_count) VALUES (?, 0)`, id)

	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

func (s *Server) handleDelegationLinksDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/delegation/links/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "link ID required"})
		return
	}
	s.store.DB().ExecContext(r.Context(), `DELETE FROM delegation_state WHERE link_id = ?`, id)
	s.store.DB().ExecContext(r.Context(), `DELETE FROM delegation_links WHERE id = ?`, id)
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

func (s *Server) handleDelegationHistory(w http.ResponseWriter, r *http.Request) {
	limit := 50
	agentID := r.URL.Query().Get("agent_id")

	query := `SELECT id, source_id, target_id, prompt, COALESCE(result,''), status, started_at, COALESCE(completed_at,'')
		FROM delegation_history`
	var args []any
	if agentID != "" {
		query += ` WHERE source_id = ? OR target_id = ?`
		args = append(args, agentID, agentID)
	}
	query += ` ORDER BY started_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var history []map[string]any
	for rows.Next() {
		var id, sourceID, targetID, prompt, result, status, startedAt, completedAt string
		rows.Scan(&id, &sourceID, &targetID, &prompt, &result, &status, &startedAt, &completedAt)
		history = append(history, map[string]any{
			"id": id, "source_id": sourceID, "target_id": targetID,
			"prompt": prompt, "result": result, "status": status,
			"started_at": startedAt, "completed_at": completedAt,
		})
	}
	if history == nil {
		history = []map[string]any{}
	}
	writeJSON(w, history)
}

// --- C3: Memory Actions ---

func (s *Server) handleMemoryGet(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/memory/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "memory ID required"})
		return
	}
	mem, err := s.store.GetMemory(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "memory not found"})
		return
	}
	writeJSON(w, mem)
}

func (s *Server) handleMemoryUpdate(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/memory/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "memory ID required"})
		return
	}

	var p struct {
		Title   string   `json:"title"`
		Content string   `json:"content"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	tagsJSON, _ := json.Marshal(p.Tags)
	_, err := s.store.DB().ExecContext(r.Context(),
		`UPDATE memories SET title=?, content=?, tags=?, updated_at=datetime('now') WHERE id=?`,
		p.Title, p.Content, string(tagsJSON), id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Update FTS index.
	s.store.DB().ExecContext(r.Context(),
		`UPDATE memories_fts SET title=?, content=?, tags=? WHERE rowid=(SELECT rowid FROM memories WHERE id=?)`,
		p.Title, p.Content, strings.Join(p.Tags, " "), id)

	writeJSON(w, map[string]string{"id": id, "status": "updated"})
}

func (s *Server) handleMemoryDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/memory/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "memory ID required"})
		return
	}
	if err := s.memEngine.Delete(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

// --- C4: Knowledge Graph ---

func (s *Server) handleGraphGet(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/graph/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "start node ID required"})
		return
	}

	depth := 2
	direction := r.URL.Query().Get("direction")
	if direction == "" {
		direction = "both"
	}

	if s.graphEngine == nil {
		writeJSON(w, map[string]any{"nodes": []any{}, "edges": []any{}})
		return
	}

	nodes, edges, err := s.graphEngine.Graph(r.Context(), id, direction, depth)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"nodes": nodes, "edges": edges})
}

func (s *Server) handleGraphLink(w http.ResponseWriter, r *http.Request) {
	var p struct {
		SourceID string `json:"source_id"`
		TargetID string `json:"target_id"`
		Relation string `json:"relation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.SourceID == "" || p.TargetID == "" || p.Relation == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "source_id, target_id, and relation required"})
		return
	}

	if s.graphEngine == nil {
		w.WriteHeader(http.StatusNotImplemented)
		writeJSON(w, map[string]string{"error": "graph engine not available"})
		return
	}

	id, err := s.graphEngine.Link(r.Context(), p.SourceID, p.TargetID, p.Relation, nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "linked"})
}

func (s *Server) handleGraphUnlink(w http.ResponseWriter, r *http.Request) {
	var p struct {
		SourceID string `json:"source_id"`
		TargetID string `json:"target_id"`
		Relation string `json:"relation"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if s.graphEngine == nil {
		w.WriteHeader(http.StatusNotImplemented)
		writeJSON(w, map[string]string{"error": "graph engine not available"})
		return
	}

	if err := s.graphEngine.Unlink(r.Context(), p.SourceID, p.TargetID, p.Relation); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "unlinked"})
}

// --- C5: Audit Log ---

func (s *Server) handleAuditQuery(w http.ResponseWriter, r *http.Request) {
	agentID := r.URL.Query().Get("agent_id")
	tool := r.URL.Query().Get("tool")
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		fmt.Sscanf(limitStr, "%d", &limit)
	}

	query := `SELECT id, session_id, agent_id, event_type, payload, created_at FROM audit_log WHERE 1=1`
	var args []any
	if agentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, agentID)
	}
	if tool != "" {
		query += ` AND event_type = ?`
		args = append(args, "tool."+tool)
	}
	if from != "" {
		query += ` AND created_at >= ?`
		args = append(args, from)
	}
	if to != "" {
		query += ` AND created_at <= ?`
		args = append(args, to)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.store.DB().QueryContext(r.Context(), query, args...)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var entries []map[string]any
	for rows.Next() {
		var id int
		var sessionID, aID, eventType, payload, createdAt string
		rows.Scan(&id, &sessionID, &aID, &eventType, &payload, &createdAt)
		entries = append(entries, map[string]any{
			"id": id, "session_id": sessionID, "agent_id": aID,
			"event_type": eventType, "payload": payload, "created_at": createdAt,
		})
	}
	if entries == nil {
		entries = []map[string]any{}
	}
	writeJSON(w, entries)
}

func (s *Server) handleAuditStats(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT event_type, COUNT(*) as cnt FROM audit_log GROUP BY event_type ORDER BY cnt DESC`)
	if err != nil {
		writeJSON(w, map[string]any{})
		return
	}
	defer rows.Close()

	var stats []map[string]any
	total := 0
	errors := 0
	for rows.Next() {
		var eventType string
		var count int
		rows.Scan(&eventType, &count)
		stats = append(stats, map[string]any{"event_type": eventType, "count": count})
		total += count
		if strings.Contains(eventType, "error") || strings.Contains(eventType, "fail") {
			errors += count
		}
	}

	// Most used tool.
	var mostUsed string
	s.store.DB().QueryRow(`SELECT event_type FROM audit_log GROUP BY event_type ORDER BY COUNT(*) DESC LIMIT 1`).Scan(&mostUsed)

	// Unique tools.
	var uniqueTools int
	s.store.DB().QueryRow(`SELECT COUNT(DISTINCT event_type) FROM audit_log`).Scan(&uniqueTools)

	errorRate := 0.0
	if total > 0 {
		errorRate = float64(errors) / float64(total) * 100
	}

	writeJSON(w, map[string]any{
		"total":        total,
		"unique_tools": uniqueTools,
		"error_rate":   errorRate,
		"most_used":    mostUsed,
		"breakdown":    stats,
	})
}

// --- C6: Tool Registry Browser ---

func (s *Server) handleToolsList(w http.ResponseWriter, r *http.Request) {
	if s.toolRegistry == nil {
		writeJSON(w, []any{})
		return
	}

	defs := s.toolRegistry.List()
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })

	var tools []map[string]any
	for _, d := range defs {
		group, risk, source, _ := s.toolRegistry.GetMeta(d.Name)
		if group == "" {
			group = categorizeToolName(d.Name)
		}
		entry := map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"schema":      json.RawMessage(d.InputSchema),
			"category":    group,
		}
		if risk != "" {
			entry["risk"] = risk
		}
		if source != "" {
			entry["source"] = source
		}
		tools = append(tools, entry)
	}
	writeJSON(w, tools)
}

func categorizeToolName(name string) string {
	switch {
	case strings.HasPrefix(name, "fs_") || name == "read_file" || name == "write_file" || name == "list_dir":
		return "filesystem"
	case strings.HasPrefix(name, "exec") || name == "shell":
		return "execution"
	case strings.HasPrefix(name, "web_") || name == "web_search" || name == "web_fetch":
		return "web"
	case strings.HasPrefix(name, "memory_") || name == "memory_search" || name == "memory_store":
		return "memory"
	case strings.HasPrefix(name, "graph_") || name == "memory_link" || name == "memory_graph":
		return "graph"
	case strings.HasPrefix(name, "cron_"):
		return "cron"
	case strings.HasPrefix(name, "audit_"):
		return "audit"
	case name == "delegate" || name == "delegation_status":
		return "orchestration"
	case strings.HasPrefix(name, "team_"):
		return "orchestration"
	case name == "handoff":
		return "orchestration"
	case name == "evaluate":
		return "orchestration"
	case name == "spawn":
		return "orchestration"
	default:
		return "other"
	}
}

func (s *Server) handleToolProfiles(w http.ResponseWriter, r *http.Request) {
	profiles := tool.AllProfiles()
	result := make([]map[string]any, 0, len(profiles))
	for _, p := range profiles {
		groups := tool.ProfileGroups(p)
		groupNames := make([]string, 0, len(groups))
		for g := range groups {
			groupNames = append(groupNames, g)
		}
		sort.Strings(groupNames)
		result = append(result, map[string]any{
			"name":   p,
			"groups": groupNames,
		})
	}
	writeJSON(w, result)
}

func (s *Server) handleToolGroups(w http.ResponseWriter, r *http.Request) {
	if s.toolRegistry == nil {
		writeJSON(w, map[string]any{})
		return
	}

	// Build group → tools map from registry.
	defs := s.toolRegistry.List()
	groups := make(map[string][]map[string]any)
	for _, d := range defs {
		group, risk, _, _ := s.toolRegistry.GetMeta(d.Name)
		if group == "" {
			group = "other"
		}
		groups[group] = append(groups[group], map[string]any{
			"name":        d.Name,
			"description": d.Description,
			"risk":        risk,
		})
	}
	writeJSON(w, groups)
}

// --- C9: Credential Management ---

func (s *Server) handleCredentialsList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT key, created_at, updated_at FROM credentials ORDER BY key`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var creds []map[string]any
	for rows.Next() {
		var key, createdAt, updatedAt string
		rows.Scan(&key, &createdAt, &updatedAt)
		creds = append(creds, map[string]any{
			"name": key, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if creds == nil {
		creds = []map[string]any{}
	}
	writeJSON(w, creds)
}

func (s *Server) handleCredentialsStore(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Name == "" || p.Value == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "name and value required"})
		return
	}

	if err := s.store.StoreCredential(r.Context(), p.Name, []byte(p.Value), s.encKey); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"name": p.Name, "status": "stored"})
}

func (s *Server) handleCredentialsDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathParam(r.URL.Path, "/api/credentials/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "credential name required"})
		return
	}
	_, err := s.store.DB().ExecContext(r.Context(), `DELETE FROM credentials WHERE key = ?`, name)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"name": name, "status": "deleted"})
}

// --- C10: Skill Install/Uninstall ---

func (s *Server) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var p struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.URL == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "url required"})
		return
	}

	// Only allow https:// URLs to prevent git ext:: command injection.
	if !strings.HasPrefix(p.URL, "https://") {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "only https:// URLs are allowed"})
		return
	}

	skillsDir := os.Getenv("SAGECLAW_SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = "skills"
	}

	// Git clone into skills dir.
	cmd := exec.CommandContext(r.Context(), "git", "clone", "--depth=1", p.URL)
	cmd.Env = append(os.Environ(), "GIT_PROTOCOL_WHITELIST=https")
	cmd.Dir = skillsDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("git clone failed: %s", string(output))})
		return
	}
	writeJSON(w, map[string]string{"status": "installed", "output": string(output)})
}

func (s *Server) handleSkillDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathParam(r.URL.Path, "/api/skills/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "skill name required"})
		return
	}

	// Prevent path traversal.
	if strings.Contains(name, "..") || strings.Contains(name, "/") {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid skill name"})
		return
	}

	skillsDir := os.Getenv("SAGECLAW_SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = "skills"
	}

	skillPath := skillsDir + "/" + name
	if _, err := os.Stat(skillPath); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "skill not found"})
		return
	}

	if err := os.RemoveAll(skillPath); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"name": name, "status": "uninstalled"})
}

func (s *Server) handleSkillReload(w http.ResponseWriter, r *http.Request) {
	// Send SIGHUP to self for hot-reload.
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	proc.Signal(os.Signal(nil)) // No-op on Windows, works on Linux.
	writeJSON(w, map[string]string{"status": "reload requested"})
}

// --- C11: Template Management ---

func (s *Server) handleTemplatesList(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir("templates")
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	var templates []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		desc := ""
		// Try to read a description from the template.
		if data, err := os.ReadFile("templates/" + entry.Name() + "/description.txt"); err == nil {
			desc = strings.TrimSpace(string(data))
		}
		templates = append(templates, map[string]any{
			"name":        entry.Name(),
			"description": desc,
		})
	}
	if templates == nil {
		templates = []map[string]any{}
	}
	writeJSON(w, templates)
}

func (s *Server) handleTemplatesApply(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Template string `json:"template"`
		Dir      string `json:"dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Template == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "template name required"})
		return
	}
	if p.Dir == "" {
		p.Dir = "."
	}

	// Prevent path traversal and absolute path escape.
	if strings.Contains(p.Template, "..") || strings.Contains(p.Dir, "..") ||
		strings.HasPrefix(p.Dir, "/") || strings.HasPrefix(p.Dir, "\\") ||
		(len(p.Dir) >= 2 && p.Dir[1] == ':') {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid path"})
		return
	}

	srcDir := "templates/" + p.Template
	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "template not found"})
		return
	}

	// Copy template configs to target dir.
	entries, _ := os.ReadDir(srcDir)
	copied := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(srcDir + "/" + entry.Name())
		if err != nil {
			continue
		}
		os.MkdirAll(p.Dir+"/configs", 0755)
		if err := os.WriteFile(p.Dir+"/configs/"+entry.Name(), data, 0644); err == nil {
			copied++
		}
	}

	writeJSON(w, map[string]any{"status": "applied", "template": p.Template, "files_copied": copied})
}

// --- C12: Session Delete/Archive ---

func (s *Server) handleSessionDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/sessions/")
	// Strip /archive suffix if present.
	id = strings.TrimSuffix(id, "/archive")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "session ID required"})
		return
	}

	// Delete messages first (cascade).
	s.store.DB().ExecContext(r.Context(), `DELETE FROM messages WHERE session_id = ?`, id)
	result, err := s.store.DB().ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

func (s *Server) handleSessionArchive(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	// Extract ID from /api/sessions/{id}/archive
	path = strings.TrimPrefix(path, "/api/sessions/")
	path = strings.TrimSuffix(path, "/archive")
	id := path
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "session ID required"})
		return
	}

	_, err := s.store.DB().ExecContext(r.Context(),
		`UPDATE sessions SET metadata = json_set(COALESCE(metadata,'{}'), '$.archived', 1) WHERE id = ?`, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "archived"})
}

// --- C13: Health ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	var sessionCount, memoryCount, cronCount int
	s.store.DB().QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount)
	s.store.DB().QueryRow("SELECT COUNT(*) FROM memories").Scan(&memoryCount)
	s.store.DB().QueryRow("SELECT COUNT(*) FROM cron_jobs WHERE enabled = 1").Scan(&cronCount)

	health := map[string]any{
		"pipeline":        "running",
		"uptime_seconds":  time.Since(s.startTime).Seconds(),
		"sessions_active": sessionCount,
		"memories_count":  memoryCount,
		"cron_jobs":       cronCount,
		"providers":       s.safeProviderHealth(),
		"cache":           provider.GlobalCacheStats.Snapshot().WithCalculations(),
	}

	writeJSON(w, health)
}

// --- MCP Server Management ---

func (s *Server) handleMCPServersList(w http.ResponseWriter, r *http.Request) {
	if s.mcpMgr == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.mcpMgr.ListServers())
}

func (s *Server) handleMCPServersAdd(w http.ResponseWriter, r *http.Request) {
	if s.mcpMgr == nil {
		http.Error(w, "MCP manager not initialized", http.StatusServiceUnavailable)
		return
	}

	var req struct {
		Name   string               `json:"name"`
		Config mcp.MCPServerConfig  `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if err := s.mcpMgr.AddServer(req.Name, req.Config); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	writeJSON(w, map[string]string{"status": "connected", "name": req.Name})
}

func (s *Server) handleMCPServersRemove(w http.ResponseWriter, r *http.Request) {
	if s.mcpMgr == nil {
		http.Error(w, "MCP manager not initialized", http.StatusServiceUnavailable)
		return
	}

	name := strings.TrimPrefix(r.URL.Path, "/api/mcp/servers/")
	if name == "" {
		http.Error(w, "server name required", http.StatusBadRequest)
		return
	}

	if err := s.mcpMgr.RemoveServer(name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]string{"status": "removed", "name": name})
}

// --- Consent ---

func (s *Server) handleConsentResponse(w http.ResponseWriter, r *http.Request) {
	var req struct {
		// New nonce-based format.
		Nonce   string `json:"nonce"`
		Granted bool   `json:"granted"`
		Tier    string `json:"tier"` // "once", "always", "deny"

		// Legacy format (backward compat).
		Group string `json:"group"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// New nonce-based path.
	if req.Nonce != "" {
		if s.consentHandler == nil {
			http.Error(w, "consent handler not configured", http.StatusServiceUnavailable)
			return
		}
		if req.Tier == "" {
			req.Tier = "once"
		}
		if err := s.consentHandler(req.Nonce, req.Granted, req.Tier); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// Clear pending consent by nonce.
		s.mu.Lock()
		filtered := s.pendingConsent[:0]
		for _, c := range s.pendingConsent {
			if n, _ := c["nonce"].(string); n != req.Nonce {
				filtered = append(filtered, c)
			}
		}
		s.pendingConsent = filtered
		s.mu.Unlock()

		writeJSON(w, map[string]string{"status": "ok"})
		return
	}

	// Legacy group-based path (backward compat during M2→M6 transition).
	if req.Group == "" {
		http.Error(w, "nonce or group is required", http.StatusBadRequest)
		return
	}

	log.Printf("consent: legacy group-based request for %q (deprecated — use nonce format)", req.Group)

	// Try to resolve nonce from pending consents.
	var resolvedNonce string
	s.mu.RLock()
	for _, c := range s.pendingConsent {
		if g, _ := c["group"].(string); g == req.Group {
			if n, _ := c["nonce"].(string); n != "" {
				resolvedNonce = n
				break
			}
		}
	}
	s.mu.RUnlock()

	if resolvedNonce != "" && s.consentHandler != nil {
		if err := s.consentHandler(resolvedNonce, req.Granted, "once"); err != nil {
			log.Printf("consent: nonce resolution failed for legacy request: %v", err)
			http.Error(w, "consent nonce expired or already used", http.StatusGone)
			return
		}
	} else {
		http.Error(w, "consent handler not configured or no matching nonce", http.StatusServiceUnavailable)
		return
	}

	// Clear pending consent for this group.
	s.mu.Lock()
	filtered := s.pendingConsent[:0]
	for _, c := range s.pendingConsent {
		if g, _ := c["group"].(string); g != req.Group {
			filtered = append(filtered, c)
		}
	}
	s.pendingConsent = filtered
	s.mu.Unlock()

	writeJSON(w, map[string]string{"status": "ok"})
}

// handleConsentPending returns queued consent prompts for polling.
func (s *Server) handleConsentPending(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	pending := s.pendingConsent
	s.mu.RUnlock()
	if pending == nil {
		pending = []map[string]any{}
	}
	writeJSON(w, pending)
}

// handleConsentGrants returns persistent "always allow" grants.
func (s *Server) handleConsentGrants(w http.ResponseWriter, r *http.Request) {
	if s.consentStore == nil {
		writeJSON(w, []any{})
		return
	}

	ownerID := r.URL.Query().Get("owner_id")
	platform := r.URL.Query().Get("platform")

	grants, err := s.consentStore.ListGrants(ownerID, platform)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if grants == nil {
		grants = []tool.ConsentGrant{}
	}
	writeJSON(w, grants)
}

// handleConsentRevokeGrant revokes a persistent grant by ID.
func (s *Server) handleConsentRevokeGrant(w http.ResponseWriter, r *http.Request) {
	if s.consentStore == nil {
		http.Error(w, "consent store not configured", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/api/consent/grants/")
	if id == "" {
		http.Error(w, "grant ID is required", http.StatusBadRequest)
		return
	}

	if err := s.consentStore.RevokeByID(id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	writeJSON(w, map[string]string{"status": "revoked"})
}
