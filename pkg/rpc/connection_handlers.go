package rpc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/xoai/sageclaw/pkg/channel/discord"
	"github.com/xoai/sageclaw/pkg/channel/telegram"
	"github.com/xoai/sageclaw/pkg/store"
)

// handleConnectionsList returns all connections with optional filters.
func (s *Server) handleConnectionsList(w http.ResponseWriter, r *http.Request) {
	filter := store.ConnectionFilter{
		Platform: r.URL.Query().Get("platform"),
		Status:   r.URL.Query().Get("status"),
		AgentID:  r.URL.Query().Get("agent_id"),
	}

	conns, err := s.store.ListConnections(r.Context(), filter)
	if err != nil {
		writeJSON(w, []any{})
		return
	}

	var result []map[string]any
	for _, c := range conns {
		item := map[string]any{
			"id":         c.ID,
			"platform":   c.Platform,
			"agent_id":   c.AgentID,
			"label":      c.Label,
			"metadata":   json.RawMessage(c.Metadata),
			"status":     c.Status,
			"created_at": c.CreatedAt.Format("2006-01-02 15:04:05"),
			"updated_at": c.UpdatedAt.Format("2006-01-02 15:04:05"),
		}

		// If agent_id is set, look up agent name for display.
		if c.AgentID != "" {
			agentName := c.AgentID // Default to ID.
			var name string
			if err := s.store.DB().QueryRow(`SELECT name FROM agents WHERE id = ?`, c.AgentID).Scan(&name); err == nil && name != "" {
				agentName = name
			}
			item["agent_name"] = agentName
		}

		// Check if running.
		if s.chanMgr != nil {
			item["running"] = s.chanMgr.IsRunning(c.ID)
		}

		result = append(result, item)
	}
	if result == nil {
		result = []map[string]any{}
	}
	writeJSON(w, result)
}

// handleConnectionCreate creates a new connection.
// Request: { platform: "telegram", token: "bot123:ABC..." }
func (s *Server) handleConnectionCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Platform string `json:"platform"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Platform == "" || p.Token == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "platform and token required"})
		return
	}

	// Generate connection ID.
	connID := generateConnID(p.Platform)

	// Fetch metadata from platform API.
	metadata, label, err := fetchPlatformMetadata(r.Context(), p.Platform, connID, p.Token)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "failed to connect: " + err.Error()})
		return
	}

	// Store credential.
	credKey := "conn_" + connID + "_token"
	if err := s.store.StoreCredential(r.Context(), credKey, []byte(p.Token), s.encKey); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "storing credential: " + err.Error()})
		return
	}

	// Create connection record.
	metadataJSON, _ := json.Marshal(metadata)
	conn := store.Connection{
		ID:            connID,
		Platform:      p.Platform,
		Label:         label,
		Metadata:      string(metadataJSON),
		CredentialKey: credKey,
		Status:        "active",
	}
	if err := s.store.CreateConnection(r.Context(), conn); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "creating connection: " + err.Error()})
		return
	}

	// Start the channel adapter.
	connStatus := "active"
	if s.chanMgr != nil {
		cfg := map[string]string{"__conn_id": connID}
		switch p.Platform {
		case "telegram":
			cfg["TELEGRAM_BOT_TOKEN"] = p.Token
		case "discord":
			cfg["DISCORD_BOT_TOKEN"] = p.Token
		}
		if err := s.chanMgr.StartConnection(connID, p.Platform, cfg); err != nil {
			log.Printf("connection %s: adapter start failed: %v", connID, err)
			// Don't fail the creation — connection is saved, just not running.
			connStatus = "error"
			s.store.UpdateConnection(r.Context(), connID, map[string]any{"status": "error"})
		}
	}

	writeJSON(w, map[string]any{
		"id":       connID,
		"platform": p.Platform,
		"label":    label,
		"metadata": metadata,
		"status":   connStatus,
	})
}

// handleConnectionUpdate updates a connection (agent binding, status).
func (s *Server) handleConnectionUpdate(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v2/connections/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "connection ID required"})
		return
	}

	var p map[string]any
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	// Filter to allowed fields.
	fields := map[string]any{}
	if v, ok := p["agent_id"]; ok {
		if v == nil {
			fields["agent_id"] = ""
		} else {
			fields["agent_id"] = v
		}
	}
	if v, ok := p["status"]; ok {
		fields["status"] = v

		// Stop/start adapter based on status change.
		if s.chanMgr != nil {
			status, _ := v.(string)
			if status == "stopped" {
				s.chanMgr.StopConnection(id)
			} else if status == "active" {
				// Restart: load credential and start.
				s.restartConnection(r.Context(), id)
			}
		}
	}
	if v, ok := p["label"]; ok {
		fields["label"] = v
	}

	if err := s.store.UpdateConnection(r.Context(), id, fields); err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": id, "status": "updated"})
}

// handleConnectionDelete stops and removes a connection.
func (s *Server) handleConnectionDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/v2/connections/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "connection ID required"})
		return
	}

	// Get connection to find credential key.
	conn, err := s.store.GetConnection(r.Context(), id)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "connection not found"})
		return
	}

	// Stop adapter.
	if s.chanMgr != nil {
		s.chanMgr.StopConnection(id)
	}

	// Delete credential.
	s.store.DB().ExecContext(r.Context(), `DELETE FROM credentials WHERE key = ?`, conn.CredentialKey)

	// Delete connection record.
	if err := s.store.DeleteConnection(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

// restartConnection loads a connection's credential and starts its adapter.
func (s *Server) restartConnection(ctx context.Context, connID string) {
	conn, err := s.store.GetConnection(ctx, connID)
	if err != nil {
		log.Printf("restart %s: connection not found: %v", connID, err)
		return
	}

	token, err := s.store.GetCredential(ctx, conn.CredentialKey, s.encKey)
	if err != nil || len(token) == 0 {
		log.Printf("restart %s: credential not found", connID)
		return
	}

	cfg := map[string]string{"__conn_id": connID}
	switch conn.Platform {
	case "telegram":
		cfg["TELEGRAM_BOT_TOKEN"] = string(token)
	case "discord":
		cfg["DISCORD_BOT_TOKEN"] = string(token)
	}

	if err := s.chanMgr.StartConnection(connID, conn.Platform, cfg); err != nil {
		log.Printf("restart %s: start failed: %v", connID, err)
		s.store.UpdateConnection(ctx, connID, map[string]any{"status": "error"})
	}
}

// generateConnID creates a connection ID like "tg_a1b2c3d4".
func generateConnID(platform string) string {
	prefix := map[string]string{
		"telegram": "tg",
		"discord":  "dc",
		"zalo":     "zl",
		"whatsapp": "wa",
	}[platform]
	if prefix == "" {
		if len(platform) >= 2 {
			prefix = platform[:2]
		} else {
			prefix = platform
		}
	}

	b := make([]byte, 4)
	rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// fetchPlatformMetadata calls the platform API to get bot info.
// Returns metadata map, auto-generated label, and error.
func fetchPlatformMetadata(ctx context.Context, platform, connID, token string) (map[string]any, string, error) {
	switch platform {
	case "telegram":
		adapter := telegram.New(connID, token)
		user, err := adapter.GetMe(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("telegram getMe: %w", err)
		}
		metadata := map[string]any{
			"username":   user.Username,
			"first_name": user.FirstName,
			"id":         user.ID,
		}
		label := "@" + user.Username
		if label == "@" {
			label = user.FirstName
		}
		return metadata, label, nil

	case "discord":
		// Call Discord API to get bot info.
		adapter := discord.New(connID, token)
		user, err := adapter.GetMe(ctx)
		if err != nil {
			return nil, "", fmt.Errorf("discord getMe: %w", err)
		}
		metadata := map[string]any{
			"username":      user.Username,
			"discriminator": user.Discriminator,
			"id":            user.ID,
		}
		label := user.Username
		if label == "" {
			label = "Discord Bot"
		}
		return metadata, label, nil

	default:
		// Other platforms: minimal metadata.
		metadata := map[string]any{
			"platform": platform,
		}
		return metadata, platform + " connection", nil
	}
}
