package provider

import (
	"sync"
	"time"
)

// CooldownTracker tracks model-scoped cooldowns for rate-limited or
// overloaded providers. Global (shared across all sessions) because
// rate limits are typically account-level, not session-level.
type CooldownTracker struct {
	entries sync.Map // key: "provider:model" → *cooldownEntry
}

type cooldownEntry struct {
	until  time.Time
	level  int            // Backoff level: 0, 1, 2 (max).
	reason FailoverReason // Why the cooldown was set.
}

// NewCooldownTracker creates a new tracker.
func NewCooldownTracker() *CooldownTracker {
	return &CooldownTracker{}
}

// Mark sets a cooldown for a specific provider+model combination.
// If retryAfter > 0, uses that duration. Otherwise, calculates from
// backoff level with exponential increase.
//
// Backoff levels (rate_limit): 60s → 120s → 300s (max)
// Backoff levels (overloaded): 30s → 60s → 120s (max)
func (ct *CooldownTracker) Mark(providerName, model string, reason FailoverReason, retryAfter time.Duration) {
	key := providerName + ":" + model

	var level int
	if existing, ok := ct.entries.Load(key); ok {
		level = existing.(*cooldownEntry).level + 1
		if level > 2 {
			level = 2
		}
	}

	var duration time.Duration
	if retryAfter > 0 {
		duration = retryAfter
		if duration > 5*time.Minute {
			duration = 5 * time.Minute
		}
	} else {
		duration = cooldownDuration(reason, level)
	}

	ct.entries.Store(key, &cooldownEntry{
		until:  time.Now().Add(duration),
		level:  level,
		reason: reason,
	})
}

// IsAvailable returns true if the given provider+model has no active cooldown
// or the cooldown has expired. Expired entries are automatically cleaned up.
func (ct *CooldownTracker) IsAvailable(providerName, model string) bool {
	key := providerName + ":" + model
	v, ok := ct.entries.Load(key)
	if !ok {
		return true
	}
	entry := v.(*cooldownEntry)
	if time.Now().After(entry.until) {
		ct.entries.Delete(key)
		return true
	}
	return false
}

// ShortestCooldown returns the duration until the nearest cooldown expires.
// Returns 0 if no active cooldowns exist.
func (ct *CooldownTracker) ShortestCooldown() time.Duration {
	shortest := time.Duration(0)
	now := time.Now()

	ct.entries.Range(func(_, value any) bool {
		entry := value.(*cooldownEntry)
		remaining := entry.until.Sub(now)
		if remaining <= 0 {
			return true // Expired, skip.
		}
		if shortest == 0 || remaining < shortest {
			shortest = remaining
		}
		return true
	})

	return shortest
}

// Clear removes a cooldown entry for a specific provider+model.
func (ct *CooldownTracker) Clear(providerName, model string) {
	ct.entries.Delete(providerName + ":" + model)
}

func cooldownDuration(reason FailoverReason, level int) time.Duration {
	switch reason {
	case ReasonRateLimit:
		switch level {
		case 0:
			return 60 * time.Second
		case 1:
			return 120 * time.Second
		default:
			return 300 * time.Second
		}
	case ReasonOverloaded:
		switch level {
		case 0:
			return 30 * time.Second
		case 1:
			return 60 * time.Second
		default:
			return 120 * time.Second
		}
	case ReasonBilling:
		return 5 * time.Minute
	default:
		return 30 * time.Second
	}
}
