package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel"
)

const (
	telegramAPI    = "https://api.telegram.org/bot"
	pollTimeout    = 30 // Long polling timeout in seconds.
	maxMessageLen  = 4096
)

// AudioStore saves and loads audio files for voice messaging.
type AudioStore interface {
	Save(sessionID, msgID string, data []byte, ext string) (string, error)
	Load(path string) ([]byte, error)
	Exists(path string) bool
}

// streamMsg tracks a message being progressively edited during streaming.
type streamMsg struct {
	chatID    string
	messageID int
	draftID   int // Unique draft identifier for sendMessageDraft (Bot API 9.5).
	text      string
	lastEdit  time.Time
}

// recentStream tracks a stream that was recently ended, to prevent
// sendResponse from sending a duplicate message.
type recentStream struct {
	endedAt time.Time
}

// Adapter implements the Channel interface for Telegram.
type Adapter struct {
	connID      string // Connection ID: "tg_abc123"
	token       string
	client      *http.Client
	msgBus      bus.MessageBus
	cancel      context.CancelFunc
	baseURL     string // For testing.
	botID       int64  // Bot user ID (from getMe).
	botUsername string // Bot username without @ (from getMe).
	audioStore  AudioStore // Optional: for voice message handling.

	// Consent support.
	ownerUserID string                // Platform user ID of the connection owner.
	consentCB   consentCallback       // Called when user responds to consent prompt.
	ownerStore  channel.OwnerStore    // For auto-capturing owner_user_id.

	// Streaming: progressive message editing.
	streamMu      sync.Mutex
	streams       map[string]*streamMsg    // sessionID → active stream
	recentStreams map[string]*recentStream // sessionID → recently ended (prevents duplicate sends)
}

// New creates a new Telegram adapter.
func New(connID, token string, opts ...Option) *Adapter {
	a := &Adapter{
		connID:  connID,
		token:   token,
		client:  &http.Client{Timeout: time.Duration(pollTimeout+10) * time.Second},
		baseURL: telegramAPI + token,
		streams:       make(map[string]*streamMsg),
		recentStreams: make(map[string]*recentStream),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Option configures the adapter.
type Option func(*Adapter)

// ConnID returns the adapter's connection ID.
func (a *Adapter) ConnID() string { return a.connID }

// WithBaseURL overrides the API URL (for testing).
func WithBaseURL(url string) Option {
	return func(a *Adapter) { a.baseURL = url }
}

// WithAudioStore enables voice message handling.
func WithAudioStore(store AudioStore) Option {
	return func(a *Adapter) { a.audioStore = store }
}

// WithOwnerUserID sets the owner user ID for consent verification.
func WithOwnerUserID(id string) Option {
	return func(a *Adapter) { a.ownerUserID = id }
}

// WithConsentCallback sets the function called when consent is granted/denied.
func WithConsentCallback(cb func(nonce string, granted bool, tier string)) Option {
	return func(a *Adapter) { a.consentCB = cb }
}

// WithOwnerStore enables auto-capture of owner_user_id on first inbound message.
func WithOwnerStore(s channel.OwnerStore) Option {
	return func(a *Adapter) { a.ownerStore = s }
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) Platform() string  { return "telegram" }

// Start begins long polling for updates.
func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus

	// Fetch bot info for mention matching.
	botUser, err := a.GetMe(ctx)
	if err != nil {
		log.Printf("telegram: warning: could not get bot info: %v", err)
	} else {
		a.botID = botUser.ID
		a.botUsername = botUser.Username
		log.Printf("telegram: connected as @%s (connection %s)", a.botUsername, a.connID)
	}

	adapterCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to outbound messages — only process messages for this connection.
	msgBus.SubscribeOutbound(adapterCtx, func(env bus.Envelope) {
		if env.Channel == a.connID {
			a.sendResponse(env)
		}
	})

	go a.pollLoop(adapterCtx)
	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

// GetMe calls the Telegram getMe API to fetch bot info.
func (a *Adapter) GetMe(ctx context.Context) (*TelegramUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.baseURL+"/getMe", nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool          `json:"ok"`
		Result *TelegramUser `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing getMe response: %w", err)
	}
	if !result.OK || result.Result == nil {
		return nil, fmt.Errorf("getMe failed: %s", string(body))
	}
	return result.Result, nil
}

func (a *Adapter) pollLoop(ctx context.Context) {
	offset := 0
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := a.getUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("telegram poll error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			if update.CallbackQuery != nil {
				a.handleCallbackQuery(ctx, update.CallbackQuery)
			} else if update.Message != nil {
				a.handleMessage(ctx, update.Message)
			}
			offset = update.UpdateID + 1
		}
	}
}

func (a *Adapter) getUpdates(ctx context.Context, offset int) ([]Update, error) {
	reqURL := fmt.Sprintf("%s/getUpdates?timeout=%d&offset=%d&allowed_updates=%s",
		a.baseURL, pollTimeout, offset, url.QueryEscape(`["message","callback_query"]`))

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool     `json:"ok"`
		Result []Update `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("telegram API error: %s", string(body))
	}

	return result.Result, nil
}

const maxVoiceDurationSec = 600 // 10 minutes max voice message.

func (a *Adapter) handleMessage(ctx context.Context, msg *TelegramMessage) {
	// Auto-capture owner on first inbound message.
	if msg.From != nil && a.ownerUserID == "" && a.ownerStore != nil {
		userID := strconv.FormatInt(msg.From.ID, 10)
		channel.CaptureOwner(ctx, a.ownerStore, a.connID, a.ownerUserID, userID)
		a.ownerUserID = userID
	}

	// Handle voice messages.
	hasAudio := false
	canonicalMsg := a.normalizeMessageWithAudio(ctx, msg, &hasAudio)

	// Detect kind.
	kind := "dm"
	if msg.Chat.Type != "private" {
		kind = "group"
	}

	// Detect mention (only relevant for groups).
	mentioned := kind == "dm" // DMs are always "mentioned".
	if kind == "group" && a.botUsername != "" {
		for _, entity := range msg.Entities {
			if entity.Type == "mention" && entity.Offset+entity.Length <= len(msg.Text) {
				mentionText := msg.Text[entity.Offset : entity.Offset+entity.Length]
				if strings.EqualFold(mentionText, "@"+a.botUsername) {
					mentioned = true
				}
			}
		}
		// Also check reply to bot's message.
		if !mentioned && msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.ID == a.botID {
			mentioned = true
		}
	}

	// Detect thread/topic.
	threadID := ""
	if msg.MessageThreadID != 0 {
		threadID = bus.SanitizeThreadID(strconv.Itoa(msg.MessageThreadID))
	}

	chatIDStr := strconv.FormatInt(msg.Chat.ID, 10)

	// Show typing/recording indicator while the agent processes.
	if hasAudio {
		a.sendChatAction(chatIDStr, "record_voice")
	} else {
		a.sendChatAction(chatIDStr, "typing")
	}

	a.msgBus.PublishInbound(ctx, bus.Envelope{
		Channel:   a.connID,
		ChatID:    chatIDStr,
		Kind:      kind,
		ThreadID:  threadID,
		Mentioned: mentioned,
		HasAudio:  hasAudio,
		Messages:  []canonical.Message{canonicalMsg},
		Metadata: map[string]string{
			"telegram_message_id": strconv.Itoa(msg.MessageID),
			"telegram_user_id":    strconv.FormatInt(msg.From.ID, 10),
		},
	})
}

func (a *Adapter) sendResponse(env bus.Envelope) {
	for _, msg := range env.Messages {
		// Check for audio content first.
		if audio := canonical.ExtractAudio(msg); audio != nil {
			// If streaming was active, end it first.
			a.endStream(env.SessionID)
			a.sendChatAction(env.ChatID, "upload_voice")
			if err := a.sendVoice(env.ChatID, audio); err != nil {
				log.Printf("telegram: sendVoice failed: %v, falling back to text", err)
				if audio.Transcript != "" {
					a.sendMessage(env.ChatID, audio.Transcript)
				}
			}
			continue
		}

		text := extractText(msg)
		if text == "" {
			continue
		}

		// If streaming was active for this session, the text is already
		// in the progressively edited message. Just finalize with formatting.
		a.streamMu.Lock()
		sm, hasStream := a.streams[env.SessionID]
		_, wasRecentStream := a.recentStreams[env.SessionID]
		a.streamMu.Unlock()

		if hasStream && sm != nil {
			// Stream still active — update text and finalize.
			a.streamMu.Lock()
			sm.text = text
			a.streamMu.Unlock()
			a.endStream(env.SessionID)
			continue
		}
		if wasRecentStream {
			// Stream already ended (event forwarder handled it).
			// Clean up and skip to avoid duplicate message.
			a.streamMu.Lock()
			delete(a.recentStreams, env.SessionID)
			a.streamMu.Unlock()
			continue
		}

		// No streaming — send normally.
		chunks := chunkText(text, maxMessageLen)
		if len(chunks) > 1 {
			a.sendChatAction(env.ChatID, "typing")
		}
		for _, chunk := range chunks {
			a.sendMessage(env.ChatID, chunk)
		}
	}
}

// OnAgentEvent handles agent events for progressive message editing.
// Called from the pipeline's onEvent callback for Telegram sessions.
func (a *Adapter) OnAgentEvent(sessionID, chatID, eventType, text string) {
	switch eventType {
	case "chunk":
		a.streamChunk(sessionID, chatID, text)
	case "run.completed", "run.failed":
		a.endStream(sessionID)
	}
}

// streamChunk accumulates text and sends draft updates to Telegram.
// Uses sendMessageDraft (Bot API 9.5) for real-time streaming — shows a
// draft bubble that updates with animation when using the same draft_id.
// Throttled to max 2 drafts per second.
func (a *Adapter) streamChunk(sessionID, chatID, delta string) {
	a.streamMu.Lock()
	sm, exists := a.streams[sessionID]

	if !exists {
		// First chunk — generate a draft_id and initialize stream.
		// draft_id must be non-zero; use lower 31 bits of current time as unique ID.
		draftID := int(time.Now().UnixMilli() & 0x7FFFFFFF)
		if draftID == 0 {
			draftID = 1
		}
		a.streams[sessionID] = &streamMsg{
			chatID:   chatID,
			draftID:  draftID,
			text:     delta,
			lastEdit: time.Now(),
		}
		a.streamMu.Unlock()
		a.sendMessageDraft(chatID, draftID, delta)
		return
	}

	sm.text += delta

	// Throttle: send draft at most every 500ms.
	if time.Since(sm.lastEdit) < 500*time.Millisecond {
		a.streamMu.Unlock()
		return
	}
	text := sm.text
	draftID := sm.draftID
	sm.lastEdit = time.Now()
	a.streamMu.Unlock()

	// Update the draft with accumulated text (same draft_id = animated update).
	a.sendMessageDraft(chatID, draftID, text)
}

// endStream finalizes streaming: send the real message with MarkdownV2 formatting.
// The draft is replaced by the final sendMessage call.
func (a *Adapter) endStream(sessionID string) {
	a.streamMu.Lock()
	sm, exists := a.streams[sessionID]
	if !exists {
		a.streamMu.Unlock()
		return
	}
	delete(a.streams, sessionID)
	// Mark as recently ended so sendResponse skips duplicate send.
	a.recentStreams[sessionID] = &recentStream{endedAt: time.Now()}
	a.streamMu.Unlock()

	// Finalize: send the real message with MarkdownV2 (replaces the draft).
	a.sendMessageFinal(sm.chatID, sm.draftID, sm.text)
}

// sendMessageDraft sends or updates a draft message in the chat.
// Bot API 9.5: shows a draft bubble that updates with animation when
// called with the same draft_id. draft_id must be non-zero.
func (a *Adapter) sendMessageDraft(chatID string, draftID int, text string) {
	if len(text) > maxMessageLen {
		text = text[:maxMessageLen]
	}
	params := url.Values{
		"chat_id":    {chatID},
		"draft_id":   {strconv.Itoa(draftID)},
		"text":       {toTelegramMarkdown(text)},
		"parse_mode": {"MarkdownV2"},
	}
	resp, err := a.client.PostForm(a.baseURL+"/sendMessageDraft", params)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// sendPlainMessage sends a plain text message and returns its message_id.
func (a *Adapter) sendPlainMessage(chatID, text string) int {
	params := url.Values{
		"chat_id": {chatID},
		"text":    {text},
	}
	resp, err := a.client.PostForm(a.baseURL+"/sendMessage", params)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.Result.MessageID
}

// editMessageText edits an existing message with plain text (no formatting).
func (a *Adapter) editMessageText(chatID string, messageID int, text string) {
	if len(text) > maxMessageLen {
		text = text[:maxMessageLen]
	}
	params := url.Values{
		"chat_id":    {chatID},
		"message_id": {strconv.Itoa(messageID)},
		"text":       {text},
	}
	resp, err := a.client.PostForm(a.baseURL+"/editMessageText", params)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// sendChatAction sends a typing/recording indicator to the chat.
// action: "typing", "record_voice", "upload_voice", "upload_document", etc.
// The indicator auto-expires after 5 seconds or when a message is sent.
func (a *Adapter) sendChatAction(chatID, action string) {
	params := url.Values{
		"chat_id": {chatID},
		"action":  {action},
	}
	resp, err := a.client.PostForm(a.baseURL+"/sendChatAction", params)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// sendMessageFinal finalizes a draft by sending a permanent message with the same draft_id.
// Bot API: sendMessage with draft_id replaces the ephemeral draft with a real message.
func (a *Adapter) sendMessageFinal(chatID string, draftID int, text string) error {
	params := url.Values{
		"chat_id":    {chatID},
		"text":       {toTelegramMarkdown(text)},
		"parse_mode": {"MarkdownV2"},
		"draft_id":   {strconv.Itoa(draftID)},
	}

	resp, err := a.client.PostForm(a.baseURL+"/sendMessage", params)
	if err != nil {
		return fmt.Errorf("sending final message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(body), "can't parse") {
			params.Set("parse_mode", "")
			resp2, err := a.client.PostForm(a.baseURL+"/sendMessage", params)
			if err != nil {
				return err
			}
			resp2.Body.Close()
		}
		return fmt.Errorf("telegram error: %s", string(body))
	}

	return nil
}

func (a *Adapter) sendMessage(chatID, text string) error {
	params := url.Values{
		"chat_id":    {chatID},
		"text":       {toTelegramMarkdown(text)},
		"parse_mode": {"MarkdownV2"},
	}

	resp, err := a.client.PostForm(a.baseURL+"/sendMessage", params)
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		// Retry without Markdown if parsing fails.
		if strings.Contains(string(body), "can't parse") {
			params.Set("parse_mode", "")
			resp2, err := a.client.PostForm(a.baseURL+"/sendMessage", params)
			if err != nil {
				return err
			}
			resp2.Body.Close()
		}
		return fmt.Errorf("telegram error: %s", string(body))
	}

	return nil
}

func extractText(msg canonical.Message) string {
	var parts []string
	for _, c := range msg.Content {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func chunkText(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			chunks = append(chunks, text)
			break
		}
		// Find a good break point.
		breakAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			breakAt = idx + 1
		}
		chunks = append(chunks, text[:breakAt])
		text = text[breakAt:]
	}
	return chunks
}

// normalizeMessageWithAudio converts a Telegram message to canonical form,
// handling voice messages when an audio store is available.
func (a *Adapter) normalizeMessageWithAudio(ctx context.Context, msg *TelegramMessage, hasAudio *bool) canonical.Message {
	var content []canonical.Content

	if msg.Text != "" {
		content = append(content, canonical.Content{Type: "text", Text: msg.Text})
	}

	if msg.Caption != "" && msg.Text == "" {
		content = append(content, canonical.Content{Type: "text", Text: msg.Caption})
	}

	// Handle voice messages.
	if msg.Voice != nil && a.audioStore != nil {
		if msg.Voice.Duration > maxVoiceDurationSec {
			content = append(content, canonical.Content{
				Type: "text",
				Text: fmt.Sprintf("(Voice message too long: %d seconds. Maximum is %d seconds.)", msg.Voice.Duration, maxVoiceDurationSec),
			})
		} else {
			audioContent := a.downloadVoice(ctx, msg)
			if audioContent != nil {
				content = append(content, *audioContent)
				*hasAudio = true
			} else {
				content = append(content, canonical.Content{
					Type: "text",
					Text: "(Voice message received but could not be downloaded)",
				})
			}
		}
	} else if msg.Voice != nil && a.audioStore == nil {
		content = append(content, canonical.Content{
			Type: "text",
			Text: "(Voice message received — voice support not configured)",
		})
	}

	// Handle video notes (circular video messages).
	// Video notes are MP4 format — not compatible with the OGG voice pipeline.
	// Treat as text description instead of audio.
	if msg.VideoNote != nil {
		content = append(content, canonical.Content{
			Type: "text",
			Text: fmt.Sprintf("(Video note received, %d seconds. Video notes are not supported for voice — please send a voice message instead.)", msg.VideoNote.Duration),
		})
	}

	// Handle photos (take the largest).
	if len(msg.Photo) > 0 {
		content = append(content, canonical.Content{
			Type: "text",
			Text: "[Image attached]",
		})
	}

	if len(content) == 0 {
		content = append(content, canonical.Content{Type: "text", Text: "(empty message)"})
	}

	return canonical.Message{Role: "user", Content: content}
}

// normalizeMessage is the legacy version without audio support (for tests).
func normalizeMessage(msg *TelegramMessage) canonical.Message {
	a := &Adapter{}
	hasAudio := false
	return a.normalizeMessageWithAudio(context.Background(), msg, &hasAudio)
}

// downloadVoice downloads a Telegram voice message and stores it.
func (a *Adapter) downloadVoice(ctx context.Context, msg *TelegramMessage) *canonical.Content {
	if msg.Voice == nil {
		return nil
	}

	data, err := a.downloadFile(ctx, msg.Voice.FileID)
	if err != nil {
		log.Printf("telegram: voice download failed: %v", err)
		return nil
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	msgID := strconv.Itoa(msg.MessageID)

	// Use chatID as session placeholder — pipeline will resolve the actual session.
	filePath, err := a.audioStore.Save(chatID, msgID, data, "ogg")
	if err != nil {
		log.Printf("telegram: voice save failed: %v", err)
		return nil
	}

	return &canonical.Content{
		Type: "audio",
		Audio: &canonical.AudioSource{
			FilePath:   filePath,
			MimeType:   "audio/ogg",
			DurationMs: msg.Voice.Duration * 1000,
		},
	}
}

// downloadVideoNote downloads a Telegram video note and stores it.
func (a *Adapter) downloadVideoNote(ctx context.Context, msg *TelegramMessage) *canonical.Content {
	if msg.VideoNote == nil {
		return nil
	}

	data, err := a.downloadFile(ctx, msg.VideoNote.FileID)
	if err != nil {
		log.Printf("telegram: video note download failed: %v", err)
		return nil
	}

	chatID := strconv.FormatInt(msg.Chat.ID, 10)
	msgID := strconv.Itoa(msg.MessageID)

	filePath, err := a.audioStore.Save(chatID, msgID, data, "mp4")
	if err != nil {
		log.Printf("telegram: video note save failed: %v", err)
		return nil
	}

	return &canonical.Content{
		Type: "audio",
		Audio: &canonical.AudioSource{
			FilePath:   filePath,
			MimeType:   "video/mp4",
			DurationMs: msg.VideoNote.Duration * 1000,
		},
	}
}

// downloadFile downloads a file from Telegram using the getFile API.
func (a *Adapter) downloadFile(ctx context.Context, fileID string) ([]byte, error) {
	// Step 1: Get file path.
	reqURL := fmt.Sprintf("%s/getFile?file_id=%s", a.baseURL, url.QueryEscape(fileID))
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing getFile: %w", err)
	}
	if !result.OK || result.Result.FilePath == "" {
		return nil, fmt.Errorf("getFile failed: %s", string(body))
	}

	// Step 2: Download the file.
	// File URL format: https://api.telegram.org/file/bot{token}/{file_path}
	fileURL := fmt.Sprintf("https://api.telegram.org/file/bot%s/%s", a.token, result.Result.FilePath)
	// For testing, use baseURL.
	if !strings.HasPrefix(a.baseURL, telegramAPI) {
		fileURL = fmt.Sprintf("%s/file/%s", a.baseURL, result.Result.FilePath)
	}

	fileReq, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return nil, err
	}

	fileResp, err := a.client.Do(fileReq)
	if err != nil {
		return nil, err
	}
	defer fileResp.Body.Close()

	if fileResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("file download HTTP %d", fileResp.StatusCode)
	}

	return io.ReadAll(fileResp.Body)
}

// sendVoice sends an OGG Opus voice message to a Telegram chat.
func (a *Adapter) sendVoice(chatID string, audio *canonical.AudioSource) error {
	if audio == nil || audio.FilePath == "" {
		return fmt.Errorf("no audio file path")
	}

	// Load the audio file.
	var data []byte
	var err error
	if a.audioStore != nil {
		data, err = a.audioStore.Load(audio.FilePath)
	} else {
		return fmt.Errorf("no audio store configured")
	}
	if err != nil {
		return fmt.Errorf("loading audio file: %w", err)
	}

	// Build multipart form for sendVoice API.
	return a.sendVoiceMultipart(chatID, data, audio.DurationMs/1000)
}

// sendVoiceMultipart sends a voice message using multipart/form-data.
func (a *Adapter) sendVoiceMultipart(chatID string, oggData []byte, durationSec int) error {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	writer.WriteField("chat_id", chatID)
	if durationSec > 0 {
		writer.WriteField("duration", strconv.Itoa(durationSec))
	}

	part, err := writer.CreateFormFile("voice", "voice.ogg")
	if err != nil {
		return fmt.Errorf("creating form file: %w", err)
	}
	if _, err := part.Write(oggData); err != nil {
		return fmt.Errorf("writing voice data: %w", err)
	}
	writer.Close()

	req, err := http.NewRequest("POST", a.baseURL+"/sendVoice", &buf)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendVoice: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("sendVoice HTTP %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// Telegram API types.

type Update struct {
	UpdateID      int              `json:"update_id"`
	Message       *TelegramMessage `json:"message"`
	CallbackQuery *CallbackQuery   `json:"callback_query"`
}

type TelegramMessage struct {
	MessageID       int               `json:"message_id"`
	MessageThreadID int               `json:"message_thread_id"`
	From            *TelegramUser     `json:"from"`
	Chat            TelegramChat      `json:"chat"`
	Text            string            `json:"text"`
	Caption         string            `json:"caption"`
	Photo           []PhotoSize       `json:"photo"`
	Voice           *VoiceMessage     `json:"voice"`
	VideoNote       *VideoNote        `json:"video_note"`
	Entities        []MessageEntity   `json:"entities"`
	ReplyToMessage  *TelegramMessage  `json:"reply_to_message"`
}

// VoiceMessage represents a Telegram voice message (OGG Opus).
type VoiceMessage struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Duration     int    `json:"duration"`      // Duration in seconds.
	MimeType     string `json:"mime_type"`      // Usually "audio/ogg".
	FileSize     int    `json:"file_size"`
}

// VideoNote represents a Telegram video note (circular video message).
type VideoNote struct {
	FileID       string `json:"file_id"`
	FileUniqueID string `json:"file_unique_id"`
	Length       int    `json:"length"`
	Duration     int    `json:"duration"`
	FileSize     int    `json:"file_size"`
}

type MessageEntity struct {
	Type   string `json:"type"`
	Offset int    `json:"offset"`
	Length int    `json:"length"`
}

type TelegramUser struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
}

type TelegramChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type PhotoSize struct {
	FileID   string `json:"file_id"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	FileSize int    `json:"file_size"`
}
