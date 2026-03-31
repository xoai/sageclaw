package provider

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RetryConfig configures retry behavior for HTTP requests.
type RetryConfig struct {
	MaxAttempts int           // Maximum number of attempts (default 4).
	BaseBackoff time.Duration // Base backoff for 5xx/network errors (default 1s).
	RateBackoff time.Duration // Base backoff for 429 rate limits (default 5s).
	MaxBackoff  time.Duration // Maximum backoff duration (default 60s).
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 4,
		BaseBackoff: 1 * time.Second,
		RateBackoff: 5 * time.Second,
		MaxBackoff:  60 * time.Second,
	}
}

// DoWithRetry executes an HTTP request with automatic retries on transient errors.
// Retries on: 429 (rate limit), 500, 502, 503, 504.
// Parses Retry-After header for 429 responses.
//
// On failure, the returned error is a *ProviderError (extractable via errors.As)
// with a classified FailoverReason. Non-retryable errors (401, 402, 403, 413)
// are returned immediately without retrying.
//
// The caller is responsible for closing the returned response body on success.
func DoWithRetry(client *http.Client, req *http.Request, cfg RetryConfig) (*http.Response, error) {
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 4
	}

	// Save body bytes so we can replay on retries.
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("reading request body: %w", err)
		}
	}

	var lastErr error
	var lastStatus int
	var lastRetryAfter time.Duration

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		// Reset body reader for each attempt.
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		if attempt > 0 {
			backoff := retryBackoff(attempt, lastErr, cfg)
			select {
			case <-time.After(backoff):
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			// Network error (DNS, connection refused, TLS, timeout).
			lastErr = err
			lastStatus = 0
			continue
		}

		// Success or non-retryable error.
		if !isRetryable(resp.StatusCode) {
			// Non-retryable HTTP errors → return ProviderError immediately.
			if resp.StatusCode >= 400 {
				body, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				return nil, &ProviderError{
					Reason: ClassifyHTTPError(resp.StatusCode, string(body)),
					Status: resp.StatusCode,
					Err:    fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateBody(body)),
				}
			}
			return resp, nil
		}

		// Retryable error — capture status and Retry-After for backoff.
		lastStatus = resp.StatusCode
		lastRetryAfter = parseRetryAfter(resp)
		lastErr = retryError(resp)
		resp.Body.Close()
	}

	// All retries exhausted — return classified ProviderError.
	if lastStatus > 0 {
		return nil, &ProviderError{
			Reason:     ClassifyHTTPError(lastStatus, ""),
			Status:     lastStatus,
			RetryAfter: lastRetryAfter,
			Err:        fmt.Errorf("all %d retries exhausted: %w", cfg.MaxAttempts, lastErr),
		}
	}
	// Network error — no HTTP status.
	return nil, &ProviderError{
		Reason: ReasonTimeout,
		Status: 0,
		Err:    fmt.Errorf("all %d retries exhausted (network): %w", cfg.MaxAttempts, lastErr),
	}
}

// parseRetryAfter extracts the Retry-After duration from a response header.
func parseRetryAfter(resp *http.Response) time.Duration {
	ra := resp.Header.Get("Retry-After")
	if ra == "" {
		return 0
	}
	if secs, err := strconv.Atoi(ra); err == nil {
		return time.Duration(secs) * time.Second
	}
	return 0
}

// truncateBody returns the first 200 bytes of a response body for error messages.
func truncateBody(body []byte) string {
	if len(body) > 200 {
		return string(body[:200]) + "..."
	}
	return string(body)
}

// IsFallbackEligible returns true if the HTTP status code indicates the
// request should be retried on a different provider (for router use).
func IsFallbackEligible(status int) bool {
	switch status {
	case 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

func isRetryable(status int) bool {
	return status == 429 || status >= 500
}

func retryError(resp *http.Response) error {
	if resp.StatusCode == 429 {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := strconv.Atoi(ra); err == nil {
				return fmt.Errorf("HTTP 429 (retry-after: %ds)", secs)
			}
		}
		return fmt.Errorf("HTTP 429")
	}
	return fmt.Errorf("HTTP %d", resp.StatusCode)
}

func retryBackoff(attempt int, lastErr error, cfg RetryConfig) time.Duration {
	if lastErr != nil {
		errStr := lastErr.Error()

		// Parse "HTTP 429 (retry-after: Ns)" from our formatted error.
		if strings.Contains(errStr, "retry-after:") {
			var secs int
			if _, err := fmt.Sscanf(errStr, "HTTP 429 (retry-after: %ds)", &secs); err == nil && secs >= 0 {
				d := time.Duration(secs) * time.Second
				if d > cfg.MaxBackoff {
					d = cfg.MaxBackoff
				}
				return d
			}
		}

		// 429 without Retry-After: longer base backoff with jitter.
		if strings.Contains(errStr, "429") {
			base := cfg.RateBackoff
			if base == 0 {
				base = 5 * time.Second
			}
			d := base * time.Duration(1<<uint(attempt-1))
			return addJitter(d, cfg.MaxBackoff)
		}
	}

	// 5xx or network errors: exponential backoff with jitter.
	base := cfg.BaseBackoff
	if base == 0 {
		base = 1 * time.Second
	}
	d := base * time.Duration(1<<uint(attempt-1))
	return addJitter(d, cfg.MaxBackoff)
}

// addJitter adds ±25% jitter to prevent thundering herd, capped at max.
func addJitter(d, max time.Duration) time.Duration {
	if d > max {
		d = max
	}
	jitter := time.Duration(rand.Int63n(int64(d/4)+1)) - d/8
	d += jitter
	if d < 0 {
		d = 100 * time.Millisecond
	}
	return d
}
