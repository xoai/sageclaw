package provider

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// FailoverReason classifies why a provider request failed.
type FailoverReason string

const (
	ReasonRateLimit       FailoverReason = "rate_limit"
	ReasonOverloaded      FailoverReason = "overloaded"
	ReasonBilling         FailoverReason = "billing"
	ReasonAuth            FailoverReason = "auth"
	ReasonTimeout         FailoverReason = "timeout"
	ReasonContextOverflow FailoverReason = "context_overflow"
)

// ProviderError is a classified error from an LLM provider.
// It implements the error interface so DoWithRetry can return it
// without changing its (*http.Response, error) signature.
// Callers extract it via errors.As(err, &provErr).
type ProviderError struct {
	Reason     FailoverReason
	Status     int           // HTTP status code (0 for network errors).
	Provider   string        // e.g. "anthropic", "openai"
	Model      string        // e.g. "claude-sonnet-4-20250514"
	RetryAfter time.Duration // From Retry-After header, 0 if not set.
	Err        error         // Wrapped original error.
}

func (e *ProviderError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s error (HTTP %d, %s): %v", e.Provider, e.Status, e.Reason, e.Err)
	}
	return fmt.Sprintf("%s error (%s): %v", e.Provider, e.Reason, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// ClassifyHTTPError maps an HTTP status code and response body to a FailoverReason.
func ClassifyHTTPError(status int, body string) FailoverReason {
	switch {
	case status == 429:
		return ReasonRateLimit
	case status == 402:
		return ReasonBilling
	case status == 401 || status == 403:
		return ReasonAuth
	case status == 413 || strings.Contains(body, "context_length_exceeded") ||
		strings.Contains(body, "max_tokens") || strings.Contains(body, "too many tokens"):
		return ReasonContextOverflow
	case status >= 500 && status <= 504:
		return ReasonOverloaded
	case status == 408:
		return ReasonTimeout
	default:
		// Check body for common patterns.
		lower := strings.ToLower(body)
		if strings.Contains(lower, "rate limit") || strings.Contains(lower, "resource_exhausted") ||
			strings.Contains(lower, "throttled") {
			return ReasonRateLimit
		}
		if strings.Contains(lower, "overloaded") || strings.Contains(lower, "capacity") {
			return ReasonOverloaded
		}
		if strings.Contains(lower, "billing") || strings.Contains(lower, "quota exceeded") {
			return ReasonBilling
		}
		return ReasonOverloaded // Default for unknown server errors.
	}
}

// EnrichError sets the Provider and Model fields on a ProviderError if the
// given error is one. Returns the original error unchanged if not a ProviderError.
// Clients call this to add context after DoWithRetry or direct HTTP calls.
func EnrichError(err error, providerName, model string) error {
	var pe *ProviderError
	if errors.As(err, &pe) {
		pe.Provider = providerName
		pe.Model = model
	}
	return err
}

// NewHTTPError creates a ProviderError from an HTTP response for paths
// that don't go through DoWithRetry (e.g., ChatStream).
func NewHTTPError(status int, body string, providerName, model string) *ProviderError {
	return &ProviderError{
		Reason:   ClassifyHTTPError(status, body),
		Status:   status,
		Provider: providerName,
		Model:    model,
		Err:      fmt.Errorf("HTTP %d: %s", status, body),
	}
}

// IsFailoverEligible returns true if the failure reason should trigger
// a combo/tier fallback rather than a hard failure.
func IsFailoverEligible(reason FailoverReason) bool {
	switch reason {
	case ReasonRateLimit, ReasonOverloaded, ReasonTimeout:
		return true
	default:
		return false
	}
}
