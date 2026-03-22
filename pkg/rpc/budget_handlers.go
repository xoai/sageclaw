package rpc

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/xoai/sageclaw/pkg/provider"
)

func (s *Server) handleBudgetSummary(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, provider.SpendingSummary{})
		return
	}
	writeJSON(w, s.budgetEngine.GetSummary(r.Context()))
}

func (s *Server) handleBudgetConfig(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, provider.DefaultBudgetConfig())
		return
	}
	writeJSON(w, s.budgetEngine.GetConfig())
}

func (s *Server) handleBudgetConfigUpdate(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		w.WriteHeader(http.StatusNotImplemented)
		writeJSON(w, map[string]string{"error": "budget engine not available"})
		return
	}

	var p struct {
		DailyLimitUSD   float64 `json:"daily_limit_usd"`
		MonthlyLimitUSD float64 `json:"monthly_limit_usd"`
		AlertAtPercent  float64 `json:"alert_at_percent"`
		HardStop        bool    `json:"hard_stop"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.AlertAtPercent == 0 {
		p.AlertAtPercent = 80
	}

	if err := s.budgetEngine.UpdateConfig(r.Context(), p.DailyLimitUSD, p.MonthlyLimitUSD, p.AlertAtPercent, p.HardStop); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "updated"})
}

func (s *Server) handleBudgetHistory(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, []any{})
		return
	}
	daysStr := r.URL.Query().Get("days")
	days := 30
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}
	writeJSON(w, s.budgetEngine.GetDailyCosts(r.Context(), days))
}

func (s *Server) handleBudgetAlerts(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.budgetEngine.GetAlerts(r.Context(), 50))
}

func (s *Server) handleBudgetAlertsUnread(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.budgetEngine.GetUnacknowledgedAlerts(r.Context()))
}

func (s *Server) handleBudgetAlertAck(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		w.WriteHeader(http.StatusNotImplemented)
		writeJSON(w, map[string]string{"error": "budget engine not available"})
		return
	}

	idStr := extractPathParam(r.URL.Path, "/api/budget/alerts/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid alert ID"})
		return
	}

	if err := s.budgetEngine.AcknowledgeAlert(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"status": "acknowledged"})
}

func (s *Server) handleBudgetTopModels(w http.ResponseWriter, r *http.Request) {
	if s.budgetEngine == nil {
		writeJSON(w, []any{})
		return
	}
	writeJSON(w, s.budgetEngine.GetTopModels(r.Context(), 10))
}
