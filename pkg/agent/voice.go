package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/provider"
)

// AudioCodec converts between OGG Opus and raw PCM audio.
// Matches the Codec interface from pkg/audio without importing it
// (to avoid circular deps — the loop doesn't need the full audio package).
type AudioCodec interface {
	DecodeOGGToPCM(oggData []byte, targetSampleRate int) ([]byte, error)
	EncodePCMToOGG(pcmData []byte, sampleRate int) ([]byte, error)
}

// AudioTranscriber converts audio to text via an external API (e.g. Gemini REST).
// Used when the Live API can't accept OGG directly and local codec is unavailable.
type AudioTranscriber interface {
	TranscribeAudio(ctx context.Context, audioData []byte, mimeType string) (string, error)
}

// AudioStore saves and loads audio files.
type AudioStore interface {
	Save(sessionID, msgID string, data []byte, ext string) (string, error)
	Load(path string) ([]byte, error)
}

// CanVoice returns true if this loop is configured for voice messaging.
// Audio codec is required for input (OGG→PCM conversion) since Gemini Live
// only accepts raw PCM. Output codec is optional — falls back to transcript text.
func (l *Loop) CanVoice() bool {
	return l.config.VoiceEnabled && l.liveProvider != nil && l.audioStore != nil
}

// RunVoice processes voice messages through a Gemini Live session.
// It handles the full cycle: decode audio → send to model → receive
// audio response → encode → store → return canonical messages.
func (l *Loop) RunVoice(ctx context.Context, sessionID string, history []canonical.Message, liveSessionPool LiveSessionPool) RunResult {
	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	l.onEvent(Event{Type: EventRunStarted, SessionID: sessionID, AgentID: l.config.AgentID})
	l.onEvent(Event{Type: EventVoiceStarted, SessionID: sessionID, AgentID: l.config.AgentID})

	// Sanitize history (same as text path).
	history = SanitizeHistory(history)

	var totalUsage canonical.Usage

	// Run PreContext middleware to get system prompt injections.
	hookData := &middleware.HookData{
		HookPoint: middleware.HookPreContext,
		Messages:  history,
		Metadata:  map[string]any{"session_id": sessionID, "agent_id": l.config.AgentID},
	}
	if l.preContext != nil {
		l.preContext(ctx, hookData, func(ctx context.Context, data *middleware.HookData) error {
			return nil
		})
	}

	// Build system prompt with injections.
	system := l.config.SystemPrompt
	if len(hookData.Injections) > 0 {
		system += "\n\n" + strings.Join(hookData.Injections, "\n\n")
	}

	// Get tool definitions.
	tools := l.toolRegistry.ListForAgent(
		l.config.ToolProfile,
		l.config.ToolDeny,
		l.config.AllowedMCPServers,
	)

	// Build live session config.
	cfg := provider.LiveSessionConfig{
		Model:              l.config.VoiceModel,
		SystemPrompt:       system,
		Tools:              tools,
		VoiceName:          l.config.VoiceName,
		ResponseModalities: []string{"AUDIO"},
	}

	voiceStart := time.Now()

	// Find the last audio message in history to send.
	var audioToSend *canonical.AudioSource
	for i := len(history) - 1; i >= 0; i-- {
		if audio := canonical.ExtractAudio(history[i]); audio != nil {
			audioToSend = audio
			break
		}
	}

	if audioToSend == nil {
		return RunResult{Error: fmt.Errorf("no audio found in history")}
	}

	// Load the audio file.
	loadStart := time.Now()
	audioData, err := l.audioStore.Load(audioToSend.FilePath)
	if err != nil {
		return RunResult{Error: fmt.Errorf("load audio: %w", err)}
	}
	log.Printf("voice[%s]: load audio %dB in %dms", sessionID, len(audioData), time.Since(loadStart).Milliseconds())

	mime := audioToSend.MimeType
	if mime == "" {
		mime = "audio/ogg"
	}

	// Pipeline: decode audio AND acquire session concurrently.
	type decodeResult struct {
		pcm []byte
		err error
	}
	type sessionResult struct {
		sess provider.LiveSession
		err  error
	}

	decodeCh := make(chan decodeResult, 1)
	sessCh := make(chan sessionResult, 1)

	// Start decode in background.
	go func() {
		if l.audioCodec != nil && (mime == "audio/ogg" || strings.HasPrefix(mime, "audio/ogg")) {
			decStart := time.Now()
			pcm, decErr := l.audioCodec.DecodeOGGToPCM(audioData, 16000)
			log.Printf("voice[%s]: decode OGG→PCM in %dms (%dB→%dB)", sessionID, time.Since(decStart).Milliseconds(), len(audioData), len(pcm))
			decodeCh <- decodeResult{pcm: pcm, err: decErr}
		} else {
			decodeCh <- decodeResult{err: fmt.Errorf("no codec or wrong mime")}
		}
	}()

	// Start session acquisition in background.
	go func() {
		sessStart := time.Now()
		s, sErr := liveSessionPool.GetOrCreate(ctx, sessionID, cfg)
		log.Printf("voice[%s]: session acquire in %dms", sessionID, time.Since(sessStart).Milliseconds())
		sessCh <- sessionResult{sess: s, err: sErr}
	}()

	// Wait for both.
	decRes := <-decodeCh
	sessRes := <-sessCh

	if sessRes.err != nil {
		errMsg := fmt.Errorf("voice session failed: %w", sessRes.err)
		l.onEvent(Event{Type: EventRunFailed, SessionID: sessionID, Error: errMsg})
		return RunResult{
			Messages: []canonical.Message{{
				Role:    "assistant",
				Content: []canonical.Content{{Type: "text", Text: "I'm having trouble with voice right now. Please try again or send a text message."}},
			}},
			Usage: totalUsage,
			Error: errMsg,
		}
	}
	sess := sessRes.sess

	// Send audio (or transcription fallback) to session.
	var sendErr error
	pcmSent := false

	if decRes.err == nil && len(decRes.pcm) > 0 {
		sendStart := time.Now()
		sendErr = sess.Send(ctx, provider.LiveMessage{Audio: decRes.pcm, AudioMime: "audio/pcm;rate=16000"})
		log.Printf("voice[%s]: send PCM in %dms", sessionID, time.Since(sendStart).Milliseconds())
		if sendErr == nil {
			pcmSent = true
		}
	} else {
		log.Printf("voice[%s]: decode unavailable (%v), falling back to transcription", sessionID, decRes.err)
	}

	if !pcmSent {
		// Fallback: transcribe audio via Gemini REST API, send text to Live session.
		if l.audioTranscriber == nil {
			return RunResult{Error: fmt.Errorf("voice: cannot process audio — no codec and no transcriber configured")}
		}
		trStart := time.Now()
		transcript, trErr := l.audioTranscriber.TranscribeAudio(ctx, audioData, mime)
		log.Printf("voice[%s]: transcribe in %dms", sessionID, time.Since(trStart).Milliseconds())
		if trErr != nil {
			return RunResult{Error: fmt.Errorf("voice transcribe: %w", trErr)}
		}
		log.Printf("voice[%s]: transcribed: %q", sessionID, transcript)

		sendErr = sess.Send(ctx, provider.LiveMessage{Text: transcript})
		if sendErr != nil {
			return RunResult{Error: fmt.Errorf("send text to live: %w", sendErr)}
		}
	}

	if sendErr != nil {
		return RunResult{Error: fmt.Errorf("send audio: %w", sendErr)}
	}

	log.Printf("voice[%s]: input ready in %dms (decode+session+send)", sessionID, time.Since(voiceStart).Milliseconds())

	// Collect response events until turn complete.
	inferenceStart := time.Now()
	var audioChunks [][]byte
	var inputTranscript, outputTranscript string
	var pendingToolCalls []canonical.ToolCall

	eventCh := sess.Receive()
	// Response collection timeout: half the overall timeout, capped at 60s.
	collectDur := l.config.Timeout / 2
	if collectDur > 60*time.Second {
		collectDur = 60 * time.Second
	}
	collectTimeout := time.After(collectDur)

	for done := false; !done; {
		select {
		case ev, ok := <-eventCh:
			if !ok {
				done = true
				break
			}

			switch ev.Type {
			case "audio":
				audioChunks = append(audioChunks, ev.Audio)
				l.onEvent(Event{Type: EventVoiceAudio, SessionID: sessionID})

			case "text":
				l.onEvent(Event{Type: EventVoiceText, SessionID: sessionID, Text: ev.Text})

			case "transcript":
				if ev.Transcript != nil {
					switch ev.Transcript.Direction {
					case "input":
						inputTranscript += ev.Transcript.Text
					case "output":
						outputTranscript += ev.Transcript.Text
					}
					l.onEvent(Event{Type: EventVoiceText, SessionID: sessionID, Text: ev.Transcript.Text})
				}

			case "tool_call":
				if ev.ToolCall != nil {
					pendingToolCalls = append(pendingToolCalls, *ev.ToolCall)
				}

			case "usage":
				if ev.Usage != nil {
					totalUsage.InputTokens += ev.Usage.InputTokens
					totalUsage.OutputTokens += ev.Usage.OutputTokens
				}

			case "done":
				done = true

			case "error":
				log.Printf("voice session error: %v", ev.Error)
				done = true

			case "go_away":
				log.Printf("voice session go_away: %v", ev.Error)
				done = true
			}

		case <-collectTimeout:
			log.Printf("voice response collection timed out for session %s", sessionID)
			done = true

		case <-ctx.Done():
			return RunResult{Error: ctx.Err(), Usage: totalUsage}
		}
	}

	// Handle tool calls if any.
	if len(pendingToolCalls) > 0 {
		var toolResults []canonical.ToolResult
		toolNames := make(map[string]string) // ToolCallID → function name for Gemini.
		for _, tc := range pendingToolCalls {
			l.onEvent(Event{Type: EventToolCall, SessionID: sessionID, ToolCall: &tc})
			toolNames[tc.ID] = tc.Name

			// Check consent before execution (same as text path).
			if consentResult := l.checkConsent(ctx, sessionID, tc, 0); consentResult != nil {
				toolResults = append(toolResults, *consentResult)
				continue
			}

			result, err := l.toolRegistry.Execute(ctx, tc.Name, tc.Input)
			if err != nil {
				result = &canonical.ToolResult{
					ToolCallID: tc.ID,
					Content:    fmt.Sprintf("Tool error: %v", err),
					IsError:    true,
				}
			} else {
				result.ToolCallID = tc.ID
			}

			// Run PostTool middleware.
			postData := &middleware.HookData{
				HookPoint:  middleware.HookPostTool,
				ToolCall:   &tc,
				ToolResult: result,
				Metadata:   map[string]any{"session_id": sessionID, "agent_id": l.config.AgentID},
			}
			if l.postTool != nil {
				l.postTool(ctx, postData, func(ctx context.Context, data *middleware.HookData) error {
					return nil
				})
				result = postData.ToolResult
			}

			l.onEvent(Event{Type: EventToolResult, SessionID: sessionID, ToolResult: result})
			toolResults = append(toolResults, *result)
		}

		// Send tool results back to the session (with correct function names).
		if err := sess.Send(ctx, provider.LiveMessage{ToolResults: toolResults, ToolNames: toolNames}); err != nil {
			log.Printf("voice: failed to send tool results: %v", err)
		}

		// Collect additional audio response after tool results.
		// (Model may generate more audio after processing tool results.)
		// Post-tool-call collection: quarter of overall timeout, capped at 30s.
		postToolDur := l.config.Timeout / 4
		if postToolDur > 30*time.Second {
			postToolDur = 30 * time.Second
		}
		collectMore := time.After(postToolDur)
		for moreDone := false; !moreDone; {
			select {
			case ev, ok := <-eventCh:
				if !ok {
					moreDone = true
					break
				}
				switch ev.Type {
				case "audio":
					audioChunks = append(audioChunks, ev.Audio)
				case "transcript":
					if ev.Transcript != nil && ev.Transcript.Direction == "output" {
						outputTranscript += ev.Transcript.Text
					}
				case "usage":
					if ev.Usage != nil {
						totalUsage.InputTokens += ev.Usage.InputTokens
						totalUsage.OutputTokens += ev.Usage.OutputTokens
					}
				case "done":
					moreDone = true
				case "error", "go_away":
					moreDone = true
				}
			case <-collectMore:
				moreDone = true
			case <-ctx.Done():
				return RunResult{Error: ctx.Err(), Usage: totalUsage}
			}
		}
	}

	log.Printf("voice[%s]: inference+streaming in %dms (%d audio chunks)", sessionID, time.Since(inferenceStart).Milliseconds(), len(audioChunks))

	// Concatenate audio chunks and encode to OGG (if codec available).
	encodeStart := time.Now()
	var responseMessages []canonical.Message

	if len(audioChunks) > 0 {
		totalPCM := concatenateBytes(audioChunks)

		// Try to encode PCM→OGG for voice note response.
		// Codec is optional — fall back to transcript text if unavailable.
		var encoded bool
		if l.audioCodec != nil {
			oggResponse, err := l.audioCodec.EncodePCMToOGG(totalPCM, 24000) // Gemini outputs at 24kHz
			if err != nil {
				log.Printf("voice: OGG encode failed: %v, falling back to transcript", err)
			} else {
				msgID := fmt.Sprintf("resp-%d", time.Now().UnixMilli())
				filePath, err := l.audioStore.Save(sessionID, msgID, oggResponse, "ogg")
				if err != nil {
					log.Printf("voice: save audio failed: %v", err)
				} else {
					var content []canonical.Content
					content = append(content, canonical.Content{
						Type: "audio",
						Audio: &canonical.AudioSource{
							FilePath:   filePath,
							MimeType:   "audio/ogg",
							DurationMs: pcmDurationMs(totalPCM, 24000),
							Transcript: outputTranscript,
							SampleRate: 24000,
						},
					})
					if outputTranscript != "" {
						content = append(content, canonical.Content{
							Type: "text",
							Text: outputTranscript,
						})
					}
					responseMessages = append(responseMessages, canonical.Message{
						Role:    "assistant",
						Content: content,
					})
					encoded = true
				}
			}
		}

		// Fallback: no codec or encoding failed — send transcript as text.
		if !encoded && outputTranscript != "" {
			responseMessages = append(responseMessages, canonical.Message{
				Role:    "assistant",
				Content: []canonical.Content{{Type: "text", Text: outputTranscript}},
			})
		} else if !encoded {
			// No codec AND no transcript — send a note about it.
			responseMessages = append(responseMessages, canonical.Message{
				Role:    "assistant",
				Content: []canonical.Content{{Type: "text", Text: "(Voice response received but could not be converted to audio. Install ffmpeg for voice note replies.)"}},
			})
		}
	} else if outputTranscript != "" {
		// No audio but we have a transcript — send as text.
		responseMessages = append(responseMessages, canonical.Message{
			Role:    "assistant",
			Content: []canonical.Content{{Type: "text", Text: outputTranscript}},
		})
	}

	// Update input transcript on the original audio message.
	if inputTranscript != "" && audioToSend != nil {
		audioToSend.Transcript = inputTranscript
	}

	log.Printf("voice[%s]: encode+save in %dms", sessionID, time.Since(encodeStart).Milliseconds())
	log.Printf("voice[%s]: total pipeline %dms", sessionID, time.Since(voiceStart).Milliseconds())

	l.onEvent(Event{Type: EventRunCompleted, SessionID: sessionID, AgentID: l.config.AgentID})
	return RunResult{Messages: responseMessages, Usage: totalUsage}
}

// VoiceSessionConfig returns the LiveSessionConfig for this loop, suitable for pre-warming.
// Returns nil if voice is not enabled.
func (l *Loop) VoiceSessionConfig() *provider.LiveSessionConfig {
	if !l.CanVoice() {
		return nil
	}
	tools := l.toolRegistry.ListForAgent(
		l.config.ToolProfile,
		l.config.ToolDeny,
		l.config.AllowedMCPServers,
	)
	return &provider.LiveSessionConfig{
		Model:              l.config.VoiceModel,
		SystemPrompt:       l.config.SystemPrompt,
		Tools:              tools,
		VoiceName:          l.config.VoiceName,
		ResponseModalities: []string{"AUDIO"},
	}
}

// LiveSessionPool abstracts the session pool to avoid importing the concrete package.
type LiveSessionPool interface {
	GetOrCreate(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig) (provider.LiveSession, error)
}

// LiveSessionWarmer is optionally implemented by pools that support pre-warming.
type LiveSessionWarmer interface {
	Warm(ctx context.Context, sessionID string, cfg provider.LiveSessionConfig)
}

// concatenateBytes joins multiple byte slices into one.
func concatenateBytes(chunks [][]byte) []byte {
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	result := make([]byte, 0, total)
	for _, c := range chunks {
		result = append(result, c...)
	}
	return result
}

// pcmDurationMs calculates the duration of PCM data in milliseconds.
func pcmDurationMs(data []byte, sampleRate int) int {
	if sampleRate == 0 || len(data) == 0 {
		return 0
	}
	samples := len(data) / 2 // 16-bit = 2 bytes per sample
	return (samples * 1000) / sampleRate
}
