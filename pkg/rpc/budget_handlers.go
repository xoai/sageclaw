package rpc

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/store"
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

// handleBudgetPricingList returns all models with their current pricing.
func (s *Server) handleBudgetPricingList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all discovered models with pricing.
	models, err := s.store.ListAllDiscoveredModels(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Get user overrides to annotate.
	overrides, _ := s.store.ListModelPricingOverrides(ctx)
	overrideMap := make(map[string]bool)
	for _, o := range overrides {
		overrideMap[o.ModelID] = true
	}

	type pricingEntry struct {
		ModelID           string  `json:"model_id"`
		Provider          string  `json:"provider"`
		DisplayName       string  `json:"display_name"`
		InputCost         float64 `json:"input_cost"`
		OutputCost        float64 `json:"output_cost"`
		CacheCost         float64 `json:"cache_cost"`
		ThinkingCost      float64 `json:"thinking_cost"`
		CacheCreationCost float64 `json:"cache_creation_cost"`
		PricingSource     string  `json:"pricing_source"`
		HasOverride       bool    `json:"has_override"`
	}

	result := make([]pricingEntry, 0, len(models))
	for _, m := range models {
		source := m.PricingSource
		if source == "" {
			source = "unknown"
		}
		result = append(result, pricingEntry{
			ModelID:           m.ModelID,
			Provider:          m.Provider,
			DisplayName:       m.DisplayName,
			InputCost:         m.InputCost,
			OutputCost:        m.OutputCost,
			CacheCost:         m.CacheCost,
			ThinkingCost:      m.ThinkingCost,
			CacheCreationCost: m.CacheCreationCost,
			PricingSource:     source,
			HasOverride:       overrideMap[m.ModelID] || overrideMap[m.ID],
		})
	}
	writeJSON(w, result)
}

// handleBudgetPricingOverride sets a user pricing override for a model.
func (s *Server) handleBudgetPricingOverride(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ModelID           string  `json:"model_id"`
		Provider          string  `json:"provider"`
		InputCost         float64 `json:"input_cost"`
		OutputCost        float64 `json:"output_cost"`
		CacheCost         float64 `json:"cache_cost"`
		ThinkingCost      float64 `json:"thinking_cost"`
		CacheCreationCost float64 `json:"cache_creation_cost"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request body"})
		return
	}
	if p.ModelID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "model_id is required"})
		return
	}
	if p.InputCost < 0 || p.OutputCost < 0 || p.CacheCost < 0 || p.ThinkingCost < 0 || p.CacheCreationCost < 0 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "costs must be non-negative"})
		return
	}

	if err := s.store.UpsertModelPricingOverride(r.Context(), store.ModelPricingOverride{
		ModelID:           p.ModelID,
		Provider:          p.Provider,
		InputCost:         p.InputCost,
		OutputCost:        p.OutputCost,
		CacheCost:         p.CacheCost,
		ThinkingCost:      p.ThinkingCost,
		CacheCreationCost: p.CacheCreationCost,
	}); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Invalidate ModelRegistry cache so new pricing takes effect immediately.
	if s.modelRegistry != nil {
		s.modelRegistry.InvalidateCache()
	}

	writeJSON(w, map[string]string{"status": "saved"})
}

// handleBudgetPricingDelete removes a user pricing override.
func (s *Server) handleBudgetPricingDelete(w http.ResponseWriter, r *http.Request) {
	modelID := strings.TrimPrefix(r.URL.Path, "/api/budget/pricing/")
	if modelID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "model_id is required"})
		return
	}

	if err := s.store.DeleteModelPricingOverride(r.Context(), modelID); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	if s.modelRegistry != nil {
		s.modelRegistry.InvalidateCache()
	}

	writeJSON(w, map[string]string{"status": "deleted"})
}
