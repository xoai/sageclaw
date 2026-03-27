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
	Provider string `json:"provider"` // provider name (e.g. "anthropic")
	ModelID  string `json:"model_id"` // model ID (e.g. "claude-sonnet-4-20250514")
}

// Router selects which provider+model to use based on tier.
type Router struct {
	mu       sync.RWMutex
	routes   map[Tier]Route
	combos   map[string]Combo // named combos
	providers map[string]Provider // provider name → instance
	fallback Tier
	bridge   *ContextBridge
}

// NewRouter creates a model router with the given routes and fallback tier.
func NewRouter(routes map[Tier]Route, fallback Tier) (*Router, error) {
	if _, ok := routes[fallback]; !ok {
		return nil, fmt.Errorf("fallback tier %q not found in routes", fallback)
	}
	return &Router{
		routes:    routes,
		combos:    make(map[string]Combo),
		providers: make(map[string]Provider),
		fallback:  fallback,
		bridge:    NewContextBridge(),
	}, nil
}

// ChatWithFallback tries the primary tier, falls back on error, and uses the
// context bridge to handle model switches transparently.
func (r *Router) ChatWithFallback(ctx context.Context, tier Tier, req *canonical.Request) (*canonical.Response, error) {
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

// ForEachProvider calls fn for each registered provider.
func (r *Router) ForEachProvider(fn func(name string, p Provider)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.providers {
		fn(name, p)
	}
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
