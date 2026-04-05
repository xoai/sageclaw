package rpc

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// handleFileServe serves workspace files for the web dashboard.
// URL pattern: GET /api/files/{relative_path}
// Files are served from the workspace root with path traversal protection.
func (s *Server) handleFileServe(w http.ResponseWriter, r *http.Request) {
	if s.workspace == "" {
		http.Error(w, "workspace not configured", http.StatusNotFound)
		return
	}

	reqPath := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if reqPath == "" {
		http.Error(w, "missing file path", http.StatusBadRequest)
		return
	}

	// Security: clean and validate path stays within workspace.
	cleanPath := filepath.Clean(reqPath)
	if strings.Contains(cleanPath, "..") {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}

	fullPath := filepath.Join(s.workspace, cleanPath)

	absBase, _ := filepath.Abs(s.workspace)
	absFile, _ := filepath.Abs(fullPath)
	if !strings.HasPrefix(absFile, absBase+string(filepath.Separator)) && absFile != absBase {
		http.Error(w, "invalid path", http.StatusForbidden)
		return
	}

	info, err := os.Stat(fullPath)
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	// Set content type based on extension.
	ext := strings.ToLower(filepath.Ext(fullPath))
	contentType := mimeFromExt(ext)
	w.Header().Set("Content-Type", contentType)

	// Inline display for media, attachment for documents.
	disposition := "inline"
	if isDocumentExt(ext) {
		disposition = "attachment"
	}
	// Sanitize filename for Content-Disposition header (strip quotes).
	safeName := strings.ReplaceAll(filepath.Base(fullPath), "\"", "_")
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+safeName+"\"")

	// Generated files are immutable — cache aggressively.
	if strings.Contains(cleanPath, "generated") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}

	http.ServeFile(w, r, fullPath)
}

func mimeFromExt(ext string) string {
	switch ext {
	// Images
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	// Audio
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".m4a":
		return "audio/mp4"
	// Video
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".mov":
		return "video/quicktime"
	// Documents
	case ".pdf":
		return "application/pdf"
	case ".csv":
		return "text/csv"
	case ".txt":
		return "text/plain"
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".md":
		return "text/markdown"
	case ".html":
		return "text/html"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

func isDocumentExt(ext string) bool {
	switch ext {
	case ".pdf", ".csv", ".txt", ".json", ".yaml", ".yml", ".md", ".html", ".zip":
		return true
	default:
		return false
	}
}
