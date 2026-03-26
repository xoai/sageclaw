package gemini

import (
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
)

func TestBuildSetupMessage(t *testing.T) {
	cfg := provider.LiveSessionConfig{
		Model:              "gemini-2.5-flash-native-audio-preview-12-2025",
		SystemPrompt:       "You are a helpful assistant.",
		VoiceName:          "Kore",
		ResponseModalities: []string{"AUDIO"},
		Tools: []canonical.ToolDef{
			{
				Name:        "get_weather",
				Description: "Get current weather",
				InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}}}`),
			},
		},
	}

	msg := buildSetupMessage(cfg)

	// Verify top-level structure.
	setup, ok := msg["setup"].(map[string]any)
	if !ok {
		t.Fatal("missing setup key")
	}

	// Model.
	model, _ := setup["model"].(string)
	if model != "models/gemini-2.5-flash-native-audio-preview-12-2025" {
		t.Errorf("model = %q, want models/...", model)
	}

	// System instruction.
	si, ok := setup["systemInstruction"].(map[string]any)
	if !ok {
		t.Fatal("missing systemInstruction")
	}
	parts, _ := si["parts"].([]map[string]any)
	if len(parts) == 0 || parts[0]["text"] != "You are a helpful assistant." {
		t.Error("system instruction text mismatch")
	}

	// Generation config.
	gc, ok := setup["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing generationConfig")
	}
	modalities, _ := gc["responseModalities"].([]string)
	if len(modalities) != 1 || modalities[0] != "AUDIO" {
		t.Errorf("responseModalities = %v, want [AUDIO]", modalities)
	}

	// Voice config.
	sc, ok := gc["speechConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing speechConfig")
	}
	vc, ok := sc["voiceConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing voiceConfig")
	}
	pvc, ok := vc["prebuiltVoiceConfig"].(map[string]any)
	if !ok {
		t.Fatal("missing prebuiltVoiceConfig")
	}
	if pvc["voiceName"] != "Kore" {
		t.Errorf("voiceName = %v, want Kore", pvc["voiceName"])
	}

	// Tools.
	tools, ok := setup["tools"].([]map[string]any)
	if !ok || len(tools) == 0 {
		t.Fatal("missing tools")
	}
	funcDecls, ok := tools[0]["functionDeclarations"].([]map[string]any)
	if !ok || len(funcDecls) == 0 {
		t.Fatal("missing functionDeclarations")
	}
	if funcDecls[0]["name"] != "get_weather" {
		t.Errorf("tool name = %v, want get_weather", funcDecls[0]["name"])
	}

	// Transcription enabled.
	if _, ok := setup["inputAudioTranscription"]; !ok {
		t.Error("missing inputAudioTranscription")
	}
	if _, ok := setup["outputAudioTranscription"]; !ok {
		t.Error("missing outputAudioTranscription")
	}
}

func TestBuildSetupMessage_Defaults(t *testing.T) {
	cfg := provider.LiveSessionConfig{} // All empty.

	msg := buildSetupMessage(cfg)
	setup := msg["setup"].(map[string]any)

	model := setup["model"].(string)
	if model != "models/gemini-2.5-flash-native-audio-preview-12-2025" {
		t.Errorf("default model = %q", model)
	}

	gc := setup["generationConfig"].(map[string]any)
	modalities := gc["responseModalities"].([]string)
	if len(modalities) != 1 || modalities[0] != "AUDIO" {
		t.Errorf("default modalities = %v", modalities)
	}

	// No systemInstruction when empty.
	if _, ok := setup["systemInstruction"]; ok {
		t.Error("systemInstruction should be absent when empty")
	}

	// No tools when empty.
	if _, ok := setup["tools"]; ok {
		t.Error("tools should be absent when empty")
	}

	// No speechConfig when no voice name.
	if sc, ok := gc["speechConfig"]; ok {
		t.Errorf("speechConfig should be absent when no voice name, got %v", sc)
	}

	// No language_code when empty.
	if lc, ok := gc["language_code"]; ok {
		t.Errorf("language_code should be absent when empty, got %v", lc)
	}
}

func TestBuildSetupMessage_WithLanguage(t *testing.T) {
	cfg := provider.LiveSessionConfig{
		VoiceName:    "Sadaltager",
		LanguageCode: "vi-VN",
	}

	msg := buildSetupMessage(cfg)
	setup := msg["setup"].(map[string]any)
	gc := setup["generationConfig"].(map[string]any)

	// language_code should be inside speechConfig (per Gemini docs).
	sc := gc["speechConfig"].(map[string]any)
	lc, ok := sc["language_code"].(string)
	if !ok || lc != "vi-VN" {
		t.Errorf("speechConfig.language_code = %v, want vi-VN", sc["language_code"])
	}

	// Voice should be Sadaltager.
	vc := sc["voiceConfig"].(map[string]any)
	pvc := vc["prebuiltVoiceConfig"].(map[string]any)
	if pvc["voiceName"] != "Sadaltager" {
		t.Errorf("voiceName = %v, want Sadaltager", pvc["voiceName"])
	}
}

func TestParseServerMessage_Audio(t *testing.T) {
	// Simulate a server message with audio data.
	data := `{
		"serverContent": {
			"modelTurn": {
				"parts": [{"inlineData": {"mimeType": "audio/pcm", "data": "AQID"}}]
			}
		}
	}`

	s := &liveSession{}
	events := s.parseServerMessage([]byte(data))

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "audio" {
		t.Errorf("type = %q, want audio", events[0].Type)
	}
	if len(events[0].Audio) == 0 {
		t.Error("audio data is empty")
	}
}

func TestParseServerMessage_TurnComplete(t *testing.T) {
	data := `{"serverContent": {"turnComplete": true}}`

	s := &liveSession{}
	events := s.parseServerMessage([]byte(data))

	found := false
	for _, e := range events {
		if e.Type == "done" {
			found = true
		}
	}
	if !found {
		t.Error("expected done event for turnComplete")
	}
}

func TestParseServerMessage_ToolCall(t *testing.T) {
	data := `{
		"toolCall": {
			"functionCalls": [
				{"id": "call_1", "name": "get_weather", "args": {"city": "Tokyo"}}
			]
		}
	}`

	s := &liveSession{}
	events := s.parseServerMessage([]byte(data))

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "tool_call" {
		t.Errorf("type = %q, want tool_call", events[0].Type)
	}
	if events[0].ToolCall == nil {
		t.Fatal("ToolCall is nil")
	}
	if events[0].ToolCall.Name != "get_weather" {
		t.Errorf("tool name = %q, want get_weather", events[0].ToolCall.Name)
	}
}

func TestParseServerMessage_Transcript(t *testing.T) {
	data := `{
		"serverContent": {
			"outputTranscription": {"text": "Hello world"}
		}
	}`

	s := &liveSession{}
	events := s.parseServerMessage([]byte(data))

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "transcript" {
		t.Errorf("type = %q, want transcript", events[0].Type)
	}
	if events[0].Transcript.Direction != "output" {
		t.Errorf("direction = %q, want output", events[0].Transcript.Direction)
	}
	if events[0].Transcript.Text != "Hello world" {
		t.Errorf("text = %q, want 'Hello world'", events[0].Transcript.Text)
	}
}

func TestParseServerMessage_GoAway(t *testing.T) {
	data := `{"goAway": {"timeLeft": "30s"}}`

	s := &liveSession{}
	events := s.parseServerMessage([]byte(data))

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].Type != "go_away" {
		t.Errorf("type = %q, want go_away", events[0].Type)
	}
}
