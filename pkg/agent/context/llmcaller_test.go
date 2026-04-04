package context

import "testing"

func TestResolveMechanismModel_MechanismOverride(t *testing.T) {
	mechs := map[string]string{"snip": "gemini-2.5-flash-lite", "review": "claude-haiku-4.5"}
	got := ResolveMechanismModel(mechs, "gpt-4o-mini", "snip")
	if got != "gemini-2.5-flash-lite" {
		t.Fatalf("expected snip override, got %q", got)
	}
}

func TestResolveMechanismModel_FallbackToUtility(t *testing.T) {
	mechs := map[string]string{"review": "claude-haiku-4.5"}
	// "compact" is not in mechanism map — should fall back to utility.
	got := ResolveMechanismModel(mechs, "gpt-4o-mini", "compact")
	if got != "gpt-4o-mini" {
		t.Fatalf("expected utility fallback, got %q", got)
	}
}

func TestResolveMechanismModel_AutoWhenNoOverrides(t *testing.T) {
	got := ResolveMechanismModel(nil, "", "snip")
	if got != "" {
		t.Fatalf("expected empty (auto), got %q", got)
	}
}

func TestResolveMechanismModel_AutoUtilityIgnored(t *testing.T) {
	got := ResolveMechanismModel(nil, "auto", "snip")
	if got != "" {
		t.Fatalf("expected empty (auto), got %q", got)
	}
}

func TestResolveMechanismModel_MechanismAutoFallsThrough(t *testing.T) {
	mechs := map[string]string{"snip": "auto"}
	got := ResolveMechanismModel(mechs, "gpt-4o-mini", "snip")
	if got != "gpt-4o-mini" {
		t.Fatalf("expected utility fallback when mechanism is 'auto', got %q", got)
	}
}

func TestResolveMechanismModel_EmptyMechanismFallsThrough(t *testing.T) {
	mechs := map[string]string{"snip": ""}
	got := ResolveMechanismModel(mechs, "gpt-4o-mini", "snip")
	if got != "gpt-4o-mini" {
		t.Fatalf("expected utility fallback when mechanism is empty, got %q", got)
	}
}
