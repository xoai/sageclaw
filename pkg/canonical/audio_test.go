package canonical

import "testing"

func TestHasAudio(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			"text only",
			Message{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}},
			false,
		},
		{
			"audio present",
			Message{Role: "user", Content: []Content{
				{Type: "audio", Audio: &AudioSource{FilePath: "test.ogg", MimeType: "audio/ogg"}},
			}},
			true,
		},
		{
			"audio type but nil source",
			Message{Role: "user", Content: []Content{{Type: "audio"}}},
			false,
		},
		{
			"mixed text and audio",
			Message{Role: "user", Content: []Content{
				{Type: "text", Text: "caption"},
				{Type: "audio", Audio: &AudioSource{FilePath: "test.ogg", MimeType: "audio/ogg"}},
			}},
			true,
		},
		{
			"empty content",
			Message{Role: "user", Content: nil},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HasAudio(tt.msg); got != tt.want {
				t.Errorf("HasAudio() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractAudio(t *testing.T) {
	src := &AudioSource{FilePath: "data/audio/s/m.ogg", MimeType: "audio/ogg", DurationMs: 5000}

	msg := Message{Role: "user", Content: []Content{
		{Type: "text", Text: "voice message"},
		{Type: "audio", Audio: src},
	}}

	got := ExtractAudio(msg)
	if got == nil {
		t.Fatal("ExtractAudio returned nil")
	}
	if got.FilePath != src.FilePath {
		t.Errorf("FilePath = %q, want %q", got.FilePath, src.FilePath)
	}
	if got.DurationMs != 5000 {
		t.Errorf("DurationMs = %d, want 5000", got.DurationMs)
	}
}

func TestExtractAudio_NoAudio(t *testing.T) {
	msg := Message{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}}
	if got := ExtractAudio(msg); got != nil {
		t.Errorf("ExtractAudio on text-only = %v, want nil", got)
	}
}

func TestMessagesHaveAudio(t *testing.T) {
	textMsg := Message{Role: "user", Content: []Content{{Type: "text", Text: "hi"}}}
	audioMsg := Message{Role: "user", Content: []Content{
		{Type: "audio", Audio: &AudioSource{FilePath: "test.ogg", MimeType: "audio/ogg"}},
	}}

	if MessagesHaveAudio([]Message{textMsg}) {
		t.Error("text-only messages should not have audio")
	}
	if !MessagesHaveAudio([]Message{textMsg, audioMsg}) {
		t.Error("should detect audio in mixed messages")
	}
	if MessagesHaveAudio(nil) {
		t.Error("nil messages should not have audio")
	}
}
