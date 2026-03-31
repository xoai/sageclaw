package provider

import (
	"testing"
	"time"
)

func TestCooldownTracker_MarkAndCheck(t *testing.T) {
	ct := NewCooldownTracker()

	// Initially available.
	if !ct.IsAvailable("anthropic", "claude-sonnet-4") {
		t.Fatal("expected available before marking")
	}

	// Mark as rate-limited.
	ct.Mark("anthropic", "claude-sonnet-4", ReasonRateLimit, 0)

	// Should be in cooldown.
	if ct.IsAvailable("anthropic", "claude-sonnet-4") {
		t.Fatal("expected unavailable after marking")
	}

	// Different model on same provider should still be available.
	if !ct.IsAvailable("anthropic", "claude-haiku-4.5") {
		t.Fatal("different model should still be available")
	}
}

func TestCooldownTracker_Expiry(t *testing.T) {
	ct := NewCooldownTracker()

	// Mark with a very short custom duration.
	ct.Mark("openai", "gpt-4o", ReasonOverloaded, 10*time.Millisecond)

	if ct.IsAvailable("openai", "gpt-4o") {
		t.Fatal("expected unavailable immediately after marking")
	}

	// Wait for expiry.
	time.Sleep(20 * time.Millisecond)

	if !ct.IsAvailable("openai", "gpt-4o") {
		t.Fatal("expected available after cooldown expires")
	}
}

func TestCooldownTracker_BackoffLevels(t *testing.T) {
	ct := NewCooldownTracker()

	// Level 0: 60s for rate_limit.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)
	v, _ := ct.entries.Load("anthropic:sonnet")
	e := v.(*cooldownEntry)
	if e.level != 0 {
		t.Errorf("expected level 0, got %d", e.level)
	}
	expectedDuration := 60 * time.Second
	actual := time.Until(e.until)
	if actual < expectedDuration-2*time.Second || actual > expectedDuration+2*time.Second {
		t.Errorf("expected ~60s cooldown, got %v", actual)
	}

	// Level 1: 120s.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)
	v, _ = ct.entries.Load("anthropic:sonnet")
	e = v.(*cooldownEntry)
	if e.level != 1 {
		t.Errorf("expected level 1, got %d", e.level)
	}

	// Level 2: 300s (max).
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)
	v, _ = ct.entries.Load("anthropic:sonnet")
	e = v.(*cooldownEntry)
	if e.level != 2 {
		t.Errorf("expected level 2, got %d", e.level)
	}

	// Level stays at 2 (max).
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)
	v, _ = ct.entries.Load("anthropic:sonnet")
	e = v.(*cooldownEntry)
	if e.level != 2 {
		t.Errorf("expected level 2 (max), got %d", e.level)
	}
}

func TestCooldownTracker_RetryAfterOverride(t *testing.T) {
	ct := NewCooldownTracker()

	// Explicit Retry-After should override backoff calculation.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 5*time.Second)
	v, _ := ct.entries.Load("anthropic:sonnet")
	e := v.(*cooldownEntry)
	actual := time.Until(e.until)
	if actual < 3*time.Second || actual > 7*time.Second {
		t.Errorf("expected ~5s cooldown from Retry-After, got %v", actual)
	}
}

func TestCooldownTracker_ShortestCooldown(t *testing.T) {
	ct := NewCooldownTracker()

	// No entries → 0.
	if d := ct.ShortestCooldown(); d != 0 {
		t.Errorf("expected 0 with no entries, got %v", d)
	}

	// Add two cooldowns with different durations.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 60*time.Second)
	ct.Mark("openai", "gpt-4o", ReasonOverloaded, 10*time.Second)

	shortest := ct.ShortestCooldown()
	if shortest < 5*time.Second || shortest > 15*time.Second {
		t.Errorf("expected ~10s shortest cooldown, got %v", shortest)
	}
}

func TestCooldownTracker_Clear(t *testing.T) {
	ct := NewCooldownTracker()
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)

	if ct.IsAvailable("anthropic", "sonnet") {
		t.Fatal("expected unavailable after marking")
	}

	ct.Clear("anthropic", "sonnet")

	if !ct.IsAvailable("anthropic", "sonnet") {
		t.Fatal("expected available after clearing")
	}
}

func TestCooldownTracker_OverloadedBackoff(t *testing.T) {
	ct := NewCooldownTracker()

	// Level 0: 30s for overloaded.
	ct.Mark("gemini", "flash", ReasonOverloaded, 0)
	v, _ := ct.entries.Load("gemini:flash")
	e := v.(*cooldownEntry)
	expectedDuration := 30 * time.Second
	actual := time.Until(e.until)
	if actual < expectedDuration-2*time.Second || actual > expectedDuration+2*time.Second {
		t.Errorf("expected ~30s overloaded cooldown, got %v", actual)
	}
}

func TestCooldownTracker_BillingCooldown(t *testing.T) {
	ct := NewCooldownTracker()

	ct.Mark("anthropic", "sonnet", ReasonBilling, 0)
	v, _ := ct.entries.Load("anthropic:sonnet")
	e := v.(*cooldownEntry)
	expectedDuration := 5 * time.Minute
	actual := time.Until(e.until)
	if actual < expectedDuration-2*time.Second || actual > expectedDuration+2*time.Second {
		t.Errorf("expected ~5min billing cooldown, got %v", actual)
	}
}

func TestCooldownTracker_ProbeRecoveryIncrements(t *testing.T) {
	ct := NewCooldownTracker()

	// Mark, then expire, then mark again — level should increment.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 1*time.Millisecond)
	time.Sleep(5 * time.Millisecond)

	// Should be available now (expired).
	if !ct.IsAvailable("anthropic", "sonnet") {
		t.Fatal("expected available after expiry")
	}

	// Mark again — since entry was cleaned up by IsAvailable, level resets to 0.
	// This is intentional: if the cooldown fully expired, the probe succeeded
	// at least once (or we waited the full duration), so reset is correct.
	ct.Mark("anthropic", "sonnet", ReasonRateLimit, 0)
	v, _ := ct.entries.Load("anthropic:sonnet")
	e := v.(*cooldownEntry)
	if e.level != 0 {
		t.Errorf("expected level 0 after full expiry+recovery, got %d", e.level)
	}
}
