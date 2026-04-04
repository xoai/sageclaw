package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/anthropic"
	"github.com/xoai/sageclaw/pkg/provider/gemini"
	provgithub "github.com/xoai/sageclaw/pkg/provider/github"
	"github.com/xoai/sageclaw/pkg/provider/ollama"
	"github.com/xoai/sageclaw/pkg/provider/openai"
	"github.com/xoai/sageclaw/pkg/provider/openaicompat"
	"github.com/xoai/sageclaw/pkg/provider/openrouter"
	"github.com/xoai/sageclaw/pkg/store"
	storesqlite "github.com/xoai/sageclaw/pkg/store/sqlite"
)

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
			"config": json.RawMessage(config),
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
		Type            string `json:"type"`
		Name            string `json:"name"`
		BaseURL         string `json:"base_url"`
		APIKey          string `json:"api_key"`
		TokensPerMinute int    `json:"tokens_per_minute,omitempty"`
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
		default:
			// Check openaicompat registry for known base URLs.
			if cfg := openaicompat.KnownProvider(p.Type); cfg != nil {
				p.BaseURL = cfg.BaseURL
			}
		}
	}

	id := fmt.Sprintf("%s_%s", p.Type, strings.ReplaceAll(strings.ToLower(p.Name), " ", "_"))

	// Encrypt and store API key.
	if p.APIKey != "" {
		if err := s.store.StoreCredential(r.Context(), "provider_"+id, []byte(p.APIKey), s.encKey); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			writeJSON(w, map[string]string{"error": "failed to store API key"})
			return
		}
	}

	// Build config JSON with TPM.
	tpm := p.TokensPerMinute
	if tpm == 0 {
		tpm = provider.DefaultTPM(p.Type)
	}
	configJSON := fmt.Sprintf(`{"tokens_per_minute":%d}`, tpm)

	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO providers (id, type, name, base_url, config, status) VALUES (?, ?, ?, ?, ?, 'active')
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, base_url=excluded.base_url, config=excluded.config, updated_at=datetime('now')`,
		id, p.Type, p.Name, p.BaseURL, configJSON)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Hot-reload: create provider client and register with router immediately.
	if s.router != nil && p.APIKey != "" {
		s.activateProvider(p.Type, p.APIKey, p.BaseURL)
		s.router.SetProviderTPM(p.Type, tpm)
	}

	writeJSON(w, map[string]string{"id": id, "status": "created"})
}

// safeProviderHealth returns a snapshot of the health map (thread-safe).
func (s *Server) safeProviderHealth() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := make(map[string]string, len(s.providerHealth))
	for k, v := range s.providerHealth {
		cp[k] = v
	}
	return cp
}

// activateProvider creates a provider client, registers it, and triggers async
// model discovery + preset combo regeneration. No hardcoded model IDs.
func (s *Server) activateProvider(provType, apiKey, baseURL string) {
	var prov provider.Provider

	switch provType {
	case "anthropic":
		prov = anthropic.NewClient(apiKey)
	case "openai":
		prov = openai.NewClient(apiKey)
	case "gemini":
		prov = gemini.NewClient(apiKey)
	case "openrouter":
		prov = openrouter.NewClient(apiKey)
	case "github":
		prov = provgithub.NewClient(apiKey)
	case "ollama":
		if baseURL == "" {
			baseURL = "http://localhost:11434"
		}
		prov = ollama.New(ollama.WithBaseURL(baseURL))
	default:
		cfg := openaicompat.KnownProvider(provType)
		if cfg == nil {
			cfg = &openaicompat.Config{Name: provType}
		}
		cfg.APIKey = apiKey
		if baseURL != "" {
			cfg.BaseURL = baseURL
		}
		prov = openaicompat.New(*cfg)
	}

	if prov == nil {
		return
	}

	// Register provider for combo resolution and model discovery.
	s.router.RegisterProvider(provType, prov)
	s.mu.Lock()
	s.providerHealth[provType] = "activating"
	s.mu.Unlock()

	log.Printf("provider: %s registered — discovering models...", provType)

	// Async discovery + preset combo regeneration.
	go func() {
		s.discoverModels(provType)

		// Regenerate preset combos from all discovered models.
		allModels, _ := s.store.ListAllDiscoveredModels(context.Background())
		if len(allModels) > 0 {
			dmi := make([]provider.DiscoveredModelInfo, 0, len(allModels))
			for _, m := range allModels {
				dmi = append(dmi, provider.DiscoveredModelInfo{
					ModelID: m.ModelID, Provider: m.Provider,
					OutputCost: m.OutputCost, ContextWindow: m.ContextWindow,
				})
			}
			presets := provider.GeneratePresetCombos(dmi, s.router.ConnectedProviders())
			s.router.SetPresetCombos(presets)
		}

		s.mu.Lock()
		s.providerHealth[provType] = "connected"
		s.mu.Unlock()
		log.Printf("provider: %s activated at runtime (discovery complete)", provType)
	}()
}

// discoverModels fetches the model list from a provider API and caches it in SQLite.
// Safe to call concurrently — each provider's refresh is transactional.
func (s *Server) discoverModels(provType string) {
	if s.router == nil {
		return
	}

	var target provider.Provider
	s.router.ForEachProvider(func(name string, p provider.Provider) {
		if name == provType {
			target = p
		}
	})
	if target == nil {
		return
	}

	lister, ok := target.(provider.ModelLister)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := lister.ListModels(ctx)
	if err != nil {
		log.Printf("discover models: %s: %v", provType, err)
		return
	}

	// Convert ModelInfo → DiscoveredModel.
	discovered := make([]store.DiscoveredModel, 0, len(models))
	for _, m := range models {
		discovered = append(discovered, store.DiscoveredModel{
			ID:            m.ID,
			Provider:      m.Provider,
			ModelID:       m.ModelID,
			DisplayName:   m.Name,
			ContextWindow: m.ContextWindow,
			Capabilities:  make(map[string]bool),
		})
	}

	// Use transactional refresh if the store supports it.
	if sqlStore, ok := s.store.(*storesqlite.Store); ok {
		if err := sqlStore.RefreshDiscoveredModels(context.Background(), provType, discovered); err != nil {
			log.Printf("discover models: %s: cache write failed: %v", provType, err)
			return
		}
	}

	log.Printf("discover models: %s: cached %d models", provType, len(discovered))

	// Enrich newly discovered models with seed pricing,
	// but only for models that have NO pricing yet (empty source).
	// Models already priced by OpenRouter or user are not downgraded.
	// Uses GetSeedPricing (not FindModel) to avoid chicken-and-egg
	// during first boot when discovered_models is being populated.
	allModels, _ := s.store.ListAllDiscoveredModels(context.Background())
	hasPricing := make(map[string]bool)
	for _, m := range allModels {
		if m.PricingSource != "" {
			hasPricing[m.ModelID] = true
			hasPricing[m.ID] = true
		}
	}

	var pricingUpdates []store.ModelPricingBulk
	for _, d := range discovered {
		if hasPricing[d.ModelID] || hasPricing[d.ID] {
			continue // Already has pricing — don't downgrade.
		}
		km := provider.GetSeedPricing(d.ModelID)
		if km == nil {
			km = provider.GetSeedPricing(d.ID)
		}
		if km != nil {
			pricingUpdates = append(pricingUpdates, store.ModelPricingBulk{
				ModelID:   km.ModelID,
				Provider:  d.Provider,
				InputCost: km.InputCost, OutputCost: km.OutputCost,
				CacheCost: km.CacheCost, ThinkingCost: km.ThinkingCost,
				Source: "known",
			})
		}
	}
	if len(pricingUpdates) > 0 {
		if err := s.store.BulkUpdateModelPricing(context.Background(), pricingUpdates); err != nil {
			log.Printf("discover models: %s: pricing enrichment failed: %v", provType, err)
		}
	}

	// Preset combos are seeded once at startup from KnownModels and stored in DB.
	// No regeneration here — users may have edited them. DB is source of truth.
}

// handleProvidersUpdateConfig updates a provider's config (e.g. tokens_per_minute).
// PATCH /api/providers/{id}/config
func (s *Server) handleProvidersUpdateConfig(w http.ResponseWriter, r *http.Request) {
	remainder := extractPathParam(r.URL.Path, "/api/providers/")
	parts := strings.SplitN(remainder, "/", 2)
	if len(parts) < 2 || parts[1] != "config" || parts[0] == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "expected /api/providers/{id}/config"})
		return
	}
	id := parts[0]

	var updates map[string]any
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	// Read current config.
	var currentConfig string
	var provType string
	s.store.DB().QueryRowContext(r.Context(),
		`SELECT type, config FROM providers WHERE id = ?`, id).Scan(&provType, &currentConfig)
	if provType == "" {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "provider not found"})
		return
	}

	// Merge updates into current config.
	cfg := make(map[string]any)
	json.Unmarshal([]byte(currentConfig), &cfg)
	for k, v := range updates {
		cfg[k] = v
	}
	merged, _ := json.Marshal(cfg)

	if _, err := s.store.DB().ExecContext(r.Context(),
		`UPDATE providers SET config = ?, updated_at = datetime('now') WHERE id = ?`,
		string(merged), id); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to save config"})
		return
	}

	// Hot-reload TPM on the router.
	if s.router != nil {
		if raw, ok := cfg["tokens_per_minute"]; ok {
			var tpmInt int
			switch v := raw.(type) {
			case float64:
				tpmInt = int(v)
			case int:
				tpmInt = v
			case json.Number:
				if n, err := v.Int64(); err == nil {
					tpmInt = int(n)
				}
			}
			if tpmInt > 0 {
				s.router.SetProviderTPM(provType, tpmInt)
				log.Printf("provider: hot-reloaded TPM for %s → %d", provType, tpmInt)
			} else {
				log.Printf("provider: skipped TPM hot-reload for %s (value=%v type=%T)", provType, raw, raw)
			}
		}
	}

	writeJSON(w, map[string]string{"id": id, "status": "updated"})
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

// handleProvidersModels returns all models (discovered + known fallback) with pricing and availability.
func (s *Server) handleProvidersModels(w http.ResponseWriter, r *http.Request) {
	connectedProviders := map[string]bool{}
	s.mu.RLock()
	for pName, status := range s.providerHealth {
		if status == "connected" {
			connectedProviders[pName] = true
		}
	}
	s.mu.RUnlock()

	// 1. Load discovered models from SQLite cache.
	discovered, dbErr := s.store.ListAllDiscoveredModels(r.Context())
	degraded := dbErr != nil
	if dbErr != nil {
		log.Printf("models: failed to load discovered models: %v", dbErr)
	}

	// 2. Derive tier labels from preset combo membership.
	modelTier := map[string]string{} // modelID → "strong", "fast", "balanced", "local"
	if s.router != nil {
		for _, tierName := range []string{"strong", "fast", "balanced", "local"} {
			if combo, ok := s.router.GetCombo(tierName); ok {
				for _, m := range combo.Models {
					if _, already := modelTier[m.ModelID]; !already {
						modelTier[m.ModelID] = tierName
					}
				}
			}
		}
	}

	// 3. Build model list from discovered models (pricing comes from DB — seeded or OpenRouter).
	var models []map[string]any
	for _, d := range discovered {
		entry := map[string]any{
			"id": d.ID, "provider": d.Provider, "model_id": d.ModelID,
			"name": d.DisplayName, "context_window": d.ContextWindow,
			"max_output_tokens": d.MaxOutputTokens,
			"input_cost": d.InputCost, "output_cost": d.OutputCost,
			"cache_cost": d.CacheCost, "thinking_cost": d.ThinkingCost,
			"cache_creation_cost": d.CacheCreationCost,
			"available": connectedProviders[d.Provider],
			"source":    d.PricingSource,
		}
		if tier, ok := modelTier[d.ModelID]; ok {
			entry["tier"] = tier
		}
		// Enrich with capabilities from the registry.
		if caps, ok := provider.LookupModelCapabilities(d.ModelID); ok {
			entry["capabilities"] = caps
		}
		models = append(models, entry)
	}

	if models == nil {
		models = []map[string]any{}
	}

	// 4. Check staleness.
	stale := false
	for prov := range connectedProviders {
		age, _ := s.store.GetDiscoveredModelAge(r.Context(), prov)
		if age > 24*time.Hour {
			stale = true
			break
		}
	}

	// 5. Load combos.
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
		"stale":      stale,
		"degraded":   degraded,
		"cost_stats": provider.GlobalCacheStats.Snapshot().WithCalculations(),
	})
}

// handleProvidersModelsLive returns cached models (backward compat).
// Supports ?force=true to trigger a synchronous refresh first.
func (s *Server) handleProvidersModelsLive(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("force") == "true" {
		s.refreshAllProviderModels()
	}
	// Delegate to the cache-based endpoint.
	s.handleProvidersModels(w, r)
}

// handleProvidersModelsRefresh triggers model discovery for all connected providers.
func (s *Server) handleProvidersModelsRefresh(w http.ResponseWriter, r *http.Request) {
	refreshed, failed := s.refreshAllProviderModels()
	writeJSON(w, map[string]any{
		"refreshed": refreshed,
		"failed":    failed,
	})
}

// refreshAllProviderModels discovers models for all connected providers.
// Collects provider names first to avoid holding the router lock during discovery.
func (s *Server) refreshAllProviderModels() (refreshed, failed []string) {
	if s.router == nil {
		return
	}
	// Collect names under read lock, then discover outside the lock.
	var providers []string
	s.router.ForEachProvider(func(name string, p provider.Provider) {
		if _, ok := p.(provider.ModelLister); ok {
			providers = append(providers, name)
		}
	})
	for _, name := range providers {
		s.discoverModels(name)
		models, err := s.store.ListDiscoveredModels(context.Background(), name)
		if err == nil && len(models) > 0 {
			refreshed = append(refreshed, name)
		} else {
			failed = append(failed, name)
		}
	}
	return
}

// DiscoverAllModels runs model discovery for all connected providers concurrently.
// Called at startup after the RPC server is created.
func (s *Server) DiscoverAllModels() {
	if s.router == nil {
		return
	}
	go func() {
		var providers []string
		s.router.ForEachProvider(func(name string, p provider.Provider) {
			if _, ok := p.(provider.ModelLister); ok {
				providers = append(providers, name)
			}
		})
		for _, name := range providers {
			s.discoverModels(name)
		}
		if len(providers) > 0 {
			log.Printf("discover models: startup discovery complete for %d providers", len(providers))
		}
	}()
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
		ID          string          `json:"id"`
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

	// Block reserved preset combo names.
	reserved := map[string]bool{"strong": true, "fast": true, "balanced": true, "local": true}
	if reserved[strings.ToLower(p.Name)] {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("name %q is reserved for preset combos", p.Name)})
		return
	}

	if p.Strategy == "" {
		p.Strategy = "priority"
	}

	newID := strings.ReplaceAll(strings.ToLower(p.Name), " ", "_")

	// When editing an existing combo whose name changed, delete the old row
	// so we don't create a duplicate (the id is derived from the name).
	if p.ID != "" && p.ID != newID {
		s.store.DB().ExecContext(r.Context(), `DELETE FROM combos WHERE id = ? AND is_preset = 0`, p.ID)
		if s.router != nil {
			s.router.RemoveCombo(p.ID)
		}
	}

	// Sanitize double-encoded models before storing.
	modelsRaw := p.Models
	var testModels []provider.ComboModel
	if json.Unmarshal(modelsRaw, &testModels) != nil {
		var unwrapped string
		if json.Unmarshal(modelsRaw, &unwrapped) == nil {
			modelsRaw = json.RawMessage(unwrapped)
		}
	}

	_, err := s.store.DB().ExecContext(r.Context(),
		`INSERT INTO combos (id, name, description, strategy, models, is_preset) VALUES (?, ?, ?, ?, ?, 0)
		 ON CONFLICT(id) DO UPDATE SET name=excluded.name, description=excluded.description,
		 strategy=excluded.strategy, models=excluded.models`,
		newID, p.Name, p.Description, p.Strategy, string(modelsRaw))
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Sync combo to router for live resolution.
	if s.router != nil {
		var models []provider.ComboModel
		if json.Unmarshal(modelsRaw, &models) == nil && len(models) > 0 {
			s.router.SetCombo(newID, provider.Combo{
				Name: p.Name, Strategy: p.Strategy, Models: models, IsUser: true,
			})
		}
	}
	writeJSON(w, map[string]string{"id": newID, "status": "created"})
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

	// Check if any agents are using this combo.
	comboRef := "combo:" + id
	var affectedAgents []string
	if s.agentsDir != "" {
		entries, _ := os.ReadDir(s.agentsDir)
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			identityPath := filepath.Join(s.agentsDir, entry.Name(), "identity.yaml")
			data, err := os.ReadFile(identityPath)
			if err != nil {
				continue
			}
			if strings.Contains(string(data), comboRef) {
				affectedAgents = append(affectedAgents, entry.Name())
			}
		}
	}

	s.store.DB().ExecContext(r.Context(), `DELETE FROM combos WHERE id = ?`, id)

	// Remove combo from router.
	if s.router != nil {
		s.router.RemoveCombo(id)
	}

	result := map[string]any{"id": id, "status": "deleted"}
	if len(affectedAgents) > 0 {
		result["warning"] = fmt.Sprintf("Combo was used by agents: %s. They will fall back to the default tier.", strings.Join(affectedAgents, ", "))
		result["affected_agents"] = affectedAgents
	}
	writeJSON(w, result)
}
