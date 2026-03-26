package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// mockAudioStore implements AudioStore for testing.
type mockAudioStore struct {
	files map[string][]byte
}

func newMockAudioStore() *mockAudioStore {
	return &mockAudioStore{files: make(map[string][]byte)}
}

func (m *mockAudioStore) Save(sessionID, msgID string, data []byte, ext string) (string, error) {
	path := sessionID + "/" + msgID + "." + ext
	m.files[path] = data
	return path, nil
}

func (m *mockAudioStore) Load(path string) ([]byte, error) {
	if data, ok := m.files[path]; ok {
		return data, nil
	}
	return nil, fmt.Errorf("not found: %s", path)
}

func (m *mockAudioStore) Exists(path string) bool {
	_, ok := m.files[path]
	return ok
}

func TestNormalizeMessage_VoiceMessage(t *testing.T) {
	// Set up mock server for getFile + file download.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/getFile":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]string{"file_path": "voice/file_123.oga"},
			})
		case r.URL.Path == "/file/voice/file_123.oga":
			w.Write([]byte("fake-ogg-audio-data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := newMockAudioStore()
	adapter := New("tg_test", "token123", WithBaseURL(server.URL), WithAudioStore(store))

	msg := &TelegramMessage{
		MessageID: 42,
		Chat:      TelegramChat{ID: 12345, Type: "private"},
		Voice: &VoiceMessage{
			FileID:   "voice_file_123",
			Duration: 5,
			MimeType: "audio/ogg",
		},
	}

	hasAudio := false
	result := adapter.normalizeMessageWithAudio(context.Background(), msg, &hasAudio)

	if !hasAudio {
		t.Error("hasAudio should be true for voice message")
	}

	if len(result.Content) == 0 {
		t.Fatal("expected at least one content block")
	}

	audioContent := result.Content[0]
	if audioContent.Type != "audio" {
		t.Errorf("type = %q, want audio", audioContent.Type)
	}
	if audioContent.Audio == nil {
		t.Fatal("Audio source is nil")
	}
	if audioContent.Audio.DurationMs != 5000 {
		t.Errorf("duration = %d, want 5000", audioContent.Audio.DurationMs)
	}

	// Verify file was saved.
	if len(store.files) == 0 {
		t.Error("no audio files saved")
	}
}

func TestNormalizeMessage_VoiceTooLong(t *testing.T) {
	store := newMockAudioStore()
	adapter := New("tg_test", "token", WithAudioStore(store))

	msg := &TelegramMessage{
		MessageID: 1,
		Chat:      TelegramChat{ID: 1, Type: "private"},
		Voice: &VoiceMessage{
			FileID:   "voice_long",
			Duration: 700, // 700 seconds > 600 max
		},
	}

	hasAudio := false
	result := adapter.normalizeMessageWithAudio(context.Background(), msg, &hasAudio)

	if hasAudio {
		t.Error("hasAudio should be false for too-long voice")
	}

	if len(result.Content) == 0 {
		t.Fatal("expected content")
	}
	if result.Content[0].Type != "text" {
		t.Errorf("expected text content, got %q", result.Content[0].Type)
	}
}

func TestNormalizeMessage_VoiceNoStore(t *testing.T) {
	adapter := New("tg_test", "token")

	msg := &TelegramMessage{
		MessageID: 1,
		Chat:      TelegramChat{ID: 1, Type: "private"},
		Voice: &VoiceMessage{
			FileID:   "voice_123",
			Duration: 5,
		},
	}

	hasAudio := false
	result := adapter.normalizeMessageWithAudio(context.Background(), msg, &hasAudio)

	if hasAudio {
		t.Error("hasAudio should be false without audio store")
	}
	if len(result.Content) == 0 || result.Content[0].Type != "text" {
		t.Error("expected text fallback")
	}
}

func TestNormalizeMessage_TextOnly(t *testing.T) {
	adapter := New("tg_test", "token")

	msg := &TelegramMessage{
		MessageID: 1,
		Chat:      TelegramChat{ID: 1, Type: "private"},
		Text:      "Hello world",
	}

	hasAudio := false
	result := adapter.normalizeMessageWithAudio(context.Background(), msg, &hasAudio)

	if hasAudio {
		t.Error("hasAudio should be false for text message")
	}
	if len(result.Content) != 1 || result.Content[0].Text != "Hello world" {
		t.Error("text content mismatch")
	}
}

func TestSendVoiceMultipart(t *testing.T) {
	var receivedChatID string
	var receivedVoiceLen int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sendVoice" {
			r.ParseMultipartForm(10 << 20)
			receivedChatID = r.FormValue("chat_id")
			file, _, err := r.FormFile("voice")
			if err == nil {
				defer file.Close()
				buf := make([]byte, 1024)
				n, _ := file.Read(buf)
				receivedVoiceLen = n
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	adapter := New("tg_test", "token", WithBaseURL(server.URL))

	oggData := []byte("fake-ogg-response-data")
	err := adapter.sendVoiceMultipart("12345", oggData, 5)
	if err != nil {
		t.Fatalf("sendVoiceMultipart: %v", err)
	}

	if receivedChatID != "12345" {
		t.Errorf("chat_id = %q, want 12345", receivedChatID)
	}
	if receivedVoiceLen != len(oggData) {
		t.Errorf("voice data len = %d, want %d", receivedVoiceLen, len(oggData))
	}
}

func TestSendResponse_AudioContent(t *testing.T) {
	var sentVoice bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sendVoice" {
			sentVoice = true
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	store := newMockAudioStore()
	store.files["test/resp.ogg"] = []byte("ogg-data")

	adapter := New("tg_test", "token", WithBaseURL(server.URL), WithAudioStore(store))

	env := bus.Envelope{
		ChatID:   "12345",
		HasAudio: true,
		Messages: []canonical.Message{{
			Role: "assistant",
			Content: []canonical.Content{{
				Type: "audio",
				Audio: &canonical.AudioSource{
					FilePath:   "test/resp.ogg",
					MimeType:   "audio/ogg",
					DurationMs: 5000,
				},
			}},
		}},
	}
	adapter.sendResponse(env)

	if !sentVoice {
		t.Error("expected sendVoice to be called for audio content")
	}
}

func TestSendResponse_TextFallbackOnAudioError(t *testing.T) {
	var sentText bool

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sendVoice" {
			// Simulate error.
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		}
		if r.URL.Path == "/sendMessage" {
			sentText = true
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
		}
	}))
	defer server.Close()

	store := newMockAudioStore()
	store.files["test/resp.ogg"] = []byte("ogg-data")

	adapter := New("tg_test", "token", WithBaseURL(server.URL), WithAudioStore(store))

	env := bus.Envelope{
		ChatID: "12345",
		Messages: []canonical.Message{{
			Role: "assistant",
			Content: []canonical.Content{{
				Type: "audio",
				Audio: &canonical.AudioSource{
					FilePath:   "test/resp.ogg",
					MimeType:   "audio/ogg",
					Transcript: "Hello from voice",
				},
			}},
		}},
	}
	adapter.sendResponse(env)

	if !sentText {
		t.Error("expected text fallback when sendVoice fails")
	}
}

func TestDownloadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/getFile":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": map[string]string{"file_path": "voice/test.oga"},
			})
		case r.URL.Path == "/file/voice/test.oga":
			w.Write([]byte("audio-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := New("tg_test", "token", WithBaseURL(server.URL))

	data, err := adapter.downloadFile(context.Background(), "file_id_123")
	if err != nil {
		t.Fatalf("downloadFile: %v", err)
	}
	if string(data) != "audio-bytes" {
		t.Errorf("data = %q, want audio-bytes", data)
	}
}
