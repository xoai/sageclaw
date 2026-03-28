package rpc

import (
	"context"
	"net/http"
)

func (s *Server) handleTunnelStatus(w http.ResponseWriter, r *http.Request) {
	result := map[string]any{}

	if s.tunnel != nil {
		status := s.tunnel.GetStatus()
		result["running"] = status.Running
		result["url"] = status.URL
		result["mode"] = status.Mode
		result["started_at"] = status.StartedAt
		result["error"] = status.Error
		result["two_fa"] = status.TwoFA
		result["latency_ms"] = status.Latency
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

	// Use a server-scoped context, not the request context —
	// the tunnel must outlive the HTTP request.
	if err := s.tunnel.Start(context.Background()); err != nil {
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
