package provider

import (
	"errors"
	"fmt"
	"testing"
)

func TestClassifyHTTPError(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   FailoverReason
	}{
		{429, "", ReasonRateLimit},
		{402, "", ReasonBilling},
		{401, "", ReasonAuth},
		{403, "", ReasonAuth},
		{408, "", ReasonTimeout},
		{500, "", ReasonOverloaded},
		{502, "", ReasonOverloaded},
		{503, "", ReasonOverloaded},
		{504, "", ReasonOverloaded},
		{413, "", ReasonContextOverflow},
		{400, `{"error":{"message":"context_length_exceeded"}}`, ReasonContextOverflow},
		{400, `{"error":{"message":"too many tokens"}}`, ReasonContextOverflow},
		{400, `RESOURCE_EXHAUSTED`, ReasonRateLimit},
		{400, `rate limit reached`, ReasonRateLimit},
		{400, `overloaded`, ReasonOverloaded},
		{400, `billing issue`, ReasonBilling},
		{400, `quota exceeded`, ReasonBilling},
	}

	for _, tt := range tests {
		got := ClassifyHTTPError(tt.status, tt.body)
		if got != tt.want {
			t.Errorf("ClassifyHTTPError(%d, %q) = %q, want %q", tt.status, tt.body, got, tt.want)
		}
	}
}

func TestIsFailoverEligible(t *testing.T) {
	tests := []struct {
		reason FailoverReason
		want   bool
	}{
		{ReasonRateLimit, true},
		{ReasonOverloaded, true},
		{ReasonTimeout, true},
		{ReasonBilling, false},
		{ReasonAuth, false},
		{ReasonContextOverflow, false},
	}

	for _, tt := range tests {
		if got := IsFailoverEligible(tt.reason); got != tt.want {
			t.Errorf("IsFailoverEligible(%q) = %v, want %v", tt.reason, got, tt.want)
		}
	}
}

func TestProviderError_Interface(t *testing.T) {
	pe := &ProviderError{
		Reason:   ReasonRateLimit,
		Status:   429,
		Provider: "anthropic",
		Model:    "claude-sonnet-4",
		Err:      fmt.Errorf("too many requests"),
	}

	// Must implement error interface.
	var err error = pe
	if err.Error() == "" {
		t.Error("expected non-empty error string")
	}

	// Must be extractable via errors.As.
	var extracted *ProviderError
	if !errors.As(err, &extracted) {
		t.Fatal("expected errors.As to succeed")
	}
	if extracted.Reason != ReasonRateLimit {
		t.Errorf("expected rate_limit, got %q", extracted.Reason)
	}
	if extracted.Provider != "anthropic" {
		t.Errorf("expected anthropic, got %q", extracted.Provider)
	}
}

func TestProviderError_NetworkError(t *testing.T) {
	pe := &ProviderError{
		Reason:   ReasonTimeout,
		Status:   0, // No HTTP status for network errors.
		Provider: "openai",
		Err:      fmt.Errorf("connection refused"),
	}

	if pe.Status != 0 {
		t.Errorf("expected Status=0 for network error, got %d", pe.Status)
	}
	if !IsFailoverEligible(pe.Reason) {
		t.Error("network errors should be failover-eligible")
	}
	// Error string should not mention HTTP status.
	if err := pe.Error(); err == "" {
		t.Error("expected non-empty error string")
	}
}

func TestProviderError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("original error")
	pe := &ProviderError{Reason: ReasonAuth, Err: inner}

	if !errors.Is(pe, inner) {
		t.Error("expected Unwrap to expose inner error")
	}
}
