package gemini

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"

	"nhooyr.io/websocket"
)

const (
	// Gemini Live API WebSocket endpoint.
	liveWSEndpoint = "wss://generativelanguage.googleapis.com/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
)

// LiveClient implements provider.LiveProvider for Gemini Live API.
type LiveClient struct {
	apiKey string
}

// NewLiveClient creates a Gemini Live provider.
func NewLiveClient(apiKey string) *LiveClient {
	return &LiveClient{apiKey: apiKey}
}

func (c *LiveClient) Name() string { return "gemini-live" }

// OpenSession establishes a WebSocket connection to Gemini Live API
// and sends the setup message. Returns a LiveSession for bidirectional audio.
func (c *LiveClient) OpenSession(ctx context.Context, cfg provider.LiveSessionConfig) (provider.LiveSession, error) {
	url := fmt.Sprintf("%s?key=%s", liveWSEndpoint, c.apiKey)

	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("gemini live: dial: %w", err)
	}

	// Set read limit high enough for audio chunks.
	conn.SetReadLimit(10 * 1024 * 1024) // 10 MB

	readCtx, readCancel := context.WithCancel(context.Background())

	sess := &liveSession{
		conn:   conn,
		events: make(chan provider.LiveEvent, 64),
		done:   make(chan struct{}),
		cancel: readCancel,
		id:     fmt.Sprintf("gemini-live-%p", conn),
	}

	// Send setup message.
	setup := buildSetupMessage(cfg)
	if err := sess.sendJSON(ctx, setup); err != nil {
		readCancel()
		conn.Close(websocket.StatusNormalClosure, "setup failed")
		return nil, fmt.Errorf("gemini live: setup: %w", err)
	}

	// Wait for setup complete.
	if err := sess.waitSetupComplete(ctx); err != nil {
		readCancel()
		conn.Close(websocket.StatusNormalClosure, "setup incomplete")
		return nil, fmt.Errorf("gemini live: setup response: %w", err)
	}

	// Start receive loop with cancellable context.
	go sess.receiveLoop(readCtx)

	return sess, nil
}

// Verify interface compliance.
var _ provider.LiveProvider = (*LiveClient)(nil)

// liveSession is an active Gemini Live WebSocket session.
type liveSession struct {
	conn   *websocket.Conn
	events chan provider.LiveEvent
	done   chan struct{}
	cancel context.CancelFunc // Cancels the receiveLoop's read context.
	id     string

	mu      sync.Mutex
	writeMu sync.Mutex // Protects concurrent WebSocket writes.
	closed  bool
}

func (s *liveSession) ID() string { return s.id }

func (s *liveSession) Receive() <-chan provider.LiveEvent { return s.events }

func (s *liveSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.done)
	s.cancel() // Cancel the receiveLoop's read context.
	return s.conn.Close(websocket.StatusNormalClosure, "session closed")
}

// Send sends audio, text, or tool results to the model.
func (s *liveSession) Send(ctx context.Context, msg provider.LiveMessage) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("gemini live: session closed")
	}
	s.mu.Unlock()

	// Tool results.
	if len(msg.ToolResults) > 0 {
		return s.sendToolResponse(ctx, msg.ToolResults, msg.ToolNames)
	}

	// Audio input — send via realtimeInput with PCM data.
	// Gemini Live only accepts audio/pcm via realtimeInput.
	// After sending, signal audioStreamEnd so Gemini processes and responds.
	if len(msg.Audio) > 0 {
		mime := msg.AudioMime
		if mime == "" {
			mime = "audio/pcm;rate=16000"
		}
		if err := s.sendAudio(ctx, msg.Audio, mime); err != nil {
			return err
		}
		return s.sendAudioStreamEnd(ctx)
	}

	// Text input.
	if msg.Text != "" {
		return s.sendText(ctx, msg.Text)
	}

	return nil
}

// sendAudio sends audio data via realtimeInput with the specified MIME type.
// Gemini Live realtimeInput only accepts: audio/pcm or audio/pcm;rate=xxxxx.
// Callers must convert other formats (OGG, WAV, etc.) to PCM before sending.
func (s *liveSession) sendAudio(ctx context.Context, audioData []byte, mimeType string) error {
	msg := map[string]any{
		"realtimeInput": map[string]any{
			"audio": map[string]any{
				"data":     base64.StdEncoding.EncodeToString(audioData),
				"mimeType": mimeType,
			},
		},
	}
	return s.sendJSON(ctx, msg)
}

// sendAudioAsContent sends audio as a clientContent turn with an inline blob.
// Unlike realtimeInput (which requires PCM and VAD), clientContent accepts
// multiple audio formats including audio/ogg and treats the audio as a
// complete user turn. This is ideal for async voice messages.
func (s *liveSession) sendAudioAsContent(ctx context.Context, audioData []byte, mimeType string) error {
	msg := map[string]any{
		"clientContent": map[string]any{
			"turns": []map[string]any{
				{
					"role": "user",
					"parts": []map[string]any{
						{
							"inlineData": map[string]any{
								"data":     base64.StdEncoding.EncodeToString(audioData),
								"mimeType": mimeType,
							},
						},
					},
				},
			},
			"turnComplete": true,
		},
	}
	return s.sendJSON(ctx, msg)
}

// sendAudioStreamEnd signals that the audio input is complete.
// This triggers Gemini's VAD to process the audio and generate a response.
func (s *liveSession) sendAudioStreamEnd(ctx context.Context) error {
	msg := map[string]any{
		"realtimeInput": map[string]any{
			"audioStreamEnd": true,
		},
	}
	return s.sendJSON(ctx, msg)
}

// sendText sends text via clientContent (interrupts current generation).
func (s *liveSession) sendText(ctx context.Context, text string) error {
	msg := map[string]any{
		"clientContent": map[string]any{
			"turns": []map[string]any{
				{
					"role":  "user",
					"parts": []map[string]any{{"text": text}},
				},
			},
			"turnComplete": true,
		},
	}
	return s.sendJSON(ctx, msg)
}

// sendToolResponse sends function call results back to the model.
// toolNames maps ToolCallID → function name (Gemini requires the function name).
func (s *liveSession) sendToolResponse(ctx context.Context, results []canonical.ToolResult, toolNames map[string]string) error {
	var responses []map[string]any
	for _, r := range results {
		name := r.ToolCallID // Fallback.
		if n, ok := toolNames[r.ToolCallID]; ok && n != "" {
			name = n
		}
		responses = append(responses, map[string]any{
			"id":       r.ToolCallID,
			"name":     name,
			"response": map[string]any{"result": r.Content},
		})
	}

	msg := map[string]any{
		"toolResponse": map[string]any{
			"functionResponses": responses,
		},
	}
	return s.sendJSON(ctx, msg)
}

func (s *liveSession) sendJSON(ctx context.Context, msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("gemini live: marshal: %w", err)
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.Write(ctx, websocket.MessageText, data)
}

// waitSetupComplete reads messages until BidiGenerateContentSetupComplete.
func (s *liveSession) waitSetupComplete(ctx context.Context) error {
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		return fmt.Errorf("reading setup response: %w", err)
	}

	var resp map[string]json.RawMessage
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parsing setup response: %w", err)
	}

	if _, ok := resp["setupComplete"]; !ok {
		return fmt.Errorf("expected setupComplete, got: %s", string(data))
	}

	return nil
}

// receiveLoop reads messages from the WebSocket and emits LiveEvents.
func (s *liveSession) receiveLoop(ctx context.Context) {
	defer close(s.events)

	for {
		select {
		case <-s.done:
			return
		default:
		}

		_, data, err := s.conn.Read(ctx)
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if !closed {
				s.events <- provider.LiveEvent{Type: "error", Error: fmt.Errorf("read: %w", err)}
			}
			return
		}

		events := s.parseServerMessage(data)
		for _, ev := range events {
			select {
			case s.events <- ev:
			case <-s.done:
				return
			}
		}
	}
}

// parseServerMessage converts a raw Gemini Live server message into LiveEvents.
func (s *liveSession) parseServerMessage(data []byte) []provider.LiveEvent {
	var msg serverMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		log.Printf("gemini live: parse error: %v", err)
		return nil
	}

	var events []provider.LiveEvent

	// Server content (audio/text response).
	if msg.ServerContent != nil {
		sc := msg.ServerContent

		// Model turn — extract audio and text parts.
		if sc.ModelTurn != nil {
			for _, part := range sc.ModelTurn.Parts {
				if part.InlineData != nil && part.InlineData.Data != "" {
					audioData, err := base64.StdEncoding.DecodeString(part.InlineData.Data)
					if err == nil && len(audioData) > 0 {
						events = append(events, provider.LiveEvent{Type: "audio", Audio: audioData})
					}
				}
				if part.Text != "" {
					events = append(events, provider.LiveEvent{Type: "text", Text: part.Text})
				}
			}
		}

		// Input transcription.
		if sc.InputTranscription != nil && sc.InputTranscription.Text != "" {
			events = append(events, provider.LiveEvent{
				Type:       "transcript",
				Transcript: &provider.LiveTranscript{Direction: "input", Text: sc.InputTranscription.Text},
			})
		}

		// Output transcription.
		if sc.OutputTranscription != nil && sc.OutputTranscription.Text != "" {
			events = append(events, provider.LiveEvent{
				Type:       "transcript",
				Transcript: &provider.LiveTranscript{Direction: "output", Text: sc.OutputTranscription.Text},
			})
		}

		// Turn complete.
		if sc.TurnComplete {
			events = append(events, provider.LiveEvent{Type: "done"})
		}
	}

	// Tool call.
	if msg.ToolCall != nil {
		for _, fc := range msg.ToolCall.FunctionCalls {
			inputJSON, _ := json.Marshal(fc.Args)
			events = append(events, provider.LiveEvent{
				Type: "tool_call",
				ToolCall: &canonical.ToolCall{
					ID:    fc.ID,
					Name:  fc.Name,
					Input: inputJSON,
				},
			})
		}
	}

	// Usage metadata.
	if msg.UsageMetadata != nil {
		events = append(events, provider.LiveEvent{
			Type: "usage",
			Usage: &canonical.Usage{
				InputTokens:  msg.UsageMetadata.PromptTokenCount,
				OutputTokens: msg.UsageMetadata.CandidatesTokenCount,
			},
		})
	}

	// GoAway — server is disconnecting.
	if msg.GoAway != nil {
		events = append(events, provider.LiveEvent{
			Type:  "go_away",
			Error: fmt.Errorf("server disconnecting: %s time left", msg.GoAway.TimeLeft),
		})
	}

	return events
}

// --- Gemini Live protocol types ---

type serverMessage struct {
	ServerContent *serverContent `json:"serverContent,omitempty"`
	ToolCall      *toolCallMsg   `json:"toolCall,omitempty"`
	UsageMetadata *usageMetadata `json:"usageMetadata,omitempty"`
	GoAway        *goAwayMsg     `json:"goAway,omitempty"`
}

type serverContent struct {
	ModelTurn           *modelTurn     `json:"modelTurn,omitempty"`
	TurnComplete        bool           `json:"turnComplete,omitempty"`
	GenerationComplete  bool           `json:"generationComplete,omitempty"`
	Interrupted         bool           `json:"interrupted,omitempty"`
	InputTranscription  *transcription `json:"inputTranscription,omitempty"`
	OutputTranscription *transcription `json:"outputTranscription,omitempty"`
}

type modelTurn struct {
	Parts []serverPart `json:"parts"`
}

type serverPart struct {
	Text       string      `json:"text,omitempty"`
	InlineData *inlineData `json:"inlineData,omitempty"`
}

type inlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"` // Base64 encoded.
}

type transcription struct {
	Text string `json:"text"`
}

type toolCallMsg struct {
	FunctionCalls []functionCall `json:"functionCalls"`
}

type functionCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type usageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
}

type goAwayMsg struct {
	TimeLeft string `json:"timeLeft"`
}

// buildSetupMessage creates the initial setup JSON for a Gemini Live session.
func buildSetupMessage(cfg provider.LiveSessionConfig) map[string]any {
	model := cfg.Model
	if model == "" {
		model = "gemini-2.5-flash-native-audio-preview-12-2025"
	}

	modalities := cfg.ResponseModalities
	if len(modalities) == 0 {
		modalities = []string{"AUDIO"}
	}

	setup := map[string]any{
		"model": "models/" + model,
		"generationConfig": map[string]any{
			"responseModalities": modalities,
		},
	}

	// Speech config with voice selection and language.
	// Per Gemini docs: language_code goes inside speechConfig, not generationConfig.
	speechConfig := map[string]any{}
	if cfg.VoiceName != "" {
		speechConfig["voiceConfig"] = map[string]any{
			"prebuiltVoiceConfig": map[string]any{
				"voiceName": cfg.VoiceName,
			},
		}
	}
	if cfg.LanguageCode != "" {
		speechConfig["language_code"] = cfg.LanguageCode
	}
	if len(speechConfig) > 0 {
		setup["generationConfig"].(map[string]any)["speechConfig"] = speechConfig
	}

	// System instruction.
	if cfg.SystemPrompt != "" {
		setup["systemInstruction"] = map[string]any{
			"parts": []map[string]any{{"text": cfg.SystemPrompt}},
		}
	}

	// Tools (function declarations).
	if len(cfg.Tools) > 0 {
		var funcDecls []map[string]any
		for _, tool := range cfg.Tools {
			decl := map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
			}
			if len(tool.InputSchema) > 0 {
				var params any
				if json.Unmarshal(tool.InputSchema, &params) == nil {
					decl["parameters"] = params
				}
			}
			funcDecls = append(funcDecls, decl)
		}
		setup["tools"] = []map[string]any{
			{"functionDeclarations": funcDecls},
		}
	}

	// Enable transcription for both directions.
	setup["inputAudioTranscription"] = map[string]any{}
	setup["outputAudioTranscription"] = map[string]any{}

	return map[string]any{"setup": setup}
}
