package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/security"
)

func newTestSendMedia(t *testing.T) (ToolFunc, string) {
	t.Helper()
	dir := t.TempDir()
	sb, err := security.NewSandbox(dir)
	if err != nil {
		t.Fatalf("creating sandbox: %v", err)
	}

	// Mock session info: always returns telegram platform.
	sessionInfo := func(ctx context.Context, sessionID string) (string, string, string, error) {
		return "telegram", "chat123", "tg_abc", nil
	}

	// Mock sender: records the call.
	var lastSend struct {
		connID, chatID, filePath, mimeType, sendAs, caption string
	}
	sender := func(ctx context.Context, connID, chatID, filePath, mimeType, sendAs, caption string) error {
		lastSend.connID = connID
		lastSend.chatID = chatID
		lastSend.filePath = filePath
		lastSend.mimeType = mimeType
		lastSend.sendAs = sendAs
		lastSend.caption = caption
		return nil
	}

	// Mock connection info: agent "default" bound to "tg_abc".
	connInfo := func(ctx context.Context, connID string) (string, string, error) {
		if connID == "tg_abc" {
			return "telegram", "default", nil
		}
		return "", "", fmt.Errorf("connection not found: %s", connID)
	}

	reg := NewRegistry()
	RegisterMediaSend(reg, sb, sessionInfo, connInfo, sender)
	_, fn, ok := reg.Get("send_media")
	if !ok {
		t.Fatal("send_media tool not registered")
	}

	return fn, dir
}

func withSession(ctx context.Context) context.Context {
	return WithSessionID(ctx, "test-session-123")
}

func TestSendMedia_ImageAuto(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	// Create a test image file.
	imgPath := filepath.Join(dir, "test.png")
	if err := os.WriteFile(imgPath, []byte("fake-png-data"), 0644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]string{"path": "test.png"})
	result, err := fn(withSession(context.Background()), input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content)
	}
	if !strings.Contains(result.Content, "photo") {
		t.Errorf("expected send_as=photo in result, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "telegram") {
		t.Errorf("expected platform=telegram in result, got: %s", result.Content)
	}
}

func TestSendMedia_AudioAutoVoice(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	audioPath := filepath.Join(dir, "voice.ogg")
	os.WriteFile(audioPath, []byte("fake-ogg"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "voice.ogg"})
	result, _ := fn(withSession(context.Background()), input)
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "voice") {
		t.Errorf("expected send_as=voice, got: %s", result.Content)
	}
}

func TestSendMedia_DocumentExplicit(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "large.png")
	os.WriteFile(imgPath, []byte("fake-png"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "large.png", "send_as": "document"})
	result, _ := fn(withSession(context.Background()), input)
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "document") {
		t.Errorf("expected send_as=document, got: %s", result.Content)
	}
}

func TestSendMedia_WithCaption(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.jpg")
	os.WriteFile(imgPath, []byte("fake-jpg"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "photo.jpg", "caption": "Check this out!"})
	result, _ := fn(withSession(context.Background()), input)
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content)
	}
}

func TestSendMedia_UnsupportedType(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	exePath := filepath.Join(dir, "malware.exe")
	os.WriteFile(exePath, []byte("evil"), 0644)

	input, _ := json.Marshal(map[string]string{"path": "malware.exe"})
	result, _ := fn(withSession(context.Background()), input)
	if !result.IsError {
		t.Error("expected error for .exe file")
	}
	if !strings.Contains(result.Content, "unsupported file type") {
		t.Errorf("expected unsupported type error, got: %s", result.Content)
	}
}

func TestSendMedia_FileNotFound(t *testing.T) {
	fn, _ := newTestSendMedia(t)

	input, _ := json.Marshal(map[string]string{"path": "nonexistent.png"})
	result, _ := fn(withSession(context.Background()), input)
	if !result.IsError {
		t.Error("expected error for missing file")
	}
	if !strings.Contains(result.Content, "file not found") {
		t.Errorf("expected file not found error, got: %s", result.Content)
	}
}

func TestSendMedia_NoSession(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "test.png")
	os.WriteFile(imgPath, []byte("data"), 0644)

	// No session in context.
	input, _ := json.Marshal(map[string]string{"path": "test.png"})
	result, _ := fn(context.Background(), input)
	if !result.IsError {
		t.Error("expected error without session")
	}
	if !strings.Contains(result.Content, "session") {
		t.Errorf("expected session error, got: %s", result.Content)
	}
}

func TestSendMedia_PathTraversal(t *testing.T) {
	fn, _ := newTestSendMedia(t)

	input, _ := json.Marshal(map[string]string{"path": "../../etc/passwd"})
	result, _ := fn(withSession(context.Background()), input)
	if !result.IsError {
		t.Error("expected error for path traversal")
	}
	if !strings.Contains(result.Content, "outside workspace") {
		t.Errorf("expected workspace boundary error, got: %s", result.Content)
	}
}

func TestSendMedia_Directory(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	subdir := filepath.Join(dir, "subdir")
	os.MkdirAll(subdir, 0755)

	input, _ := json.Marshal(map[string]string{"path": "subdir"})
	result, _ := fn(withSession(context.Background()), input)
	if !result.IsError {
		t.Error("expected error for directory")
	}
	if !strings.Contains(result.Content, "directory") {
		t.Errorf("expected directory error, got: %s", result.Content)
	}
}

func TestResolveSendAs(t *testing.T) {
	tests := []struct {
		sendAs   string
		mimeType string
		want     string
	}{
		{"auto", "image/png", "photo"},
		{"auto", "audio/ogg", "voice"},
		{"auto", "video/mp4", "video"},
		{"auto", "application/pdf", "document"},
		{"auto", "text/plain", "document"},
		{"document", "image/png", "document"}, // explicit override
		{"photo", "application/pdf", "photo"}, // explicit override
	}
	for _, tt := range tests {
		got := resolveSendAs(tt.sendAs, tt.mimeType)
		if got != tt.want {
			t.Errorf("resolveSendAs(%q, %q) = %q, want %q", tt.sendAs, tt.mimeType, got, tt.want)
		}
	}
}

func TestDetectMediaMIME(t *testing.T) {
	if m := DetectMediaMIME(".png"); m != "image/png" {
		t.Errorf("expected image/png, got %q", m)
	}
	if m := DetectMediaMIME(".exe"); m != "" {
		t.Errorf("expected empty for .exe, got %q", m)
	}
	if m := DetectMediaMIME(".PDF"); m != "application/pdf" {
		t.Errorf("expected case-insensitive match, got %q", m)
	}
}

// --- Cross-channel tests ---

func withAgent(ctx context.Context, agentID string) context.Context {
	return WithAgentID(ctx, agentID)
}

func TestSendMedia_CrossChannel_Success(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.png")
	os.WriteFile(imgPath, []byte("png-data"), 0644)

	// Explicit channel + chat_id, agent "default" is bound to "tg_abc".
	input, _ := json.Marshal(map[string]string{
		"path": "photo.png", "channel": "tg_abc", "chat_id": "user456",
	})
	ctx := withAgent(context.Background(), "default") // no session needed
	result, err := fn(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("expected success, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "telegram") {
		t.Errorf("expected telegram in result, got: %s", result.Content)
	}
}

func TestSendMedia_CrossChannel_NotBound(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.png")
	os.WriteFile(imgPath, []byte("data"), 0644)

	input, _ := json.Marshal(map[string]string{
		"path": "photo.png", "channel": "tg_abc", "chat_id": "user456",
	})
	// Agent "other" is NOT bound to "tg_abc".
	ctx := withAgent(context.Background(), "other")
	result, _ := fn(ctx, input)
	if !result.IsError {
		t.Error("expected error for unbound agent")
	}
	if !strings.Contains(result.Content, "not bound") {
		t.Errorf("expected binding error, got: %s", result.Content)
	}
}

func TestSendMedia_CrossChannel_MissingChatID(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.png")
	os.WriteFile(imgPath, []byte("data"), 0644)

	// Channel provided but no chat_id.
	input, _ := json.Marshal(map[string]string{
		"path": "photo.png", "channel": "tg_abc",
	})
	ctx := withAgent(context.Background(), "default")
	result, _ := fn(ctx, input)
	if !result.IsError {
		t.Error("expected error for missing chat_id")
	}
	if !strings.Contains(result.Content, "chat_id is required") {
		t.Errorf("expected chat_id error, got: %s", result.Content)
	}
}

func TestSendMedia_CrossChannel_ConnectionNotFound(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.png")
	os.WriteFile(imgPath, []byte("data"), 0644)

	input, _ := json.Marshal(map[string]string{
		"path": "photo.png", "channel": "dc_unknown", "chat_id": "user456",
	})
	ctx := withAgent(context.Background(), "default")
	result, _ := fn(ctx, input)
	if !result.IsError {
		t.Error("expected error for unknown connection")
	}
	if !strings.Contains(result.Content, "connection not found") {
		t.Errorf("expected connection error, got: %s", result.Content)
	}
}

func TestSendMedia_NoSessionNoChannel(t *testing.T) {
	fn, dir := newTestSendMedia(t)

	imgPath := filepath.Join(dir, "photo.png")
	os.WriteFile(imgPath, []byte("data"), 0644)

	// No session, no explicit channel — should fail.
	input, _ := json.Marshal(map[string]string{"path": "photo.png"})
	ctx := withAgent(context.Background(), "default")
	result, _ := fn(ctx, input)
	if !result.IsError {
		t.Error("expected error with no session and no channel")
	}
	if !strings.Contains(result.Content, "session") || !strings.Contains(result.Content, "channel") {
		t.Errorf("expected session/channel error, got: %s", result.Content)
	}
}
