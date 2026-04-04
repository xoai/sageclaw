package rpc

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/store"
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

	// Hot-reload team config into the runtime (agent loops, tools, etc.).
	if s.teamReloadFunc != nil {
		go s.teamReloadFunc()
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
	if s.teamReloadFunc != nil {
		go s.teamReloadFunc()
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

	// Delete team and related data in a transaction.
	tx, err := s.store.DB().BeginTx(r.Context(), nil)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to start transaction"})
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(r.Context(), `DELETE FROM team_tasks WHERE team_id = ?`, id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to delete team tasks"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM team_messages WHERE team_id = ?`, id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to delete team messages"})
		return
	}
	res, err := tx.ExecContext(r.Context(), `DELETE FROM teams WHERE id = ?`, id)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to delete team"})
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "team not found"})
		return
	}
	if err := tx.Commit(); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to commit delete"})
		return
	}
	if s.teamReloadFunc != nil {
		go s.teamReloadFunc()
	}
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

	teamID := parts[0]

	var p struct {
		TaskID      string `json:"task_id"`
		Action      string `json:"action"`
		Feedback    string `json:"feedback"`
		Title       string `json:"title"`
		Description string `json:"description"`
		Priority    int    `json:"priority"`
		AssignTo    string `json:"assign_to"`
		ParentID    string `json:"parent_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request body"})
		return
	}

	// Create action doesn't require task_id.
	if p.Action == "" || (p.TaskID == "" && p.Action != "create" && p.Action != "delete-bulk") {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "action required (task_id required for most actions)"})
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

	case "delete":
		err = s.store.DeleteTask(ctx, p.TaskID)
		if err != nil {
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "not found") {
				status = http.StatusNotFound
			} else if strings.Contains(err.Error(), "not in terminal state") {
				status = http.StatusBadRequest
			}
			w.WriteHeader(status)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		// Broadcast SSE event.
		s.emit("", "team.task.deleted", map[string]any{"type": "team.task.deleted", "task_id": p.TaskID, "team_id": teamID})
		writeJSON(w, map[string]string{"task_id": p.TaskID, "action": "delete", "status": "ok"})
		return

	case "delete-bulk":
		count, delErr := s.store.DeleteTerminalTasks(ctx, teamID)
		if delErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": delErr.Error()})
			return
		}
		// Broadcast SSE event.
		s.emit("", "team.task.cleared", map[string]any{"type": "team.task.cleared", "team_id": teamID, "count": count})
		writeJSON(w, map[string]any{"action": "delete-bulk", "deleted": count})
		return

	case "create":
		if strings.TrimSpace(p.Title) == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "title required"})
			return
		}
		if len(p.Title) > 500 {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "title must be 500 characters or less"})
			return
		}
		if p.Priority < 0 || p.Priority > 3 {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "priority must be 0-3"})
			return
		}
		// Validate assign_to is a team member.
		if p.AssignTo != "" {
			members, memErr := s.store.ListTeamMembers(ctx, teamID)
			if memErr != nil {
				w.WriteHeader(http.StatusInternalServerError)
				writeJSON(w, map[string]string{"error": "failed to list team members"})
				return
			}
			found := false
			for _, m := range members {
				if m.AgentID == p.AssignTo {
					found = true
					break
				}
			}
			if !found {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "assign_to is not a team member"})
				return
			}
		}
		// Validate parent_id.
		if p.ParentID != "" {
			parent, pErr := s.store.GetTask(ctx, p.ParentID)
			if pErr != nil || parent == nil {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "parent task not found"})
				return
			}
			if parent.TeamID != teamID {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "parent task belongs to different team"})
				return
			}
			if parent.Status == "completed" || parent.Status == "cancelled" || parent.Status == "failed" {
				w.WriteHeader(http.StatusBadRequest)
				writeJSON(w, map[string]string{"error": "parent task is in terminal state"})
				return
			}
		}

		taskNum, numErr := s.store.NextTaskNumber(ctx, teamID)
		if numErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "failed to get task number"})
			return
		}

		task := store.TeamTask{
			TeamID:      teamID,
			Title:       strings.TrimSpace(p.Title),
			Description: p.Description,
			Priority:    p.Priority,
			CreatedBy:   "dashboard",
			ParentID:    p.ParentID,
			TaskNumber:  taskNum,
			Identifier:  fmt.Sprintf("TSK-%d", taskNum),
		}

		taskID, createErr := s.store.CreateTask(ctx, task)
		if createErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": createErr.Error()})
			return
		}

		// Increment parent subtask_count if applicable.
		if p.ParentID != "" {
			_ = s.store.IncrementSubtaskCount(ctx, p.ParentID)
		}

		// Assign if requested.
		if p.AssignTo != "" {
			_ = s.store.ClaimTask(ctx, taskID, p.AssignTo)
		}

		// Read back full task.
		created, _ := s.store.GetTask(ctx, taskID)
		if created != nil {
			// SSE: team.task.created is broadcast by existing infrastructure if wired.
			s.emit("", "team.task.created", map[string]any{"type": "team.task.created", "task_id": taskID, "team_id": teamID, "task": created, "seq": created.UpdatedAt.UnixMilli()})
			writeJSON(w, created)
		} else {
			writeJSON(w, map[string]string{"id": taskID, "status": "created"})
		}
		return

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

// --- Task Comments (Dashboard) ---

func (s *Server) handleTeamsTaskComments(w http.ResponseWriter, r *http.Request) {
	// Path: /api/teams/task-comments/{taskId}
	taskID := extractPathParam(r.URL.Path, "/api/teams/task-comments/")
	if taskID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "task ID required"})
		return
	}

	ctx := r.Context()

	switch r.Method {
	case http.MethodGet:
		comments, err := s.store.ListComments(ctx, taskID)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		if comments == nil {
			comments = []store.TeamTaskComment{}
		}
		writeJSON(w, comments)

	case http.MethodPost:
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Text) == "" {
			w.WriteHeader(http.StatusBadRequest)
			writeJSON(w, map[string]string{"error": "text required"})
			return
		}

		// Verify task exists.
		task, err := s.store.GetTask(ctx, taskID)
		if err != nil || task == nil {
			w.WriteHeader(http.StatusNotFound)
			writeJSON(w, map[string]string{"error": "task not found"})
			return
		}

		id, err := s.store.CreateComment(ctx, store.TeamTaskComment{
			TaskID:      taskID,
			UserID:      "dashboard",
			Content:     strings.TrimSpace(body.Text),
			CommentType: "note",
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, map[string]string{"id": id, "status": "created"})

	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
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

