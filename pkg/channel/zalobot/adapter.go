package zalobot

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	zaloBotAPI    = "https://bot-api.zaloplatforms.com/bot"
	pollTimeout   = 30 // Long polling timeout in seconds.
	maxMessageLen = 2000
)

// Adapter implements the Channel interface for Zalo Bot.
type Adapter struct {
	connID      string
	token       string
	client      *http.Client
	msgBus      bus.MessageBus
	cancel      context.CancelFunc
	baseURL     string // For testing.
	botID       string
	botName     string
	ownerUserID string             // Platform user ID of the connection owner.
	consentCB   ConsentCallback    // Called when user responds to consent prompt.
	ownerStore  channel.OwnerStore // For auto-capturing owner_user_id.

	// Tool activity: typing + single status message.
	typingMu   sync.Mutex
	typingCtrl map[string]*typing.Controller
	statusMu   sync.Mutex
	statusSent map[string]bool
}

// New creates a new Zalo Bot adapter.
func New(connID, token string, opts ...Option) *Adapter {
	a := &Adapter{
		connID:     connID,
		token:      token,
		client:     &http.Client{Timeout: time.Duration(pollTimeout+10) * time.Second},
		baseURL:    zaloBotAPI + token,
		typingCtrl: make(map[string]*typing.Controller),
		statusSent: make(map[string]bool),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Option configures the adapter.
type Option func(*Adapter)

// WithBaseURL overrides the API URL (for testing).
func WithBaseURL(url string) Option {
	return func(a *Adapter) { a.baseURL = url }
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) ConnID() string    { return a.connID }
func (a *Adapter) Platform() string  { return "zalo_bot" }

// OnAgentEvent handles agent lifecycle events for typing indicators.
func (a *Adapter) OnAgentEvent(sessionID, chatID, eventType, text string) {
	switch eventType {
	case "run.started":
		a.startTyping(sessionID, chatID)
	case "run.completed", "run.failed":
		a.markTypingRunComplete(sessionID)
		a.statusMu.Lock()
		delete(a.statusSent, sessionID)
		a.statusMu.Unlock()
	}
}

// OnToolStatus handles tool status updates — sends a single status message.
func (a *Adapter) OnToolStatus(sessionID, chatID string, update toolstatus.StatusUpdate) {
	if update.Done || update.Text == "" {
		return
	}
	a.statusMu.Lock()
	sent := a.statusSent[sessionID]
	if !sent {
		a.statusSent[sessionID] = true
	}
	a.statusMu.Unlock()
	if sent {
		return
	}
	a.sendMessage(chatID, update.Text)
}

func (a *Adapter) startTyping(sessionID, chatID string) {
	a.typingMu.Lock()
	if old, ok := a.typingCtrl[sessionID]; ok {
		old.Stop()
	}
	ctrl := typing.NewController(
		func() error { return nil },
		nil, 5000, 60000,
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

// UpdateWebhookURL logs the webhook URL. Zalobot uses long polling rather
// than webhooks, but the URL is logged for informational purposes.
func (a *Adapter) UpdateWebhookURL(ctx context.Context, webhookURL string) error {
	log.Printf("zalo_bot[%s]: webhook URL available: %s (zalobot uses long polling, no registration needed)", a.connID, webhookURL)
	return nil
}

// Start begins long polling for updates.
func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus

	botUser, err := a.GetMe(ctx)
	if err != nil {
		log.Printf("zalo_bot: warning: could not get bot info: %v", err)
	} else {
		a.botID = botUser.ID
		a.botName = botUser.AccountName
		log.Printf("zalo_bot: connected as %s (connection %s)", a.botName, a.connID)
	}

	adapterCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

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

// GetMe calls the Zalo Bot getMe API.
func (a *Adapter) GetMe(ctx context.Context) (*BotUser, error) {
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

	var result APIResponse[*BotUser]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing getMe response: %w", err)
	}
	if !result.OK || result.Result == nil {
		return nil, fmt.Errorf("getMe failed: %s", string(body))
	}
	return result.Result, nil
}

func (a *Adapter) pollLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := a.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("zalo_bot poll error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		for _, update := range updates {
			a.handleUpdate(ctx, update)
		}
	}
}

func (a *Adapter) getUpdates(ctx context.Context) ([]Update, error) {
	reqURL := fmt.Sprintf("%s/getUpdates?timeout=%d", a.baseURL, pollTimeout)

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

	var result APIResponse[Update]
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if !result.OK {
		// 408 is the normal long-poll timeout — no messages available.
		if result.ErrorCode == 408 {
			return nil, nil
		}
		return nil, fmt.Errorf("zalo_bot API error: %s", string(body))
	}

	// API returns a single update, not an array.
	if result.Result.Message.MessageID == "" {
		return nil, nil
	}
	return []Update{result.Result}, nil
}

func (a *Adapter) handleUpdate(ctx context.Context, update Update) {
	msg := update.Message
	if msg.MessageID == "" {
		return
	}

	// Auto-capture owner on first inbound message.
	if msg.From.ID != "" && a.ownerUserID == "" && a.ownerStore != nil {
		channel.CaptureOwner(ctx, a.ownerStore, a.connID, a.ownerUserID, msg.From.ID)
		a.ownerUserID = msg.From.ID
	}

	// Check for consent text reply (text messages only).
	if update.EventName == "message.text.received" && msg.Text != "" {
		if a.HandleConsentText(msg.From.ID, msg.Text) {
			return
		}
	}

	canonicalMsg := normalizeMessage(update.EventName, &msg)

	kind := "dm"
	if msg.Chat.ChatType == "GROUP" {
		kind = "group"
	}

	// DMs are always "mentioned". Group mention detection not yet supported.
	mentioned := kind == "dm"

	a.msgBus.PublishInbound(ctx, bus.Envelope{
		Channel:   a.connID,
		ChatID:    msg.Chat.ID,
		Kind:      kind,
		Mentioned: mentioned,
		Messages:  []canonical.Message{canonicalMsg},
		Metadata: map[string]string{
			"zalobot_message_id": msg.MessageID,
			"zalobot_user_id":   msg.From.ID,
			"zalobot_user_name": msg.From.DisplayName,
		},
	})
}

func (a *Adapter) sendResponse(env bus.Envelope) {
	defer a.markTypingDispatchIdle(env.SessionID)

	for _, msg := range env.Messages {
		text := extractText(msg)
		if text == "" {
			continue
		}

		chunks := chunkText(text, maxMessageLen)
		for _, chunk := range chunks {
			if err := a.sendMessage(env.ChatID, chunk); err != nil {
				log.Printf("zalo_bot: send error: %v", err)
			}
		}
	}
}

func (a *Adapter) sendMessage(chatID, text string) error {
	payload, _ := json.Marshal(map[string]string{
		"chat_id": chatID,
		"text":    text,
	})

	req, err := http.NewRequest("POST", a.baseURL+"/sendMessage", strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zalo_bot API error (%d): %s", resp.StatusCode, string(body))
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
		breakAt := maxLen
		if idx := strings.LastIndex(text[:maxLen], "\n"); idx > maxLen/2 {
			breakAt = idx + 1
		}
		chunks = append(chunks, text[:breakAt])
		text = text[breakAt:]
	}
	return chunks
}

// normalizeMessage converts a Zalo Bot message to canonical form.
func normalizeMessage(eventName string, msg *ZBMessage) canonical.Message {
	var content []canonical.Content

	switch eventName {
	case "message.text.received":
		if msg.Text != "" {
			content = append(content, canonical.Content{Type: "text", Text: msg.Text})
		}
	case "message.image.received":
		if msg.Caption != "" {
			content = append(content, canonical.Content{Type: "text", Text: msg.Caption})
		}
		content = append(content, canonical.Content{Type: "text", Text: "[Image attached]"})
	case "message.sticker.received":
		content = append(content, canonical.Content{Type: "text", Text: "[Sticker]"})
	default:
		if msg.Text != "" {
			content = append(content, canonical.Content{Type: "text", Text: msg.Text})
		}
	}

	if len(content) == 0 {
		content = append(content, canonical.Content{Type: "text", Text: "(empty message)"})
	}

	return canonical.Message{Role: "user", Content: content}
}
