package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	maxAttemptsPerMinute = 5
	bucketExpiry         = 10 * time.Minute
)

// LoginLimiter rate-limits login attempts per IP address.
type LoginLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	count    int
	resetAt  time.Time
	lastSeen time.Time
}

// NewLoginLimiter creates a rate limiter.
func NewLoginLimiter() *LoginLimiter {
	return &LoginLimiter{
		buckets: make(map[string]*bucket),
	}
}

// Allow returns true if a login attempt from this IP is allowed.
func (l *LoginLimiter) Allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()

	// Lazy cleanup of expired buckets.
	if len(l.buckets) > 100 {
		for k, b := range l.buckets {
			if now.Sub(b.lastSeen) > bucketExpiry {
				delete(l.buckets, k)
			}
		}
	}

	b, ok := l.buckets[ip]
	if !ok {
		l.buckets[ip] = &bucket{
			count:    1,
			resetAt:  now.Add(time.Minute),
			lastSeen: now,
		}
		return true
	}

	b.lastSeen = now

	// Reset if the minute window has passed.
	if now.After(b.resetAt) {
		b.count = 1
		b.resetAt = now.Add(time.Minute)
		return true
	}

	b.count++
	return b.count <= maxAttemptsPerMinute
}

// ClientIP extracts the real client IP from an HTTP request.
// When trustProxy is true (tunnel active), reads X-Forwarded-For.
// Otherwise uses RemoteAddr directly to prevent header spoofing.
func ClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// First IP in the chain is the original client.
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
	}

	// Extract IP from RemoteAddr ("ip:port").
	ip := r.RemoteAddr
	if idx := strings.LastIndex(ip, ":"); idx > 0 {
		ip = ip[:idx]
	}
	return ip
}
