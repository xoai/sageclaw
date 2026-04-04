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
	IsUser   bool // true for user-created combos (shadows presets)
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
	if primary == nil {
		return nil, fmt.Errorf("no provider available for tier %s", tier)
	}
	req.Model = primaryModel

	resp, err := primary.Chat(ctx, req)
	if err == nil {
		return resp, nil
	}

	// Primary failed — try fallback if different.
	fallbackProv, fallbackModel := r.Resolve(r.fallback)
	if fallbackProv == nil || (fallbackProv.Name() == primary.Name() && fallbackModel == primaryModel) {
		return nil, fmt.Errorf("provider %s failed (no different fallback): %w", primary.Name(), err)
	}

	log.Printf("router: %s/%s failed (%v), falling back to %s/%s",
		primary.Name(), primaryModel, err, fallbackProv.Name(), fallbackModel)

	// Bridge the context — truncate if the fallback model has a smaller window.
	result := r.bridge.Transfer(req.Messages, primaryModel, fallbackModel)
	req.Messages = result.Messages
	req.Model = fallbackModel

	return fallbackProv.Chat(ctx, req)
}

// ChatStreamWithFallback is the streaming version of ChatWithFallback.
func (r *Router) ChatStreamWithFallback(ctx context.Context, tier Tier, req *canonical.Request) (<-chan StreamEvent, error) {
	if !r.HasRoutes() {
		return nil, fmt.Errorf("no providers configured — add one via Settings > AI Models")
	}
	primary, primaryModel := r.Resolve(tier)
	if primary == nil {
		return nil, fmt.Errorf("no provider available for tier %s", tier)
	}
	req.Model = primaryModel

	stream, err := primary.ChatStream(ctx, req)
	if err == nil {
		return stream, nil
	}

	// Primary failed — try fallback.
	fallbackProv, fallbackModel := r.Resolve(r.fallback)
	if fallbackProv == nil || (fallbackProv.Name() == primary.Name() && fallbackModel == primaryModel) {
		return nil, fmt.Errorf("provider %s failed (no different fallback): %w", primary.Name(), err)
	}

	log.Printf("router: %s/%s stream failed (%v), falling back to %s/%s",
		primary.Name(), primaryModel, err, fallbackProv.Name(), fallbackModel)

	result := r.bridge.Transfer(req.Messages, primaryModel, fallbackModel)
	req.Messages = result.Messages
	req.Model = fallbackModel

	return fallbackProv.ChatStream(ctx, req)
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
// Resolution order: combo[tier] → routes[tier] → combo[fallback] → routes[fallback] → nil.
// Returns (nil, "") if no provider is available for the tier or its fallback.
func (r *Router) Resolve(tier Tier) (Provider, string) {
	// 1. Try combo for the requested tier.
	if p, model, err := r.ResolveCombo(string(tier)); err == nil {
		return p, model
	}

	// 2. Try static route for the requested tier.
	r.mu.RLock()
	route, ok := r.routes[tier]
	r.mu.RUnlock()
	if ok {
		return route.Provider, route.Model
	}

	// 3. Try combo for the fallback tier (skip if same as requested).
	if tier != r.fallback {
		if p, model, err := r.ResolveCombo(string(r.fallback)); err == nil {
			return p, model
		}
	}

	// 4. Try static route for the fallback tier.
	r.mu.RLock()
	route, ok = r.routes[r.fallback]
	r.mu.RUnlock()
	if ok {
		return route.Provider, route.Model
	}

	// 5. No provider available.
	return nil, ""
}

// HasTier returns true if the tier is configured (via static route or combo).
func (r *Router) HasTier(tier Tier) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.routes[tier]; ok {
		return true
	}
	_, ok := r.combos[string(tier)]
	return ok
}

// HasRoutes returns true if the router has any routes or combos configured.
func (r *Router) HasRoutes() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.routes) > 0 || len(r.combos) > 0
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

// GetCombo returns a combo by name. Returns ok=false if not found.
func (r *Router) GetCombo(name string) (Combo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.combos[name]
	return c, ok
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

// SetPresetCombos stores auto-generated preset combos in the router.
// Presets are stored alongside user combos; user combos with the same name
// take precedence (checked first in SetCombo which overwrites).
// Use this for auto-generated presets only — user combos go through SetCombo.
func (r *Router) SetPresetCombos(presets map[string]Combo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, combo := range presets {
		// Don't overwrite user-created combos.
		if existing, ok := r.combos[name]; ok && existing.IsUser {
			continue
		}
		r.combos[name] = combo
	}
	log.Printf("router: preset combos updated (%d presets)", len(presets))
}

// GetProvider returns a registered provider by name (e.g. "gemini", "anthropic").
// Used for direct model references like "gemini/gemini-3-flash-preview".
func (r *Router) GetProvider(name string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[name]
	return p, ok
}

// ConnectedProviders returns the names of all registered providers.
func (r *Router) ConnectedProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

// ForEachProvider calls fn for each registered provider.
func (r *Router) ForEachProvider(fn func(name string, p Provider)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name, p := range r.providers {
		fn(name, p)
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
