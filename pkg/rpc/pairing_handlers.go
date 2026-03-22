package rpc

import (
	"encoding/json"
	"net/http"
	"strings"
)

func (s *Server) handlePairingGenerate(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "pairing not initialized"})
		return
	}

	var p struct {
		Channel string `json:"channel"`
	}
	json.NewDecoder(r.Body).Decode(&p)
	if p.Channel == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "channel required"})
		return
	}

	code, err := s.pairing.GenerateCode(p.Channel)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]any{
		"code":    code,
		"channel": p.Channel,
		"expires": "10 minutes",
		"instruction": "Send this code to your " + p.Channel + " bot to pair it.",
	})
}

func (s *Server) handlePairingList(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		writeJSON(w, []any{})
		return
	}

	channel := r.URL.Query().Get("channel")
	devices, err := s.pairing.ListPaired(r.Context(), channel)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, devices)
}

func (s *Server) handlePairingDelete(w http.ResponseWriter, r *http.Request) {
	if s.pairing == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "pairing not initialized"})
		return
	}

	// Path: /api/pairing/{channel}/{chatID}
	path := extractPathParam(r.URL.Path, "/api/pairing/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "channel and chat_id required"})
		return
	}

	channel, chatID := parts[0], parts[1]
	if err := s.pairing.Unpair(r.Context(), channel, chatID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "unpaired", "channel": channel, "chat_id": chatID})
}

func (s *Server) handlePairingStatus(w http.ResponseWriter, r *http.Request) {
	enabled := s.pairing != nil && s.pairing.IsEnabled()
	writeJSON(w, map[string]any{
		"enabled": enabled,
	})
}
