package discord

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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/channel/typing"
)

const (
	discordAPIBase   = "https://discord.com/api/v10"
	gatewayURL       = "wss://gateway.discord.gg/?v=10&encoding=json"
	heartbeatDefault = 41250 * time.Millisecond
)

const (
	maxDiscordLen = 2000 // Discord message character limit.
)

// streamState tracks a message being progressively edited during streaming.
type streamState struct {
	channelID    string
	messageID    string // Discord message ID (snowflake string).
	text         string
	lastEdit     time.Time
	isToolStatus bool // True if message currently shows tool status.
}

// Adapter implements channel.Channel for Discord.
type Adapter struct {
	connID      string // Connection ID: "dc_abc123"
	token       string
	msgBus      bus.MessageBus
	client      *http.Client
	botID       string
	cancel      context.CancelFunc
	mu          sync.Mutex
	ownerUserID string              // Platform user ID of the connection owner.
	consentCB   ConsentCallback    // Called when user responds to consent prompt.
	ownerStore  channel.OwnerStore // For auto-capturing owner_user_id.
	apiBase     string             // Overridable for testing.

	// Streaming: progressive message editing.
	streamMu sync.Mutex
	streams  map[string]*streamState // sessionID → active stream

	// Tool activity: typing, tool status, reactions.
	typingMu     sync.Mutex
	typingCtrl   map[string]*typing.Controller // sessionID → typing controller
	reactionMu   sync.Mutex
	reactionsOff map[string]bool // channelID → true if reactions disabled
	lastReaction map[string]string // sessionID → last emoji (for removal)
}

// New creates a new Discord adapter.
func New(connID, token string) *Adapter {
	return &Adapter{
		connID:       connID,
		token:        token,
		client:       &http.Client{Timeout: 30 * time.Second},
		apiBase:      discordAPIBase,
		streams:      make(map[string]*streamState),
		typingCtrl:   make(map[string]*typing.Controller),
		reactionsOff: make(map[string]bool),
		lastReaction: make(map[string]string),
	}
}

// WithAPIBase overrides the Discord API base URL (for testing).
func WithAPIBase(url string) func(*Adapter) {
	return func(a *Adapter) { a.apiBase = url }
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) ConnID() string    { return a.connID }
func (a *Adapter) Platform() string  { return "discord" }

// Start connects to Discord and begins receiving messages.
func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus

	// Get bot info.
	botUser, err := a.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("getting bot info: %w", err)
	}
	a.botID = botUser.ID
	log.Printf("discord: connected as %s#%s (connection %s)", botUser.Username, botUser.Discriminator, a.connID)

	discordCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to outbound — only process messages for this connection.
	msgBus.SubscribeOutbound(discordCtx, func(env bus.Envelope) {
		if env.Channel == a.connID {
			a.sendResponse(env)
		}
	})

	go a.pollLoop(discordCtx)

	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *Adapter) pollLoop(ctx context.Context) {
	log.Println("discord: polling mode active (for full real-time, Gateway WebSocket needed)")
	<-ctx.Done()
}

func (a *Adapter) sendResponse(env bus.Envelope) {
	defer a.markTypingDispatchIdle(env.SessionID)

	for _, msg := range env.Messages {
		if msg.Role != "assistant" {
			continue
		}
		var text string
		for _, c := range msg.Content {
			if c.Type == "text" {
				text += c.Text
			}
		}
		if text == "" {
			continue
		}

		// If streaming was active, finalize it.
		a.streamMu.Lock()
		sm, hasStream := a.streams[env.SessionID]
		a.streamMu.Unlock()

		if hasStream && sm != nil {
			a.streamMu.Lock()
			sm.text = text
			a.streamMu.Unlock()
			a.endStream(env.SessionID)
			continue
		}

		// No streaming — send normally.
		chunks := chunkText(text, maxDiscordLen)
		for _, chunk := range chunks {
			a.sendMessage(env.ChatID, chunk)
		}
	}
}

// sendMessage sends a message and returns the message ID (snowflake string).
func (a *Adapter) sendMessage(channelID, content string) (string, error) {
	payload, _ := json.Marshal(map[string]string{"content": content})
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/channels/%s/messages", a.apiBase, channelID),
		bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bot "+a.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("discord API error (%d): %s", resp.StatusCode, body)
	}

	var result struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &result)
	return result.ID, nil
}

// editMessage edits an existing message.
func (a *Adapter) editMessage(channelID, messageID, content string) error {
	if len(content) > maxDiscordLen {
		content = content[:maxDiscordLen]
	}
	payload, _ := json.Marshal(map[string]string{"content": content})
	req, err := http.NewRequest("PATCH",
		fmt.Sprintf("%s/channels/%s/messages/%s", a.apiBase, channelID, messageID),
		bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+a.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord edit error (%d): %s", resp.StatusCode, body)
	}
	return nil
}

// sendTypingIndicator triggers the typing indicator in a channel.
// Discord typing expires after 10 seconds.
func (a *Adapter) sendTypingIndicator(channelID string) error {
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/channels/%s/typing", a.apiBase, channelID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+a.token)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// addReaction adds an emoji reaction to a message.
func (a *Adapter) addReaction(channelID, messageID, emoji string) error {
	a.reactionMu.Lock()
	if a.reactionsOff[channelID] {
		a.reactionMu.Unlock()
		return nil
	}
	a.reactionMu.Unlock()

	// URL-encode the emoji for the path.
	req, err := http.NewRequest("PUT",
		fmt.Sprintf("%s/channels/%s/messages/%s/reactions/%s/@me",
			a.apiBase, channelID, messageID, encodeEmoji(emoji)), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+a.token)
	req.Header.Set("Content-Length", "0")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusBadRequest {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[discord] reactions disabled for channel %s: %s", channelID, string(body))
		a.reactionMu.Lock()
		a.reactionsOff[channelID] = true
		a.reactionMu.Unlock()
	}
	return nil
}

// removeReaction removes the bot's reaction from a message.
func (a *Adapter) removeReaction(channelID, messageID, emoji string) error {
	req, err := http.NewRequest("DELETE",
		fmt.Sprintf("%s/channels/%s/messages/%s/reactions/%s/@me",
			a.apiBase, channelID, messageID, encodeEmoji(emoji)), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bot "+a.token)

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// encodeEmoji URL-encodes an emoji for Discord's reaction endpoint.
// Unicode emojis need percent-encoding; custom emojis use name:id format.
func encodeEmoji(emoji string) string {
	return url.PathEscape(emoji)
}

// --- Streaming infrastructure ---

// OnAgentEvent handles agent events for progressive message editing and typing.
func (a *Adapter) OnAgentEvent(sessionID, channelID, eventType, text string) {
	switch eventType {
	case "run.started":
		a.startTyping(sessionID, channelID)
	case "chunk":
		a.streamChunk(sessionID, channelID, text)
	case "run.completed":
		log.Printf("[discord-stream] OnAgentEvent: %s for session %s", eventType, sessionID)
		a.markTypingRunComplete(sessionID)
		a.endStream(sessionID)
	case "run.failed":
		log.Printf("[discord-stream] OnAgentEvent: %s for session %s", eventType, sessionID)
		a.markTypingRunComplete(sessionID)
		a.endStream(sessionID)
	}
}

// streamChunk accumulates text and sends/edits a message progressively.
func (a *Adapter) streamChunk(sessionID, channelID, delta string) {
	a.streamMu.Lock()
	sm, exists := a.streams[sessionID]

	if !exists {
		// First chunk — send a new message.
		a.streams[sessionID] = &streamState{
			channelID: channelID,
			text:      delta,
			lastEdit:  time.Now(),
		}
		a.streamMu.Unlock()

		msgID, err := a.sendMessage(channelID, delta)
		if err != nil {
			log.Printf("[discord-stream] first chunk send error: %v", err)
			return
		}
		a.streamMu.Lock()
		if s, ok := a.streams[sessionID]; ok {
			s.messageID = msgID
		}
		a.streamMu.Unlock()
		return
	}

	sm.text += delta

	// Throttle edits to every 1s.
	if time.Since(sm.lastEdit) < 1*time.Second {
		a.streamMu.Unlock()
		return
	}

	text := sm.text
	msgID := sm.messageID
	chID := sm.channelID
	sm.lastEdit = time.Now()
	a.streamMu.Unlock()

	if msgID == "" {
		return // Message not created yet.
	}

	// Truncate for Discord's limit.
	if len(text) > maxDiscordLen {
		text = text[:maxDiscordLen]
	}
	a.editMessage(chID, msgID, text)
}

// endStream finalizes the streamed message with the final text.
func (a *Adapter) endStream(sessionID string) {
	a.streamMu.Lock()
	sm, exists := a.streams[sessionID]
	if !exists {
		a.streamMu.Unlock()
		return
	}
	delete(a.streams, sessionID)
	a.streamMu.Unlock()

	if sm.messageID == "" || sm.text == "" {
		return
	}

	// Final edit with complete text.
	if len(sm.text) <= maxDiscordLen {
		a.editMessage(sm.channelID, sm.messageID, sm.text)
	} else {
		// Text exceeds limit — edit first chunk, send remainder.
		a.editMessage(sm.channelID, sm.messageID, sm.text[:maxDiscordLen])
		remainder := sm.text[maxDiscordLen:]
		chunks := chunkText(remainder, maxDiscordLen)
		for _, chunk := range chunks {
			a.sendMessage(sm.channelID, chunk)
		}
	}
}

// --- Tool status via message editing ---

// OnToolStatus handles tool status updates from the ToolStatusTracker.
func (a *Adapter) OnToolStatus(sessionID, channelID string, update toolstatus.StatusUpdate) {
	if update.Done {
		// Clear tool status — streaming will take over.
		a.streamMu.Lock()
		sm, exists := a.streams[sessionID]
		if exists && sm.isToolStatus {
			// Keep the stream but mark it as no longer tool status.
			sm.isToolStatus = false
		}
		a.streamMu.Unlock()
		return
	}

	if update.Text == "" {
		return
	}

	a.streamMu.Lock()
	sm, exists := a.streams[sessionID]
	if !exists {
		// First tool status — send a new message.
		a.streams[sessionID] = &streamState{
			channelID:    channelID,
			text:         update.Text,
			lastEdit:     time.Now(),
			isToolStatus: true,
		}
		a.streamMu.Unlock()

		msgID, err := a.sendMessage(channelID, update.Text)
		if err != nil {
			log.Printf("[discord] tool status send error: %v", err)
			return
		}
		a.streamMu.Lock()
		if s, ok := a.streams[sessionID]; ok {
			s.messageID = msgID
		}
		a.streamMu.Unlock()
		return
	}

	// Update existing tool status message.
	if !sm.isToolStatus {
		a.streamMu.Unlock()
		return // Text streaming already took over.
	}
	sm.text = update.Text
	msgID := sm.messageID
	a.streamMu.Unlock()

	if msgID != "" {
		a.editMessage(channelID, msgID, update.Text)
	}
}

// --- Emoji reactions ---

// OnReaction handles reaction updates from the ToolStatusTracker.
func (a *Adapter) OnReaction(channelID, userMsgID string, update toolstatus.ReactionUpdate) {
	if userMsgID == "" || update.Emoji == "" {
		return
	}

	key := channelID + ":" + userMsgID

	// Remove previous reaction if different.
	a.reactionMu.Lock()
	prev := a.lastReaction[key]
	if update.Phase == toolstatus.PhaseDone || update.Phase == toolstatus.PhaseError {
		// Terminal phase — clean up tracking entry after setting final reaction.
		delete(a.lastReaction, key)
	} else {
		a.lastReaction[key] = update.Emoji
	}
	a.reactionMu.Unlock()

	if prev != "" && prev != update.Emoji {
		a.removeReaction(channelID, userMsgID, prev)
	}

	a.addReaction(channelID, userMsgID, update.Emoji)
}

// --- Typing controller ---

func (a *Adapter) startTyping(sessionID, channelID string) {
	a.typingMu.Lock()
	if old, ok := a.typingCtrl[sessionID]; ok {
		old.Stop()
	}
	ctrl := typing.NewController(
		func() error {
			return a.sendTypingIndicator(channelID)
		},
		nil, // No explicit stop for Discord.
		9000,  // Keepalive every 9s (Discord typing expires after 10s).
		60000, // TTL safety: 60s max.
	)
	a.typingCtrl[sessionID] = ctrl
	a.typingMu.Unlock()
	ctrl.Start()
}

func (a *Adapter) markTypingRunComplete(sessionID string) {
	a.typingMu.Lock()
	ctrl, ok := a.typingCtrl[sessionID]
	a.typingMu.Unlock()
	if ok {
		ctrl.MarkRunComplete()
	}
}

func (a *Adapter) markTypingDispatchIdle(sessionID string) {
	a.typingMu.Lock()
	ctrl, ok := a.typingCtrl[sessionID]
	if ok {
		delete(a.typingCtrl, sessionID)
	}
	a.typingMu.Unlock()
	if ok {
		ctrl.MarkDispatchIdle()
	}
}

// GetMe fetches the bot's user info from Discord.
func (a *Adapter) GetMe(ctx context.Context) (*DiscordUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", a.apiBase+"/users/@me", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bot "+a.token)

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var user DiscordUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, err
	}
	return &user, nil
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
		breakAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			breakAt = idx + 1
		}
		chunks = append(chunks, text[:breakAt])
		text = text[breakAt:]
	}
	return chunks
}

// Discord API types.

type DiscordUser struct {
	ID            string `json:"id"`
	Username      string `json:"username"`
	Discriminator string `json:"discriminator"`
	Bot           bool   `json:"bot"`
}

type DiscordMessage struct {
	ID        string          `json:"id"`
	ChannelID string          `json:"channel_id"`
	GuildID   string          `json:"guild_id"`
	Author    DiscordUser     `json:"author"`
	Content   string          `json:"content"`
	Timestamp string          `json:"timestamp"`
	Mentions  []DiscordUser   `json:"mentions"`
}

// DetectKind returns "dm" for DM channels or "group" for guild channels.
func DetectKind(msg DiscordMessage) string {
	if msg.GuildID == "" {
		return "dm"
	}
	return "group"
}

// DetectMentioned returns true if the bot is mentioned in the message.
func DetectMentioned(msg DiscordMessage, botID string) bool {
	for _, m := range msg.Mentions {
		if m.ID == botID {
			return true
		}
	}
	return false
}

// NormalizeMessage converts a Discord message to canonical form.
func NormalizeMessage(msg DiscordMessage) canonical.Message {
	return canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: msg.Content}},
	}
}

// ToEnvelope converts a Discord message to a bus Envelope with kind/mention detection.
func (a *Adapter) ToEnvelope(msg DiscordMessage) bus.Envelope {
	kind := DetectKind(msg)
	mentioned := kind == "dm"
	if kind == "group" {
		mentioned = DetectMentioned(msg, a.botID)
	}

	// Auto-capture owner on first inbound message.
	if a.ownerUserID == "" && msg.Author.ID != "" && a.ownerStore != nil {
		channel.CaptureOwner(context.Background(), a.ownerStore, a.connID, a.ownerUserID, msg.Author.ID)
		a.ownerUserID = msg.Author.ID
	}

	return bus.Envelope{
		Channel:   a.connID,
		ChatID:    msg.ChannelID,
		Kind:      kind,
		Mentioned: mentioned,
		Messages:  []canonical.Message{NormalizeMessage(msg)},
		Metadata: map[string]string{
			"discord_message_id": msg.ID,
			"discord_author_id":  msg.Author.ID,
		},
	}
}

// SendMedia implements channel.MediaSender for Discord.
// Discord sends all file types as attachments — the client renders based on MIME.
func (a *Adapter) SendMedia(ctx context.Context, chatID, filePath, mimeType, sendAs, caption string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)

	errCh := make(chan error, 1)
	go func() {
		var writeErr error
		defer func() {
			writer.Close()
			pw.CloseWithError(writeErr)
			errCh <- writeErr
		}()

		// payload_json field carries the message content (caption).
		payload := map[string]string{}
		if caption != "" {
			payload["content"] = caption
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			writeErr = err
			return
		}
		if writeErr = writer.WriteField("payload_json", string(payloadJSON)); writeErr != nil {
			return
		}

		// File attachment.
		part, err := writer.CreateFormFile("files[0]", filepath.Base(filePath))
		if err != nil {
			writeErr = err
			return
		}
		_, writeErr = io.Copy(part, file)
	}()

	discordURL := fmt.Sprintf("%s/channels/%s/messages", a.apiBase, chatID)
	req, err := http.NewRequest("POST", discordURL, pr)
	if err != nil {
		pr.CloseWithError(err)
		<-errCh
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bot "+a.token)

	resp, err := a.client.Do(req)
	if err != nil {
		pr.CloseWithError(err)
		<-errCh
		return fmt.Errorf("discord sendMedia: %w", err)
	}
	defer resp.Body.Close()

	if writeErr := <-errCh; writeErr != nil {
		return writeErr
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord sendMedia HTTP %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

var _ channel.MediaSender = (*Adapter)(nil)
