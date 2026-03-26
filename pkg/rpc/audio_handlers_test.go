package rpc

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHandleAudioServe(t *testing.T) {
	dir := t.TempDir()

	// Create a test audio file.
	sessionDir := filepath.Join(dir, "session1")
	os.MkdirAll(sessionDir, 0o755)
	os.WriteFile(filepath.Join(sessionDir, "msg1.ogg"), []byte("fake-ogg"), 0o644)

	srv := &Server{audioBasePath: dir}

	tests := []struct {
		name       string
		path       string
		wantStatus int
		wantType   string
	}{
		{"valid ogg", "/api/audio/session1/msg1.ogg", 200, "audio/ogg"},
		{"not found", "/api/audio/session1/missing.ogg", 404, ""},
		{"path traversal", "/api/audio/../../../etc/passwd", 403, ""},
		{"empty path", "/api/audio/", 400, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
			w := httptest.NewRecorder()
			srv.handleAudioServe(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantType != "" && w.Header().Get("Content-Type") != tt.wantType {
				t.Errorf("content-type = %q, want %q", w.Header().Get("Content-Type"), tt.wantType)
			}
		})
	}
}

func TestHandleAudioServe_NoBasePath(t *testing.T) {
	srv := &Server{audioBasePath: ""}

	req := httptest.NewRequest("GET", "/api/audio/test.ogg", nil)
	w := httptest.NewRecorder()
	srv.handleAudioServe(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no base path", w.Code)
	}
}
