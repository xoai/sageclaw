package provider

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Tier represents a model routing tier.
type Tier string

const (
	TierLocal  Tier = "local"
	TierFast   Tier = "fast"
	TierStrong Tier = "strong"
)

// Route maps a tier to a provider and model.
type Route struct {
	Provider Provider
	Model    string
}

// Combo is a named fallback chain of provider/model pairs.
type Combo struct {
	Name     string
	Strategy string // "priority" (default), "round-robin", "cost"
	Models   []ComboModel
}

// ComboModel is a single entry in a combo's fallback chain.
type ComboModel struct {
	Provider string `json:"provider_type"` // provider name (e.g. "anthropic")
	ModelID  string `json:"model"`         // model ID (e.g. "claude-sonnet-4-20250514")
}

// Router selects which provider+model to use based on tier.
type Router struct {
	mu          sync.RWMutex
	routes      map[Tier]Route
	combos      map[string]Combo     // named combos
	providers   map[string]Provider  // provider name → instance
	providerTPM map[string]int       // provider name → tokens per minute (0 = unlimited)
	fallback    Tier
	bridge      *ContextBridge
	Cooldowns   *CooldownTracker     // Global model-scoped cooldown tracker.
}

// NewRouter creates a model router with the given routes and fallback tier.
func NewRouter(routes map[Tier]Route, fallback Tier) (*Router, error) {
	if _, ok := routes[fallback]; !ok {
		return nil, fmt.Errorf("fallback tier %q not found in routes", fallback)
	}
	return &Router{
		routes:      routes,
		combos:      make(map[string]Combo),
		providers:   make(map[string]Provider),
		providerTPM: make(map[string]int),
		fallback:    fallback,
		bridge:      NewContextBridge(),
		Cooldowns:   NewCooldownTracker(),
	}, nil
}

// NewEmptyRouter creates a router with no routes. Providers can be added
// at runtime via SetRoute. Used when SageClaw starts with no providers
// so that hot-reload from the dashboard works.
func NewEmptyRouter() *Router {
	return &Router{
		routes:      make(map[Tier]Route),
		combos:      make(map[string]Combo),
		providers:   make(map[string]Provider),
		providerTPM: make(map[string]int),
		fallback:    TierStrong,
		bridge:      NewContextBridge(),
		Cooldowns:   NewCooldownTracker(),
	}
}

// ChatWithFallback tries the primary tier, falls back on error, and uses the
// context bridge to handle model switches transparently.
func (r *Router) ChatWithFallback(ctx context.Context, tier Tier, req *canonical.Request) (*canonical.Response, error) {
	if !r.HasRoutes() {
		return nil, fmt.Errorf("no providers configured — add one via Settings > AI Models")
	}
	primary, primaryModel := r.Resolve(tier)
	req.Model = primaryModel

	resp, err := primary.Chat(ctx, req)
	if err == nil {
		return resp, nil
	}

	// Primary failed — try fallback if different.
	r.mu.RLock()
	fallbackRoute := r.routes[r.fallback]
	r.mu.RUnlock()
	if fallbackRoute.Provider.Name() == primary.Name() && fallbackRoute.Model == primaryModel {
		return nil, fmt.Errorf("provider %s failed (no different fallback): %w", primary.Name(), err)
	}

	log.Printf("router: %s/%s failed (%v), falling back to %s/%s",
		primary.Name(), primaryModel, err, fallbackRoute.Provider.Name(), fallbackRoute.Model)

	// Bridge the context — truncate if the fallback model has a smaller window.
	result := r.bridge.Transfer(req.Messages, primaryModel, fallbackRoute.Model)
	req.Messages = result.Messages
	req.Model = fallbackRoute.Model

	return fallbackRoute.Provider.Chat(ctx, req)
}

// ChatStreamWithFallback is the streaming version of ChatWithFallback.
func (r *Router) ChatStreamWithFallback(ctx context.Context, tier Tier, req *canonical.Request) (<-chan StreamEvent, error) {
	if !r.HasRoutes() {
		return nil, fmt.Errorf("no providers configured — add one via Settings > AI Models")
	}
	primary, primaryModel := r.Resolve(tier)
	req.Model = primaryModel

	stream, err := primary.ChatStream(ctx, req)
	if err == nil {
		return stream, nil
	}

	// Primary failed — try fallback.
	r.mu.RLock()
	fallbackRoute := r.routes[r.fallback]
	r.mu.RUnlock()
	if fallbackRoute.Provider.Name() == primary.Name() && fallbackRoute.Model == primaryModel {
		return nil, fmt.Errorf("provider %s failed (no different fallback): %w", primary.Name(), err)
	}

	log.Printf("router: %s/%s stream failed (%v), falling back to %s/%s",
		primary.Name(), primaryModel, err, fallbackRoute.Provider.Name(), fallbackRoute.Model)

	result := r.bridge.Transfer(req.Messages, primaryModel, fallbackRoute.Model)
	req.Messages = result.Messages
	req.Model = fallbackRoute.Model

	return fallbackRoute.Provider.ChatStream(ctx, req)
}

// Bridge returns the router's context bridge for direct use.
func (r *Router) Bridge() *ContextBridge {
	return r.bridge
}

// SetRoute adds or replaces a tier route at runtime (thread-safe).
// This enables hot-reload when providers are added via the dashboard.
func (r *Router) SetRoute(tier Tier, route Route) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes[tier] = route
	log.Printf("router: tier %s updated → %s/%s", tier, route.Provider.Name(), route.Model)
}

// SetDefault sets the default provider (used when no specific tier matches).
func (r *Router) SetDefault(provider Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Update strong tier as default if not already set, or override.
	r.routes[r.fallback] = Route{Provider: provider, Model: r.routes[r.fallback].Model}
}

// Resolve returns the provider and model for a given tier.
// Falls back to the default tier if the requested tier is not configured.
func (r *Router) Resolve(tier Tier) (Provider, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	route, ok := r.routes[tier]
	if !ok {
		route = r.routes[r.fallback]
	}
	return route.Provider, route.Model
}

// HasTier returns true if the tier is configured.
func (r *Router) HasTier(tier Tier) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.routes[tier]
	return ok
}

// HasRoutes returns true if the router has any routes configured.
func (r *Router) HasRoutes() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.routes) > 0
}

// Tiers returns all configured tier names.
func (r *Router) Tiers() []Tier {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tiers := make([]Tier, 0, len(r.routes))
	for t := range r.routes {
		tiers = append(tiers, t)
	}
	return tiers
}

// Fallback returns the fallback tier.
func (r *Router) Fallback() Tier {
	return r.fallback
}

// RegisterProvider makes a provider available for combo resolution by name.
func (r *Router) RegisterProvider(name string, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[name] = p
}

// SetCombo adds or replaces a named combo.
func (r *Router) SetCombo(name string, combo Combo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.combos[name] = combo
	log.Printf("router: combo %q updated (%d models, strategy=%s)", name, len(combo.Models), combo.Strategy)
}

// RemoveCombo removes a named combo.
func (r *Router) RemoveCombo(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.combos, name)
}

// Combos returns all registered combo names.
func (r *Router) Combos() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.combos))
	for name := range r.combos {
		names = append(names, name)
	}
	return names
}

// ResolveCombo looks up a combo by name and returns the first available
// provider+model pair (priority strategy). Returns an error if no model
// in the chain is available.
func (r *Router) ResolveCombo(name string) (Provider, string, error) {
	r.mu.RLock()
	combo, ok := r.combos[name]
	providers := r.providers
	r.mu.RUnlock()

	if !ok {
		// Fallback: if combo name matches a tier, resolve as tier.
		// Handles backward compat after preset combo deletion.
		switch Tier(name) {
		case TierStrong, TierFast, TierLocal:
			log.Printf("router: combo %q not found, falling back to tier %q (deprecated)", name, name)
			p, model := r.Resolve(Tier(name))
			return p, model, nil
		}
		if name == "balanced" {
			log.Printf("router: combo %q not found, falling back to tier fast (deprecated)", name)
			p, model := r.Resolve(TierFast)
			return p, model, nil
		}
		return nil, "", fmt.Errorf("combo %q not found", name)
	}

	for _, m := range combo.Models {
		p, pOk := providers[m.Provider]
		if !pOk {
			continue
		}
		return p, m.ModelID, nil
	}

	return nil, "", fmt.Errorf("combo %q: no available provider for any model in chain", name)
}

// ResolveComboWithCooldown resolves a combo, skipping models that are in cooldown.
// This replaces the provider-level exclusion with model-level cooldown checks.
func (r *Router) ResolveComboWithCooldown(name string) (Provider, string, error) {
	r.mu.RLock()
	combo, ok := r.combos[name]
	providers := r.providers
	r.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("combo %q not found", name)
	}

	for _, m := range combo.Models {
		if !r.Cooldowns.IsAvailable(m.Provider, m.ModelID) {
			continue
		}
		p, pOk := providers[m.Provider]
		if !pOk {
			continue
		}
		return p, m.ModelID, nil
	}

	return nil, "", fmt.Errorf("combo %q: all models in cooldown or unavailable", name)
}

// ComboTail returns the last model in a combo chain.
// Used for simple message routing (cheapest/lowest-priority model).
func (r *Router) ComboTail(name string) (Provider, string, error) {
	r.mu.RLock()
	combo, ok := r.combos[name]
	providers := r.providers
	r.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("combo %q not found", name)
	}

	// Walk in reverse to find last available model.
	for i := len(combo.Models) - 1; i >= 0; i-- {
		m := combo.Models[i]
		p, pOk := providers[m.Provider]
		if pOk {
			return p, m.ModelID, nil
		}
	}

	return nil, "", fmt.Errorf("combo %q: no available provider", name)
}

// Deprecated: ResolveComboExcluding is replaced by ResolveComboWithCooldown
// which uses model-scoped cooldowns instead of provider-level exclusion.
func (r *Router) ResolveComboExcluding(name, excludeProvider string) (Provider, string, error) {
	r.mu.RLock()
	combo, ok := r.combos[name]
	providers := r.providers
	r.mu.RUnlock()

	if !ok {
		return nil, "", fmt.Errorf("combo %q not found", name)
	}

	for _, m := range combo.Models {
		if m.Provider == excludeProvider {
			continue
		}
		p, pOk := providers[m.Provider]
		if !pOk {
			continue
		}
		return p, m.ModelID, nil
	}

	return nil, "", fmt.Errorf("combo %q: no available provider after excluding %s", name, excludeProvider)
}

// ForEachProvider calls fn for each registered provider.
func (r *Router) ForEachProvider(fn func(name string, p Provider)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.providers {
		fn(name, p)
	}
}

// ResolveTierFromDiscovered updates a tier route based on discovered models.
// Picks the best available model for the tier from connected providers.
func (r *Router) ResolveTierFromDiscovered(tier Tier, providerModels []TierCandidate) {
	best := pickBestForTier(tier, providerModels)
	if best == nil {
		return
	}

	r.mu.RLock()
	prov, ok := r.providers[best.Provider]
	r.mu.RUnlock()
	if !ok {
		return
	}

	r.SetRoute(tier, Route{Provider: prov, Model: best.ModelID})
}

// ResolveAllTiers updates all three tier routes from discovered models.
func (r *Router) ResolveAllTiers(candidates []TierCandidate) {
	for _, tier := range []Tier{TierStrong, TierFast, TierLocal} {
		r.ResolveTierFromDiscovered(tier, candidates)
	}
}

// TierCandidate is a model considered for tier routing.
type TierCandidate struct {
	Provider      string
	ModelID       string
	ContextWindow int
	TierHint      string  // From KnownModels: "strong", "fast", "local", "reasoning"
	InputCost     float64 // From KnownModels (0 if unknown)
}

// pickBestForTier scores candidates and returns the best one for a tier.
func pickBestForTier(tier Tier, candidates []TierCandidate) *TierCandidate {
	var best *TierCandidate
	bestScore := -1.0

	for i := range candidates {
		c := &candidates[i]
		score := scoreTierCandidate(tier, c)
		if score < 0 {
			continue // Filtered out.
		}
		if score > bestScore {
			bestScore = score
			best = c
		}
	}
	return best
}

func scoreTierCandidate(tier Tier, c *TierCandidate) float64 {
	switch tier {
	case TierStrong:
		score := float64(c.ContextWindow) / 1000.0
		if c.TierHint == "strong" {
			score += 100
		}
		if c.TierHint == "reasoning" {
			score += 50
		}
		if c.Provider == "ollama" {
			return -1 // Exclude local models from strong tier.
		}
		return score
	case TierFast:
		score := 0.0
		if c.TierHint == "fast" {
			score += 100
		}
		if c.InputCost > 0 {
			score += 50 - c.InputCost*10
		}
		if c.Provider == "ollama" {
			return -1 // Exclude local models from fast tier.
		}
		return score
	case TierLocal:
		if c.Provider != "ollama" {
			return -1 // Only local providers.
		}
		return 1 // Pick first available.
	default:
		return -1
	}
}

// DefaultTPM returns the default tokens-per-minute for a provider type.
func DefaultTPM(providerType string) int {
	switch providerType {
	case "anthropic":
		return 30000
	case "openai":
		return 60000
	case "gemini":
		return 1000000
	case "openrouter":
		return 60000
	case "github":
		return 60000
	case "ollama":
		return 0 // Unlimited.
	default:
		return 30000
	}
}

// SetProviderTPM sets the tokens-per-minute limit for a provider.
func (r *Router) SetProviderTPM(name string, tpm int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerTPM[name] = tpm
}

// GetProviderTPM returns the tokens-per-minute limit for a provider.
// Falls back to DefaultTPM if not explicitly set.
func (r *Router) GetProviderTPM(name string) int {
	r.mu.RLock()
	tpm, ok := r.providerTPM[name]
	r.mu.RUnlock()
	if ok {
		return tpm
	}
	return DefaultTPM(name)
}

// IsCombo returns true if the model string is a combo reference (combo:name).
func IsCombo(model string) bool {
	return len(model) > 6 && model[:6] == "combo:"
}

// ComboName extracts the combo name from a combo reference string.
func ComboName(model string) string {
	if IsCombo(model) {
		return model[6:]
	}
	return model
}
