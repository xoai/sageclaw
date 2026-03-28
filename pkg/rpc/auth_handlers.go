package rpc

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/xoai/sageclaw/pkg/auth"
)

func (s *Server) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, map[string]string{"state": "ready"}) // No auth configured.
		return
	}

	if !s.auth.IsSetup() {
		writeJSON(w, map[string]string{"state": "setup"})
		return
	}

	// Check if already authenticated via cookie.
	cookie, err := r.Cookie("sage-auth")
	if err == nil && cookie.Value != "" {
		if err := s.auth.Verify(cookie.Value); err == nil {
			result := map[string]any{"state": "ready"}
			if s.totp != nil {
				result["totp_enabled"] = s.totp.IsEnabled()
			}
			writeJSON(w, result)
			return
		}
	}

	writeJSON(w, map[string]string{"state": "login"})
}

func (s *Server) handleAuthSetup(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		http.Error(w, "auth not configured", http.StatusInternalServerError)
		return
	}

	var params struct {
		Password string `json:"password"`
		Confirm  string `json:"confirm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if params.Password != params.Confirm {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "passwords do not match"})
		return
	}

	if err := s.auth.Setup(params.Password); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Auto-login after setup.
	token, err := s.auth.Login(params.Password)
	if err != nil {
		writeJSON(w, map[string]string{"error": "setup succeeded but login failed"})
		return
	}

	setAuthCookie(w, token, s.jwtExpiry())
	writeJSON(w, map[string]string{"state": "ready"})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, map[string]string{"state": "ready"})
		return
	}

	// Rate limiting.
	if s.loginLimiter != nil {
		trustProxy := s.tunnel != nil && s.tunnel.GetStatus().Running
		ip := auth.ClientIP(r, trustProxy)
		if !s.loginLimiter.Allow(ip) {
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(w, map[string]string{"error": "too many attempts, try again later"})
			return
		}
	}

	var params struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	// Verify password.
	token, err := s.auth.LoginWithExpiry(params.Password, s.jwtExpiry())
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "wrong password"})
		return
	}

	// If TOTP is enabled, require second factor.
	if s.totp != nil && s.totp.IsEnabled() {
		// Don't set the auth cookie yet — generate a server-side nonce.
		// The frontend calls /api/auth/login/totp with this nonce + TOTP code.
		// The nonce proves password was verified; it is NOT a valid JWT.
		nonce := generateNonce()
		s.mu.Lock()
		// Hard cap + lazy cleanup to prevent memory exhaustion.
		if len(s.pendingTOTP) >= 100 {
			for k, v := range s.pendingTOTP {
				if time.Now().After(v.expiresAt) {
					delete(s.pendingTOTP, k)
				}
			}
		}
		if len(s.pendingTOTP) < 100 {
			s.pendingTOTP[nonce] = pendingTOTPEntry{expiresAt: time.Now().Add(3 * time.Minute)}
		}
		s.mu.Unlock()

		writeJSON(w, map[string]any{
			"status": "totp_required",
			"nonce":  nonce,
		})
		return
	}

	setAuthCookie(w, token, s.jwtExpiry())
	writeJSON(w, map[string]string{"state": "ready"})
}

func (s *Server) handleAuthLoginTOTP(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "TOTP not configured"})
		return
	}

	// Rate limiting (same limiter as password login).
	if s.loginLimiter != nil {
		trustProxy := s.tunnel != nil && s.tunnel.GetStatus().Running
		ip := auth.ClientIP(r, trustProxy)
		if !s.loginLimiter.Allow(ip) {
			w.WriteHeader(http.StatusTooManyRequests)
			writeJSON(w, map[string]string{"error": "too many attempts, try again later"})
			return
		}
	}

	var params struct {
		Code  string `json:"code"`
		Nonce string `json:"nonce"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	// Validate the server-side nonce (proves password was verified).
	s.mu.Lock()
	entry, ok := s.pendingTOTP[params.Nonce]
	if ok {
		delete(s.pendingTOTP, params.Nonce) // Single-use.
	}
	s.mu.Unlock()

	if !ok || time.Now().After(entry.expiresAt) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid or expired nonce"})
		return
	}

	// Verify TOTP code.
	if !s.totp.Verify(params.Code) {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "invalid TOTP code"})
		return
	}

	// TOTP verified — issue a fresh JWT (password already verified via nonce).
	expiry := s.jwtExpiry()
	token, err := s.auth.SignToken(expiry)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to issue token"})
		return
	}

	setAuthCookie(w, token, expiry)
	writeJSON(w, map[string]string{"state": "ready"})
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "TOTP not available"})
		return
	}

	var params struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	secret, uri, err := s.totp.Setup(params.Password)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{
		"secret": secret,
		"uri":    uri,
	})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	if s.totp == nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "TOTP not available"})
		return
	}

	var params struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid request"})
		return
	}

	if err := s.totp.Disable(params.Password); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, map[string]string{"status": "disabled"})
}

func (s *Server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sage-auth",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	writeJSON(w, map[string]string{"state": "login"})
}

func setAuthCookie(w http.ResponseWriter, token string, expiry time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sage-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(expiry / time.Second),
	})
}

// jwtExpiry returns the appropriate JWT expiry based on tunnel state.
// When tunnel is active (internet-exposed), use shorter 4h expiry.
// When tunnel is off (LAN only), use standard 24h expiry.
func (s *Server) jwtExpiry() time.Duration {
	if s.tunnel != nil && s.tunnel.GetStatus().Running {
		return 4 * time.Hour
	}
	return 24 * time.Hour
}

// generateNonce creates a 16-byte random hex nonce for TOTP flow.
func generateNonce() string {
	b := make([]byte, 16)
	io.ReadFull(rand.Reader, b)
	return hex.EncodeToString(b)
}

// authGuard wraps a handler with JWT cookie verification.
func (s *Server) authGuard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			// Fail closed: if auth failed to initialize, reject all requests.
			w.WriteHeader(http.StatusServiceUnavailable)
			writeJSON(w, map[string]string{"error": "auth not available"})
			return
		}

		cookie, err := r.Cookie("sage-auth")
		if err != nil || cookie.Value == "" {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"error": "unauthorized"})
			return
		}

		if err := s.auth.Verify(cookie.Value); err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			writeJSON(w, map[string]string{"error": "session expired"})
			return
		}

		next(w, r)
	}
}
