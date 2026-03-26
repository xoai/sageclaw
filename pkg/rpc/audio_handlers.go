package rpc

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleAudioServe serves audio files from the audio storage directory.
// URL pattern: GET /api/audio/{file_path}
// The file_path is relative to the audio base path.
func (s *Server) handleAudioServe(w http.ResponseWriter, r *http.Request) {
	if s.audioBasePath == "" {
		http.Error(w, "audio storage not configured", http.StatusNotFound)
		return
	}

	// Extract the file path from the URL: /api/audio/{path}
	reqPath := strings.TrimPrefix(r.URL.Path, "/api/audio/")
	if reqPath == "" {
		http.Error(w, "missing file path", http.StatusBadRequest)
		return
	}

	// Security: clean the path and ensure it stays within audioBasePath.
	cleanPath := filepath.Clean(reqPath)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}

	fullPath := filepath.Join(s.audioBasePath, cleanPath)

	// Verify the resolved path is still under audioBasePath.
	absBase, _ := filepath.Abs(s.audioBasePath)
	absFile, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFile, absBase) {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}

	// Check file exists.
	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Set content type based on extension.
	ext := strings.ToLower(filepath.Ext(fullPath))
	switch ext {
	case ".ogg", ".oga":
		w.Header().Set("Content-Type", "audio/ogg")
	case ".mp4":
		w.Header().Set("Content-Type", "audio/mp4")
	case ".wav":
		w.Header().Set("Content-Type", "audio/wav")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}

	// Allow caching — audio files don't change.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")

	http.ServeFile(w, r, fullPath)
}
