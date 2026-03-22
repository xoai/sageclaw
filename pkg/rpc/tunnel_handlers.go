package rpc

import (
	"encoding/json"
	"net/http"

	"github.com/xoai/sageclaw/pkg/tunnel"
)

func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	installed, version, path := tunnel.Detect()

	result := map[string]any{
		"installed":    installed,
		"version":      version,
		"path":         path,
		"install_hint": tunnel.InstallHint(),
	}

	if s.tunnel != nil {
		status := s.tunnel.GetStatus()
		result["running"] = status.Running
		result["url"] = status.URL
		result["mode"] = status.Mode
		result["started_at"] = status.StartedAt
		result["error"] = status.Error
		result["webhooks"] = status.WebhookURLs()
	} else {
		result["running"] = false
	}

	writeJSON(w, result)
}

func (s *Server) handleTunnelStart(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "tunnel not initialized"})
		return
	}

	var p struct {
		Mode       string `json:"mode"` // "quick" (default) or "named"
		TunnelName string `json:"tunnel_name"`
	}
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&p)
	}
	if p.Mode == "" {
		p.Mode = "quick"
	}

	var err error
	if p.Mode == "named" && p.TunnelName != "" {
		err = s.tunnel.StartNamed(r.Context(), p.TunnelName)
	} else {
		err = s.tunnel.StartQuick(r.Context())
	}

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "starting"})
}

func (s *Server) handleTunnelStop(w http.ResponseWriter, r *http.Request) {
	if s.tunnel == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "tunnel not initialized"})
		return
	}

	if err := s.tunnel.Stop(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "stopped"})
}
