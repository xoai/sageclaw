package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Attachment represents a file queued for upload.
type Attachment struct {
	Path     string
	Name     string
	Uploaded bool
	URL      string // Server path after upload.
}

// AttachmentBar manages file attachments for the current message.
type AttachmentBar struct {
	attachments []Attachment
	theme       Theme
}

// NewAttachmentBar creates an empty attachment bar.
func NewAttachmentBar(theme Theme) AttachmentBar {
	return AttachmentBar{theme: theme}
}

// Add queues a file for attachment.
func (a *AttachmentBar) Add(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("file not found: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("cannot attach directory")
	}
	if info.Size() > 30<<20 {
		return fmt.Errorf("file too large (max 30MB)")
	}

	a.attachments = append(a.attachments, Attachment{
		Path: abs,
		Name: filepath.Base(abs),
	})
	return nil
}

// Clear removes all attachments.
func (a *AttachmentBar) Clear() {
	a.attachments = nil
}

// Count returns the number of queued attachments.
func (a *AttachmentBar) Count() int {
	return len(a.attachments)
}

// View renders the attachment badge bar.
func (a *AttachmentBar) View(width int) string {
	if len(a.attachments) == 0 {
		return ""
	}

	badge := lipgloss.NewStyle().
		Foreground(a.theme.Accent).
		Bold(true)

	var parts []string
	for i, att := range a.attachments {
		parts = append(parts, badge.Render(fmt.Sprintf("[📎 %s #%d]", att.Name, i+1)))
	}

	return strings.Join(parts, " ") + "\n"
}

// uploadResultMsg carries the result of a file upload.
type uploadResultMsg struct {
	Index int
	URL   string
	Err   error
}

// UploadAll returns tea.Cmds to upload all pending attachments.
func (a *AttachmentBar) UploadAll(client *TUIClient, sessionID string) []tea.Cmd {
	var cmds []tea.Cmd
	for i, att := range a.attachments {
		if att.Uploaded {
			continue
		}
		idx := i
		path := att.Path
		cmds = append(cmds, func() tea.Msg {
			url, err := uploadFile(client, path, sessionID)
			return uploadResultMsg{Index: idx, URL: url, Err: err}
		})
	}
	return cmds
}

// MarkUploaded sets the upload result for an attachment.
func (a *AttachmentBar) MarkUploaded(index int, url string) {
	if index >= 0 && index < len(a.attachments) {
		a.attachments[index].Uploaded = true
		a.attachments[index].URL = url
	}
}

// uploadFile sends a file to the server via multipart POST.
func uploadFile(client *TUIClient, filePath, sessionID string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", err
	}

	if sessionID != "" {
		writer.WriteField("session_id", sessionID)
	}
	writer.Close()

	req, err := http.NewRequest("POST", client.BaseURL()+"/api/upload", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("upload failed: %d %s", resp.StatusCode, string(body))
	}

	// Server returns {"path": "/uploads/session/file.png"}
	var result struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Path, nil
}
