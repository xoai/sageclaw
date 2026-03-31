package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDoWithRetry_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	resp, err := DoWithRetry(srv.Client(), req, DefaultRetryConfig())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestDoWithRetry_RetryOn429(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	cfg := DefaultRetryConfig()
	cfg.RateBackoff = 0 // No delay in tests.
	cfg.BaseBackoff = 0

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	resp, err := DoWithRetry(srv.Client(), req, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("expected 200 after retries, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_NoRetryOn401(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(401)
		w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	_, err := DoWithRetry(srv.Client(), req, DefaultRetryConfig())
	if err == nil {
		t.Fatal("expected error on 401")
	}
	// Should return ProviderError with auth reason, no retries.
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if pe.Reason != ReasonAuth {
		t.Errorf("expected reason=auth, got %q", pe.Reason)
	}
	if pe.Status != 401 {
		t.Errorf("expected status=401, got %d", pe.Status)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry on 401), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_RetryOn500(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cfg := DefaultRetryConfig()
	cfg.BaseBackoff = 0

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	resp, err := DoWithRetry(srv.Client(), req, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDoWithRetry_ExhaustedRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 2, BaseBackoff: 0}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	_, err := DoWithRetry(srv.Client(), req, cfg)
	if err == nil {
		t.Fatal("expected error after exhausted retries")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.Reason != ReasonOverloaded {
		t.Errorf("expected reason=overloaded for 503, got %q", pe.Reason)
	}
	if pe.Status != 503 {
		t.Errorf("expected status=503, got %d", pe.Status)
	}
}

func TestDoWithRetry_429ReturnsProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(429)
	}))
	defer srv.Close()

	cfg := RetryConfig{MaxAttempts: 2, BaseBackoff: 0, RateBackoff: 0}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	_, err := DoWithRetry(srv.Client(), req, cfg)
	if err == nil {
		t.Fatal("expected error")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.Reason != ReasonRateLimit {
		t.Errorf("expected reason=rate_limit, got %q", pe.Reason)
	}
	if pe.RetryAfter != 30*time.Second {
		t.Errorf("expected RetryAfter=30s, got %v", pe.RetryAfter)
	}
}

func TestDoWithRetry_NetworkError(t *testing.T) {
	// Connect to a port that's not listening.
	cfg := RetryConfig{MaxAttempts: 2, BaseBackoff: 0}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "http://127.0.0.1:1", nil)
	_, err := DoWithRetry(client, req, cfg)
	if err == nil {
		t.Fatal("expected error on network failure")
	}
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T: %v", err, err)
	}
	if pe.Reason != ReasonTimeout {
		t.Errorf("expected reason=timeout for network error, got %q", pe.Reason)
	}
	if pe.Status != 0 {
		t.Errorf("expected status=0 for network error, got %d", pe.Status)
	}
}

func TestDoWithRetry_402ReturnsBilling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(402)
		w.Write([]byte("payment required"))
	}))
	defer srv.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", srv.URL, nil)
	_, err := DoWithRetry(srv.Client(), req, DefaultRetryConfig())
	var pe *ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *ProviderError, got %T", err)
	}
	if pe.Reason != ReasonBilling {
		t.Errorf("expected reason=billing, got %q", pe.Reason)
	}
}

func TestIsFallbackEligible(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{200, false},
		{401, false},
		{403, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{504, true},
	}
	for _, tc := range cases {
		if got := IsFallbackEligible(tc.status); got != tc.want {
			t.Errorf("IsFallbackEligible(%d) = %v, want %v", tc.status, got, tc.want)
		}
	}
}
