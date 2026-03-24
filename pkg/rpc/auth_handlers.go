package rpc

import (
	"encoding/json"
	"net/http"
	"time"
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
			writeJSON(w, map[string]string{"state": "ready"})
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

	setAuthCookie(w, token)
	writeJSON(w, map[string]string{"state": "ready"})
}

func (s *Server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		writeJSON(w, map[string]string{"state": "ready"})
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

	token, err := s.auth.Login(params.Password)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		writeJSON(w, map[string]string{"error": "wrong password"})
		return
	}

	setAuthCookie(w, token)
	writeJSON(w, map[string]string{"state": "ready"})
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

func setAuthCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "sage-auth",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(24 * time.Hour / time.Second),
	})
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
