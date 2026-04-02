package rpc

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxFileUploadSize = 30 << 20 // 30 MB

// fileUploadExtensions is the server-side allowlist for chat file uploads.
// SVG excluded — stored XSS vector via embedded JavaScript.
var fileUploadExtensions = map[string]bool{
	// Text
	".txt": true, ".md": true, ".csv": true, ".json": true,
	".yaml": true, ".yml": true, ".xml": true, ".toml": true,
	// Code
	".html": true, ".css": true, ".js": true, ".ts": true,
	".jsx": true, ".tsx": true, ".go": true, ".py": true,
	".rs": true, ".java": true, ".sh": true,
	// Docs
	".pdf": true,
	// Images (SVG excluded)
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
}

// handleFileUpload handles POST /api/upload.
// Accepts multipart form with "file" and optional "session_id" fields.
// Validates: extension allowlist, size limit, filename sanitization.
// Saves to {workspace}/uploads/{session_id}/{sanitized_filename}.
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if s.workspace == "" {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "workspace not configured"})
		return
	}

	// Enforce size limit before reading the full body.
	r.Body = http.MaxBytesReader(w, r.Body, maxFileUploadSize)

	if err := r.ParseMultipartForm(maxFileUploadSize); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("file too large or invalid form: %v", err)})
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "missing file field"})
		return
	}
	defer file.Close()

	sessionID := r.FormValue("session_id")
	if sessionID == "" {
		sessionID = "unsorted"
	}

	// Validate extension.
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if !fileUploadExtensions[ext] {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("file type %q not allowed", ext)})
		return
	}

	// Sanitize filename: keep only alphanumeric, dots, hyphens, underscores.
	safeName := sanitizeFilename(header.Filename)
	if safeName == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "invalid filename"})
		return
	}

	// Sanitize session ID (prevent path traversal).
	safeSession := sanitizeFilename(sessionID)
	if safeSession == "" {
		safeSession = "unsorted"
	}

	// Create upload directory.
	uploadDir := filepath.Join(s.workspace, "uploads", safeSession)
	if err := os.MkdirAll(uploadDir, 0755); err != nil {
		log.Printf("[upload] failed to create dir %s: %v", uploadDir, err)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to create upload directory"})
		return
	}

	// Resolve final path and verify it's within workspace (defense-in-depth).
	destPath := filepath.Join(uploadDir, safeName)
	absPath, err := filepath.Abs(destPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "path resolution failed"})
		return
	}
	absWorkspace, _ := filepath.Abs(s.workspace)
	if !strings.HasPrefix(absPath, absWorkspace+string(filepath.Separator)) {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "path traversal denied"})
		return
	}

	// Write file.
	dst, err := os.Create(destPath)
	if err != nil {
		log.Printf("[upload] failed to create file %s: %v", destPath, err)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to save file"})
		return
	}
	defer dst.Close()

	written, err := io.Copy(dst, file)
	if err != nil {
		os.Remove(destPath) // Clean up partial write.
		log.Printf("[upload] write failed: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]string{"error": "failed to write file"})
		return
	}

	// Compute workspace-relative path for the agent.
	relPath, _ := filepath.Rel(s.workspace, absPath)

	log.Printf("[upload] saved %s (%d bytes) to %s", safeName, written, relPath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"path": relPath,
		"name": safeName,
		"size": written,
		"type": ext,
	})
}

// sanitizeFilename removes path separators and special characters,
// keeping only safe characters for filenames.
func sanitizeFilename(name string) string {
	// Strip directory components.
	name = filepath.Base(name)
	if name == "." || name == ".." {
		return ""
	}

	// Replace unsafe characters with underscore.
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case r == '.', r == '-', r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}

	result := sb.String()
	// Prevent hidden files.
	if strings.HasPrefix(result, ".") {
		result = "_" + result[1:]
	}
	return result
}
