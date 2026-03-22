package rpc

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/anthropic"
	"github.com/xoai/sageclaw/pkg/provider/gemini"
	provgithub "github.com/xoai/sageclaw/pkg/provider/github"
	"github.com/xoai/sageclaw/pkg/provider/ollama"
	"github.com/xoai/sageclaw/pkg/provider/openai"
	"github.com/xoai/sageclaw/pkg/provider/openrouter"
)

// Known models per provider type for discovery.
var knownModels = map[string][]map[string]string{
	"anthropic": {
		{"id": "claude-sonnet-4-20250514", "name": "Claude Sonnet 4", "tier": "strong"},
		{"id": "claude-haiku-4-5-20251001", "name": "Claude Haiku 4.5", "tier": "fast"},
		{"id": "claude-opus-4-20250514", "name": "Claude Opus 4", "tier": "strong"},
	},
	"openai": {
		{"id": "gpt-4o", "name": "GPT-4o", "tier": "strong"},
		{"id": "gpt-4o-mini", "name": "GPT-4o Mini", "tier": "fast"},
		{"id": "gpt-4.1", "name": "GPT-4.1", "tier": "strong"},
		{"id": "gpt-4.1-mini", "name": "GPT-4.1 Mini", "tier": "fast"},
		{"id": "gpt-4.1-nano", "name": "GPT-4.1 Nano", "tier": "fast"},
		{"id": "o3-mini", "name": "o3-mini", "tier": "strong"},
	},
	"ollama": {
		{"id": "llama3.2", "name": "Llama 3.2", "tier": "local"},
		{"id": "mistral", "name": "Mistral", "tier": "local"},
		{"id": "codellama", "name": "Code Llama", "tier": "local"},
		{"id": "gemma2", "name": "Gemma 2", "tier": "local"},
		{"id": "qwen2.5", "name": "Qwen 2.5", "tier": "local"},
		{"id": "deepseek-r1", "name": "DeepSeek R1", "tier": "local"},
	},
}

// --- Providers ---

func (s *Server) handleProvidersList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, type, name, base_url, models, status, config, created_at FROM providers ORDER BY type, name`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var providers []map[string]any
	for rows.Next() {
		var id, pType, name, baseURL, models, status, config, createdAt string
		rows.Scan(&id, &pType, &name, &baseURL, &models, &status, &config, &createdAt)
		providers = append(providers, map[string]any{
			"id": id, "type": pType, "name": name, "base_url": baseURL,
			"models": json.RawMessage(models), "status": status,
			"has_key": true, "created_at": createdAt,
		})
	}
	if providers == nil {
		providers = []map[string]any{}
	}
	writeJSON(w, providers)
}

func (s *Server) handleProvidersCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Type == "" || p.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "type and name required"})
		return
	}

	// Set default base URLs.
	if p.BaseURL == "" {
		switch p.Type {
		case "anthropic":
			p.BaseURL = "https://api.anthropic.com"
		case "openai":
			p.BaseURL = "https://api.openai.com"
		case "ollama":
			p.BaseURL = "http://localhost:11434"
		}
	}

	id := fmt.Sprintf("%s_%s", p.Type, strings.ReplaceAll(strings.ToLower(p.Name), " ", "_"))

	// Get known models for this type.
	models := knownModels[p.Type]
	modelsJSON, _ := json.Marshal(models)

	// Encrypt and store API key.
	var apiKeyEnc []byte
	if p.APIKey != "" {
		if err := s.store.StoreCredential(r.Context(), "provider_"+id, []byte(p.APIKey), s.encKey); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "failed to store API key"})
			return
		}
	}

	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO providers (id, type, name, base_url, api_key_enc, models, status) VALUES (?, ?, ?, ?, ?, ?, 'active')
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, base_url=excluded.base_url, api_key_enc=excluded.api_key_enc,
		 models=excluded.models, updated_at=datetime('now')`,
		id, p.Type, p.Name, p.BaseURL, apiKeyEnc, string(modelsJSON))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Hot-reload: create provider client and register with router immediately.
	if s.router != nil && p.APIKey != "" {
		s.activateProvider(p.Type, p.APIKey, p.BaseURL)
	}

	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

// activateProvider creates a provider client and registers it with the router at runtime.
func (s *Server) activateProvider(provType, apiKey, baseURL string) {
	var prov provider.Provider
	var strongModel, fastModel string

	switch provType {
	case "anthropic":
		prov = anthropic.NewClient(apiKey)
		strongModel = "claude-sonnet-4-20250514"
		fastModel = "claude-haiku-4-5-20251001"
	case "openai":
		prov = openai.NewClient(apiKey)
		strongModel = "gpt-4o"
		fastModel = "gpt-4o-mini"
	case "gemini":
		prov = gemini.NewClient(apiKey)
		strongModel = "gemini-2.0-flash"
		fastModel = "gemini-2.0-flash-lite"
	case "openrouter":
		prov = openrouter.NewClient(apiKey)
		strongModel = "anthropic/claude-sonnet-4-20250514"
		fastModel = "anthropic/claude-haiku-4-5-20251001"
	case "github":
		prov = provgithub.NewClient(apiKey)
		strongModel = "gpt-4o"
		fastModel = "gpt-4o-mini"
	case "ollama":
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		prov = ollama.New(ollama.WithBaseURL(baseURL))
		strongModel = "llama3.2:3b"
		fastModel = "llama3.2:3b"
	default:
		return
	}

	if prov == nil {
		return
	}

	// Register with router — set tiers if not already occupied.
	if !s.router.HasTier(provider.TierStrong) {
		s.router.SetRoute(provider.TierStrong, provider.Route{Provider: prov, Model: strongModel})
	}
	if !s.router.HasTier(provider.TierFast) {
		s.router.SetRoute(provider.TierFast, provider.Route{Provider: prov, Model: fastModel})
	}

	// Update health map.
	s.providerHealth[provType] = "connected"

	log.Printf("provider: %s activated at runtime (hot-reload)", provType)
}

func (s *Server) handleProvidersDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/providers/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "provider ID required"})
		return
	}
	s.store.DB().ExecContext(r.Context(), `DELETE FROM credentials WHERE key = ?`, "provider_"+id)
	s.store.DB().ExecContext(r.Context(), `DELETE FROM providers WHERE id = ?`, id)
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}

// handleProvidersModels returns all known models with pricing and availability.
func (s *Server) handleProvidersModels(w http.ResponseWriter, r *http.Request) {
	// Build set of connected providers from health.
	connectedProviders := map[string]bool{}
	for pName, status := range s.providerHealth {
		if status == "connected" {
			connectedProviders[pName] = true
		}
	}

	// Return the full known model registry with availability status.
	var models []map[string]any
	for _, m := range provider.KnownModels {
		models = append(models, map[string]any{
			"id":             m.ID,
			"provider":       m.Provider,
			"model_id":       m.ModelID,
			"name":           m.Name,
			"tier":           m.Tier,
			"input_cost":     m.InputCost,
			"output_cost":    m.OutputCost,
			"cache_cost":     m.CacheCost,
			"context_window": m.ContextWindow,
			"available":      connectedProviders[m.Provider],
		})
	}

	// Load combos.
	comboRows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, description, strategy, models, is_preset FROM combos ORDER BY is_preset DESC, name`)
	var combos []map[string]any
	if err == nil {
		defer comboRows.Close()
		for comboRows.Next() {
			var id, name, desc, strategy, modelsJSON string
			var isPreset int
			comboRows.Scan(&id, &name, &desc, &strategy, &modelsJSON, &isPreset)
			combos = append(combos, map[string]any{
				"id": id, "name": name, "description": desc,
				"strategy": strategy, "models": json.RawMessage(modelsJSON),
				"is_preset": isPreset == 1,
			})
		}
	}
	if combos == nil {
		combos = []map[string]any{}
	}

	writeJSON(w, map[string]any{
		"models":     models,
		"combos":     combos,
		"connected":  connectedProviders,
		"cost_stats": provider.GlobalCacheStats.Snapshot().WithCalculations(),
	})
}

// --- Combos ---

func (s *Server) handleCombosList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, description, strategy, models, is_preset FROM combos ORDER BY is_preset DESC, name`)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	var combos []map[string]any
	for rows.Next() {
		var id, name, desc, strategy, models string
		var isPreset int
		rows.Scan(&id, &name, &desc, &strategy, &models, &isPreset)
		combos = append(combos, map[string]any{
			"id": id, "name": name, "description": desc,
			"strategy": strategy, "models": json.RawMessage(models),
			"is_preset": isPreset == 1,
		})
	}
	if combos == nil {
		combos = []map[string]any{}
	}
	writeJSON(w, combos)
}

func (s *Server) handleCombosCreate(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		Strategy    string          `json:"strategy"`
		Models      json.RawMessage `json:"models"`
	}
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}
	if p.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "name required"})
		return
	}
	if p.Strategy == "" {
		p.Strategy = "priority"
	}

	id := strings.ReplaceAll(strings.ToLower(p.Name), " ", "_")

	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO combos (id, name, description, strategy, models, is_preset) VALUES (?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description,
		 strategy=excluded.strategy, models=excluded.models`,
		id, p.Name, p.Description, p.Strategy, string(p.Models))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

func (s *Server) handleCombosDelete(w http.ResponseWriter, r *http.Request) {
	id := extractPathParam(r.URL.Path, "/api/combos/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "combo ID required"})
		return
	}

	// Don't allow deleting presets.
	var isPreset int
	s.store.DB().QueryRow(`SELECT is_preset FROM combos WHERE id = ?`, id).Scan(&isPreset)
	if isPreset == 1 {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "cannot delete preset combos"})
		return
	}

	s.store.DB().ExecContext(r.Context(), `DELETE FROM combos WHERE id = ?`, id)
	writeJSON(w, map[string]string{"id": id, "status": "deleted"})
}
