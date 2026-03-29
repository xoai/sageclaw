package rpc

import (
	"archive/zip"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	maxUploadSize        = 5 << 20  // 5 MB
	maxZipFiles          = 50
	maxZipUncompressed   = 10 << 20 // 10 MB
	maxFilenameLen       = 128
	pendingUploadTTL     = 10 * time.Minute
)

var (
	validSkillName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

	allowedExtensions = map[string]bool{
		".md": true, ".yaml": true, ".yml": true, ".json": true,
		".sh": true, ".py": true, ".js": true, ".txt": true,
		".toml": true, ".png": true, ".svg": true, ".css": true,
		".html": true,
	}

	scriptExtensions = map[string]bool{
		".sh": true, ".py": true, ".js": true,
	}
)

// pendingUpload tracks an uploaded skill awaiting script approval.
type pendingUpload struct {
	tempDir    string
	name       string
	hasScripts bool
	readme     string
	files      []string
	scripts    []scriptEntry
	createdAt  time.Time
}

type scriptEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// pendingUploads stores uploads awaiting approval, keyed by upload ID.
var pendingUploads sync.Map

// handleSkillUpload handles multipart file upload of .md or .zip skill files.
func (s *Server) handleSkillUpload(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	// Limit request body size.
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize+1024) // +1K for form overhead

	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "file too large (max 5 MB)"})
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "no file provided"})
		return
	}
	defer file.Close()

	// Read file into memory (already size-limited).
	data, err := io.ReadAll(file)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "failed to read file"})
		return
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))

	var tempDir string
	var skillName string

	switch ext {
	case ".md":
		tempDir, skillName, err = extractMD(data, header.Filename)
	case ".zip":
		tempDir, skillName, err = extractZIP(data)
	default:
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "unsupported file type (use .md or .zip)"})
		return
	}

	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Validate skill name.
	if !validSkillName.MatchString(skillName) {
		os.RemoveAll(tempDir)
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": fmt.Sprintf("invalid skill name %q (use lowercase alphanumeric + hyphens, max 64 chars)", skillName)})
		return
	}

	// Scan files and detect scripts.
	files, scripts, readme := scanSkillDir(tempDir)
	hasScripts := len(scripts) > 0

	// Parse agents from form field.
	var agents []string
	if agentsJSON := r.FormValue("agents"); agentsJSON != "" {
		json.Unmarshal([]byte(agentsJSON), &agents)
	}

	if hasScripts {
		// Two-phase: store pending, return review response.
		uploadID := generateUploadID()
		pending := &pendingUpload{
			tempDir:    tempDir,
			name:       skillName,
			hasScripts: true,
			readme:     readme,
			files:      files,
			scripts:    scripts,
			createdAt:  time.Now(),
		}
		pendingUploads.Store(uploadID, pending)

		// TTL cleanup goroutine.
		go func() {
			time.Sleep(pendingUploadTTL)
			if v, loaded := pendingUploads.LoadAndDelete(uploadID); loaded {
				p := v.(*pendingUpload)
				os.RemoveAll(p.tempDir)
				log.Printf("skill-upload: expired pending upload %s (%s)", uploadID, p.name)
			}
		}()

		writeJSON(w, map[string]any{
			"status":     "review",
			"upload_id":  uploadID,
			"name":       skillName,
			"hasScripts": true,
			"scripts":    scripts,
			"readme":     readme,
			"files":      files,
		})
		return
	}

	// No scripts — install immediately.
	installed, err := s.skillStore.ImportLocal(tempDir, skillName)
	os.RemoveAll(tempDir) // clean up temp
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Assign to agents.
	if len(agents) > 0 && s.agentsDir != "" {
		for _, agentID := range agents {
			if validateAgentID(agentID) {
				addSkillToAgent(s.agentsDir, agentID, installed.Name)
			}
		}
		installed.Agents = agents
	}

	sendSIGHUP()
	writeJSON(w, map[string]any{
		"status":     "installed",
		"name":       installed.Name,
		"hasScripts": false,
	})
}

// handleSkillUploadApprove finalizes a pending upload after script review.
func (s *Server) handleSkillUploadApprove(w http.ResponseWriter, r *http.Request) {
	if s.skillStore == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		writeJSON(w, map[string]string{"error": "skill store not configured"})
		return
	}

	var req struct {
		UploadID string   `json:"upload_id"`
		Agents   []string `json:"agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.UploadID == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "upload_id is required"})
		return
	}

	v, ok := pendingUploads.LoadAndDelete(req.UploadID)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]string{"error": "upload not found or expired"})
		return
	}

	pending := v.(*pendingUpload)

	installed, err := s.skillStore.ImportLocal(pending.tempDir, pending.name)
	os.RemoveAll(pending.tempDir) // clean up temp
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}

	// Assign to agents.
	if len(req.Agents) > 0 && s.agentsDir != "" {
		for _, agentID := range req.Agents {
			if validateAgentID(agentID) {
				addSkillToAgent(s.agentsDir, agentID, installed.Name)
			}
		}
		installed.Agents = req.Agents
	}

	sendSIGHUP()
	writeJSON(w, map[string]any{
		"status":     "installed",
		"name":       installed.Name,
		"hasScripts": pending.hasScripts,
	})
}

// --- Extraction helpers ---

// extractMD creates a temp skill directory from a single .md file.
func extractMD(data []byte, filename string) (tempDir string, name string, err error) {
	// Parse name from frontmatter or filename.
	name = parseFrontmatterName(string(data))
	if name == "" {
		name = strings.TrimSuffix(strings.ToLower(filepath.Base(filename)), ".md")
		name = strings.ReplaceAll(name, "_", "-")
		name = strings.ReplaceAll(name, " ", "-")
	}

	tempDir, err = os.MkdirTemp("", "sageclaw-skill-upload-")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(tempDir, "SKILL.md"), data, 0o644); err != nil {
		os.RemoveAll(tempDir)
		return "", "", fmt.Errorf("write SKILL.md: %w", err)
	}

	return tempDir, name, nil
}

// extractZIP validates and extracts a ZIP archive to a temp directory.
func extractZIP(data []byte) (tempDir string, name string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", "", fmt.Errorf("invalid zip file: %w", err)
	}

	if len(zr.File) > maxZipFiles {
		return "", "", fmt.Errorf("zip contains too many files (%d, max %d)", len(zr.File), maxZipFiles)
	}

	// Find SKILL.md — could be at root or one level deep.
	var skillMDPath string
	var stripPrefix string
	for _, f := range zr.File {
		base := filepath.Base(f.Name)
		if strings.EqualFold(base, "SKILL.md") {
			skillMDPath = f.Name
			dir := filepath.Dir(f.Name)
			if dir != "." {
				stripPrefix = dir + "/"
			}
			break
		}
	}
	if skillMDPath == "" {
		return "", "", fmt.Errorf("zip must contain a SKILL.md file")
	}

	tempDir, err = os.MkdirTemp("", "sageclaw-skill-upload-")
	if err != nil {
		return "", "", fmt.Errorf("create temp dir: %w", err)
	}

	var totalSize uint64
	for _, f := range zr.File {
		// Security: reject symlinks.
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			os.RemoveAll(tempDir)
			return "", "", fmt.Errorf("zip contains symlinks (not allowed)")
		}

		// Security: reject path traversal.
		relPath := f.Name
		if stripPrefix != "" {
			relPath = strings.TrimPrefix(relPath, stripPrefix)
		}
		if relPath == "" || strings.Contains(relPath, "..") || filepath.IsAbs(relPath) {
			continue // skip
		}

		// Security: check filename length.
		for _, part := range strings.Split(relPath, "/") {
			if len(part) > maxFilenameLen {
				os.RemoveAll(tempDir)
				return "", "", fmt.Errorf("filename too long: %s", part)
			}
		}

		// Security: extension allowlist (skip directories).
		if !f.FileInfo().IsDir() {
			ext := strings.ToLower(filepath.Ext(relPath))
			if ext != "" && !allowedExtensions[ext] {
				os.RemoveAll(tempDir)
				return "", "", fmt.Errorf("file type %s not allowed: %s", ext, relPath)
			}
		}

		// Security: zip bomb protection.
		totalSize += f.UncompressedSize64
		if totalSize > maxZipUncompressed {
			os.RemoveAll(tempDir)
			return "", "", fmt.Errorf("zip uncompressed size exceeds %d MB limit", maxZipUncompressed>>20)
		}

		destPath := filepath.Join(tempDir, relPath)

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, 0o755)
			continue
		}

		// Create parent dirs.
		os.MkdirAll(filepath.Dir(destPath), 0o755)

		rc, err := f.Open()
		if err != nil {
			os.RemoveAll(tempDir)
			return "", "", fmt.Errorf("open zip entry: %w", err)
		}

		outFile, err := os.Create(destPath)
		if err != nil {
			rc.Close()
			os.RemoveAll(tempDir)
			return "", "", fmt.Errorf("create file: %w", err)
		}

		// Limited copy to prevent decompression bombs.
		_, err = io.Copy(outFile, io.LimitReader(rc, int64(maxZipUncompressed)))
		outFile.Close()
		rc.Close()
		if err != nil {
			os.RemoveAll(tempDir)
			return "", "", fmt.Errorf("extract file: %w", err)
		}
	}

	// Parse name from SKILL.md frontmatter.
	skillMDData, _ := os.ReadFile(filepath.Join(tempDir, "SKILL.md"))
	name = parseFrontmatterName(string(skillMDData))
	if name == "" {
		// Fallback: use directory name from zip.
		if stripPrefix != "" {
			name = strings.TrimSuffix(stripPrefix, "/")
			name = filepath.Base(name)
		} else {
			name = "uploaded-skill"
		}
		name = strings.ToLower(name)
		name = strings.ReplaceAll(name, "_", "-")
		name = strings.ReplaceAll(name, " ", "-")
	}

	return tempDir, name, nil
}

// scanSkillDir lists files, detects scripts, and reads the readme.
func scanSkillDir(dir string) (files []string, scripts []scriptEntry, readme string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		files = append(files, rel)

		ext := strings.ToLower(filepath.Ext(rel))
		if scriptExtensions[ext] {
			content, _ := os.ReadFile(path)
			scripts = append(scripts, scriptEntry{
				Path:    rel,
				Content: string(content),
			})
		}

		if strings.EqualFold(filepath.Base(rel), "SKILL.md") {
			data, _ := os.ReadFile(path)
			readme = string(data)
		}
		return nil
	})
	return
}

// parseFrontmatterName extracts the name field from YAML frontmatter.
func parseFrontmatterName(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	end := strings.Index(content[3:], "---")
	if end < 0 {
		return ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			val := strings.TrimSpace(strings.TrimPrefix(line, "name:"))
			val = strings.Trim(val, "\"'")
			return strings.ToLower(strings.ReplaceAll(val, " ", "-"))
		}
	}
	return ""
}

func generateUploadID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
