package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

)

// channelConfig defines what each channel needs.
var channelConfigs = map[string][]struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}{
	"telegram": {
		{Key: "TELEGRAM_BOT_TOKEN", Label: "Bot Token"},
	},
	"discord": {
		{Key: "DISCORD_BOT_TOKEN", Label: "Bot Token"},
	},
	"zalo": {
		{Key: "ZALO_OA_ID", Label: "OA ID"},
		{Key: "ZALO_OA_SECRET", Label: "OA Secret"},
	},
	"whatsapp": {
		{Key: "WHATSAPP_PHONE_NUMBER_ID", Label: "Phone Number ID"},
		{Key: "WHATSAPP_ACCESS_TOKEN", Label: "Access Token"},
		{Key: "WHATSAPP_VERIFY_TOKEN", Label: "Verify Token"},
	},
}

// --- Channels ---

func (s *Server) handleChannelsList(w http.ResponseWriter, r *http.Request) {
	channels := []map[string]any{}

	// CLI — always active.
	channels = append(channels, map[string]any{
		"name": "cli", "type": "cli", "status": "active",
		"description": "Interactive terminal chat",
		"configurable": false,
	})

	// Configurable channels.
	type channelDef struct {
		name string
		desc string
	}
	configurable := []channelDef{
		{"telegram", "Telegram Bot (long polling)"},
		{"discord", "Discord Bot"},
		{"zalo", "Zalo Official Account (webhook)"},
		{"whatsapp", "WhatsApp Business (Cloud API)"},
	}

	for _, ch := range configurable {
		configs := channelConfigs[ch.name]
		status := "not configured"
		configured := true
		fields := []map[string]any{}

		for _, c := range configs {
			val := os.Getenv(c.Key)
			hasVal := val != ""
			if !hasVal {
				// Also check credentials DB.
				if cred, err := s.store.GetCredential(r.Context(), c.Key, s.encKey); err == nil && len(cred) > 0 {
					hasVal = true
				}
			}
			if !hasVal {
				configured = false
			}
			fields = append(fields, map[string]any{
				"key": c.Key, "label": c.Label, "configured": hasVal,
			})
		}

		if configured {
			status = "active"
		}

		channels = append(channels, map[string]any{
			"name": ch.name, "type": ch.name, "status": status,
			"description": ch.desc, "configurable": true, "fields": fields,
		})
	}

	// MCP.
	channels = append(channels, map[string]any{
		"name": "mcp", "type": "mcp", "status": "available",
		"description": "MCP server (run with --mcp flag)",
		"configurable": false,
	})

	// Dashboard.
	channels = append(channels, map[string]any{
		"name": "dashboard", "type": "web", "status": "active",
		"description": "Web dashboard (this page)",
		"configurable": false,
	})

	writeJSON(w, channels)
}

func (s *Server) handleChannelConfigure(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Channel string            `json:"channel"`
		Vars    map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	// Validate channel type.
	configs, ok := channelConfigs[p.Channel]
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "unknown channel type"})
		return
	}

	// Store each credential.
	encKey := s.encKey
	stored := 0
	for _, c := range configs {
		val, hasVal := p.Vars[c.Key]
		if hasVal && val != "" {
			if err := s.store.StoreCredential(r.Context(), c.Key, []byte(val), encKey); err == nil {
				stored++
			}
		}
	}

	// Hot-reload: start the channel immediately.
	channelStarted := false
	if s.chanMgr != nil && stored > 0 {
		cfg := make(map[string]string)
		for _, c := range configs {
			val := os.Getenv(c.Key)
			if val == "" {
				if cred, err := s.store.GetCredential(r.Context(), c.Key, s.encKey); err == nil && len(cred) > 0 {
					val = string(cred)
				}
			}
			cfg[c.Key] = val
		}
		if err := s.chanMgr.StartChannel(p.Channel, cfg); err == nil {
			channelStarted = true
		}
	}

	writeJSON(w, map[string]any{"channel": p.Channel, "stored": stored, "status": "configured", "started": channelStarted})
}

// --- Teams ---

func (s *Server) handleTeamsList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, lead_id, config, COALESCE(description,''), COALESCE(settings,'{}') FROM teams ORDER BY name`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var teams []map[string]any
	for rows.Next() {
		var id, name, leadID, config, description, settings string
		rows.Scan(&id, &name, &leadID, &config, &description, &settings)

		// Count members from config.
		members := 0
		var cfg struct {
			Members []string `json:"members"`
		}
		if json.Unmarshal([]byte(config), &cfg) == nil {
			members = len(cfg.Members)
		}

		teams = append(teams, map[string]any{
			"id": id, "name": name, "lead": leadID,
			"config": config, "members": members,
			"description": description, "settings": settings,
		})
	}
	if teams == nil {
		teams = []map[string]any{}
	}
	writeJSON(w, teams)
}

func (s *Server) handleTeamsCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name        string   `json:"name"`
		LeadID      string   `json:"lead_id"`
		Members     []string `json:"members"`
		Description string   `json:"description"`
		Settings    string   `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Name == "" || p.LeadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "name and lead_id required"})
		return
	}

	id := fmt.Sprintf("team_%s", strings.ReplaceAll(strings.ToLower(p.Name), " ", "_"))
	config, _ := json.Marshal(map[string]any{"members": p.Members})
	if p.Settings == "" {
		p.Settings = "{}"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO teams (id, name, lead_id, config, description, settings, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, lead_id=excluded.lead_id, config=excluded.config, description=excluded.description, settings=excluded.settings, updated_at=excluded.updated_at`,
		id, p.Name, p.LeadID, string(config), p.Description, p.Settings, now, now)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

func (s *Server) handleTeamsUpdate(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/teams/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "team ID required"})
		return
	}

	var p struct {
		Name        string   `json:"name"`
		LeadID      string   `json:"lead_id"`
		Members     []string `json:"members"`
		Description string   `json:"description"`
		Settings    string   `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	config, _ := json.Marshal(map[string]any{"members": p.Members})
	if p.Settings == "" {
		p.Settings = "{}"
	}
	_, err := s.store.DB().ExecContext(r.Context(),
		`UPDATE teams SET name=?, lead_id=?, config=?, description=?, settings=?, updated_at=? WHERE id=?`,
		p.Name, p.LeadID, string(config), p.Description, p.Settings, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "updated"})
}

func (s *Server) handleTeamsDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/teams/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "team ID required"})
		return
	}

	// Delete team tasks first.
	s.store.DB().ExecContext(r.Context(), `DELETE FROM team_tasks WHERE team_id = ?`, id)
	s.store.DB().ExecContext(r.Context(), `DELETE FROM team_messages WHERE team_id = ?`, id)
	s.store.DB().ExecContext(r.Context(), `DELETE FROM teams WHERE id = ?`, id)
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

func (s *Server) handleTeamsTasks(w http.ResponseWriter, r *http.Request) {
	teamID := extractPathParam(r.URL.Path, "/api/teams/tasks/")
	if teamID == "" {
		writeJSON(w, []any{})
		return
	}

	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, team_id, title, COALESCE(description,''), status, COALESCE(assigned_to,''),
			created_by, COALESCE(result,''), COALESCE(blocked_by,''), COALESCE(parent_id,''),
			COALESCE(priority,0), COALESCE(task_number,0), COALESCE(identifier,''),
			COALESCE(progress_percent,0), COALESCE(require_approval,0),
			COALESCE(batch_id,''), COALESCE(owner_agent_id,''),
			COALESCE(retry_count,0), COALESCE(max_retries,0), COALESCE(error_message,''),
			created_at, updated_at
		 FROM team_tasks WHERE team_id = ? ORDER BY created_at DESC LIMIT 100`, teamID)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var tasks []map[string]any
	for rows.Next() {
		var (
			id, tid, title, desc, status, assigned, created, result        string
			blockedBy, parentID, identifier, batchID, ownerAgentID, errMsg string
			priority, taskNumber, progressPercent, retryCount, maxRetries   int
			requireApproval                                                 int
			createdAt, updatedAt                                            string
		)
		rows.Scan(&id, &tid, &title, &desc, &status, &assigned,
			&created, &result, &blockedBy, &parentID,
			&priority, &taskNumber, &identifier,
			&progressPercent, &requireApproval,
			&batchID, &ownerAgentID,
			&retryCount, &maxRetries, &errMsg,
			&createdAt, &updatedAt)
		tasks = append(tasks, map[string]any{
			"id": id, "team_id": tid, "title": title, "description": desc, "status": status,
			"assigned_to": assigned, "created_by": created, "result": result,
			"blocked_by": blockedBy, "parent_id": parentID,
			"priority": priority, "task_number": taskNumber, "identifier": identifier,
			"progress_percent": progressPercent, "require_approval": requireApproval == 1,
			"batch_id": batchID, "owner_agent_id": ownerAgentID,
			"retry_count": retryCount, "max_retries": maxRetries, "error_message": errMsg,
			"created_at": createdAt, "updated_at": updatedAt,
		})
	}
	if tasks == nil {
		tasks = []map[string]any{}
	}
	writeJSON(w, tasks)
}

func (s *Server) handleTeamsTaskAction(w http.ResponseWriter, r *http.Request) {
	// Extract team ID from path: /api/teams/tasks/{teamId}/action
	path := r.URL.Path
	parts := strings.Split(strings.TrimPrefix(path, "/api/teams/tasks/"), "/")
	if len(parts) < 2 || parts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "team ID required"})
		return
	}

	var p struct {
		TaskID   string `json:"task_id"`
		Action   string `json:"action"`
		Feedback string `json:"feedback"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.TaskID == "" || p.Action == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "task_id and action required"})
		return
	}

	ctx := r.Context()
	var err error

	switch p.Action {
	case "cancel":
		err = s.store.CancelTask(ctx, p.TaskID)
	case "approve":
		// Approve transitions in_review → completed via UpdateTask (bypasses CompleteTask's in_progress check).
		task, getErr := s.store.GetTask(ctx, p.TaskID)
		if getErr != nil || task == nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "task not found"})
			return
		}
		if task.Status != "in_review" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "task is not in_review"})
			return
		}
		err = s.store.UpdateTask(ctx, p.TaskID, map[string]any{
			"status":       "completed",
			"completed_at": time.Now().UTC().Format(time.RFC3339),
		})
	case "reject":
		// Reject re-queues the task as pending for rework.
		task, getErr := s.store.GetTask(ctx, p.TaskID)
		if getErr != nil || task == nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "task not found"})
			return
		}
		if task.Status != "in_review" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "task is not in_review"})
			return
		}
		err = s.store.UpdateTask(ctx, p.TaskID, map[string]any{
			"status":        "pending",
			"error_message": p.Feedback,
		})
	case "retry":
		err = s.store.RetryTask(ctx, p.TaskID)
	default:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "unknown action: " + p.Action})
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"task_id": p.TaskID, "action": p.Action, "status": "ok"})
}

// --- Task Resolution ---

func (s *Server) handleTaskResolve(w http.ResponseWriter, r *http.Request) {
	identifier := r.URL.Query().Get("id")
	if identifier == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id parameter required"})
		return
	}

	var teamID, taskID string
	err := s.store.DB().QueryRowContext(r.Context(),
		`SELECT team_id, id FROM team_tasks WHERE identifier = ? LIMIT 1`, identifier).Scan(&teamID, &taskID)
	if err != nil {
		writeJSON(w, map[string]any{"found": false})
		return
	}
	writeJSON(w, map[string]any{"found": true, "team_id": teamID, "task_id": taskID, "identifier": identifier})
}

// --- Attention Count ---

func (s *Server) handleTeamsAttentionCount(w http.ResponseWriter, r *http.Request) {
	var count int
	s.store.DB().QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM team_tasks WHERE status IN ('in_review','failed','blocked')`).Scan(&count)
	writeJSON(w, map[string]any{"count": count})
}

// --- Skills ---

func (s *Server) handleSkillsList(w http.ResponseWriter, r *http.Request) {
	skillsDir := os.Getenv("SAGECLAW_SKILLS_DIR")
	if skillsDir == "" {
		skillsDir = "skills"
	}

	seen := map[string]bool{}
	var skills []map[string]any

	// Scan skill directories — primary (CWD/skills) and executable-relative.
	dirs := []string{skillsDir}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		exeSkills := filepath.Join(exeDir, "skills")
		if exeSkills != skillsDir {
			dirs = append(dirs, exeSkills)
		}
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() || seen[entry.Name()] {
				continue
			}
			skillMd := filepath.Join(dir, entry.Name(), "SKILL.md")
			if _, err := os.Stat(skillMd); err != nil {
				continue
			}

			toolCount := 0
			toolsDir := filepath.Join(dir, entry.Name(), "tools")
			if toolEntries, err := os.ReadDir(toolsDir); err == nil {
				for _, te := range toolEntries {
					if len(te.Name()) > 5 && te.Name()[len(te.Name())-5:] == ".yaml" {
						toolCount++
					}
				}
			}

			skills = append(skills, map[string]any{
				"name":        entry.Name(),
				"tools":       toolCount,
				"has_skillmd": true,
			})
			seen[entry.Name()] = true
		}
	}

	if skills == nil {
		skills = []map[string]any{}
	}
	writeJSON(w, skills)
}

