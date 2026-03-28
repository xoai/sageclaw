// Package livesession manages persistent LiveSession connections.
//
// Sessions are pooled per session ID and reused across voice messages
// for lower latency. Idle sessions are closed after a configurable timeout.
package livesession

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/provider"
)

const (
	DefaultIdleTimeout = 5 * time.Minute
)

// Pool manages persistent LiveSession instances keyed by session ID.
type Pool struct {
	provider    provider.LiveProvider
	idleTimeout time.Duration

	mu       sync.Mutex
	sessions map[string]*entry
	closed   bool
}

type entry struct {
	session   provider.LiveSession
	config    provider.LiveSessionConfig
	lastUsed  time.Time
	timer     *time.Timer
}

// NewPool creates a LiveSession pool.
func NewPool(lp provider.LiveProvider, idleTimeout time.Duration) *Pool {
	if idleTimeout == 0 {
		idleTimeout = DefaultIdleTimeout
	}
	return &Pool{
		provider:    lp,
		idleTimeout: idleTimeout,
		sessions:    make(map[string]*entry),
	}
}

// GetOrCreate returns an existing session for the given sessionID,
// or creates a new one using the provided config. The session's idle
// timer is reset on each access.
func (p *Pool) GetOrCreate(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, fmt.Errorf("livesession pool: closed")
	}

	if e, ok := p.sessions[sessionID]; ok {
		// Check if config changed (voice name, language, model).
		// If so, close the old session and create a new one.
		if configChanged(e.config, cfg) {
			log.Printf("livesession pool: config changed for %s (voice: %q→%q, lang: %q→%q), recreating session",
				sessionID, e.config.VoiceName, cfg.VoiceName, e.config.LanguageCode, cfg.LanguageCode)
			e.timer.Stop()
			e.session.Close()
			delete(p.sessions, sessionID)
		} else {
			e.lastUsed = time.Now()
			e.timer.Reset(p.idleTimeout)
			sess := e.session
			p.mu.Unlock()
			return sess, nil
		}
	}
	p.mu.Unlock()

	// Open new session (outside lock — may be slow).
	sess, err := p.provider.OpenSession(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("livesession pool: open: %w", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Check again (another goroutine may have created it).
	if e, ok := p.sessions[sessionID]; ok {
		// Close the one we just created; use the existing one.
		sess.Close()
		e.lastUsed = time.Now()
		e.timer.Reset(p.idleTimeout)
		return e.session, nil
	}

	e := &entry{
		session:  sess,
		config:   cfg,
		lastUsed: time.Now(),
	}
	e.timer = time.AfterFunc(p.idleTimeout, func() {
		p.evict(sessionID)
	})
	p.sessions[sessionID] = e

	log.Printf("livesession pool: created session for %s (pool size: %d)", sessionID, len(p.sessions))
	return sess, nil
}

// Warm pre-creates a session in the background so it's ready when voice arrives.
// No-op if a session already exists for the given ID. Safe to call repeatedly.
func (p *Pool) Warm(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if _, ok := p.sessions[sessionID]; ok {
		p.mu.Unlock()
		return // Already warm.
	}
	p.mu.Unlock()

	go func() {
		start := time.Now()
		_, err := p.GetOrCreate(ctx, sessionID, cfg)
		if err != nil {
			log.Printf("livesession pool: warm failed for %s: %v", sessionID, err)
		} else {
			log.Printf("livesession pool: pre-warmed session %s in %dms", sessionID, time.Since(start).Milliseconds())
		}
	}()
}

// Reconnect removes a stale session and creates a fresh one.
// Use this when a session errors out (WebSocket disconnect, GoAway, etc.).
func (p *Pool) Reconnect(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	p.Remove(sessionID)
	return p.GetOrCreate(ctx, sessionID, cfg)
}

// Remove closes and removes a specific session from the pool.
func (p *Pool) Remove(sessionID string) {
	p.mu.Lock()
	e, ok := p.sessions[sessionID]
	if ok {
		delete(p.sessions, sessionID)
	}
	p.mu.Unlock()

	if ok {
		e.timer.Stop()
		e.session.Close()
		log.Printf("livesession pool: removed session %s", sessionID)
	}
}

// Close shuts down all sessions and prevents new ones from being created.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	sessions := make(map[string]*entry, len(p.sessions))
	for k, v := range p.sessions {
		sessions[k] = v
	}
	p.sessions = make(map[string]*entry)
	p.mu.Unlock()

	for id, e := range sessions {
		e.timer.Stop()
		e.session.Close()
		log.Printf("livesession pool: closed session %s (shutdown)", id)
	}
}

// Size returns the current number of pooled sessions.
func (p *Pool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sessions)
}

// evict removes and closes an idle session.
func (p *Pool) evict(sessionID string) {
	p.mu.Lock()
	e, ok := p.sessions[sessionID]
	if ok {
		delete(p.sessions, sessionID)
	}
	p.mu.Unlock()

	if ok {
		e.session.Close()
		log.Printf("livesession pool: evicted idle session %s", sessionID)
	}
}

// configChanged returns true if the session-relevant config has changed.
func configChanged(old, new provider.LiveSessionConfig) bool {
	if old.VoiceName != new.VoiceName {
		return true
	}
	if old.LanguageCode != new.LanguageCode {
		return true
	}
	if old.Model != new.Model {
		return true
	}
	if old.SystemPrompt != new.SystemPrompt {
		return true
	}
	if len(old.Tools) != len(new.Tools) {
		return true
	}
	for i := range old.Tools {
		if old.Tools[i].Name != new.Tools[i].Name {
			return true
		}
	}
	return false
}
