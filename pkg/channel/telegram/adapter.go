package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	telegramAPI    = "https://api.telegram.org/bot"
	pollTimeout    = 30 // Long polling timeout in seconds.
	maxMessageLen  = 4096
)

// Adapter implements the Channel interface for Telegram.
type Adapter struct {
	token    string
	client   *http.Client
	msgBus   bus.MessageBus
	cancel   context.CancelFunc
	baseURL  string // For testing.
}

// New creates a new Telegram adapter.
func New(token string, opts ...Option) *Adapter {
	a := &Adapter{
		token:   token,
		client:  &http.Client{Timeout: time.Duration(pollTimeout+10) * time.Second},
		baseURL: telegramAPI + token,
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

func (a *Adapter) Name() string { return "telegram" }

// Start begins long polling for updates.
func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus
	adapterCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to outbound messages for delivery.
	// Uses adapterCtx so Stop() kills both polling AND the subscription.
	msgBus.SubscribeOutbound(adapterCtx, func(env bus.Envelope) {
		a.sendResponse(env)
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
			if update.Message != nil {
				a.handleMessage(ctx, update.Message)
			}
			offset = update.UpdateID + 1
		}
	}
}

func (a *Adapter) getUpdates(ctx context.Context, offset int) ([]Update, error) {
	reqURL := fmt.Sprintf("%s/getUpdates?timeout=%d&offset=%d",
		a.baseURL, pollTimeout, offset)

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

func (a *Adapter) handleMessage(ctx context.Context, msg *TelegramMessage) {
	canonicalMsg := normalizeMessage(msg)

	a.msgBus.PublishInbound(ctx, bus.Envelope{
		Channel:  "telegram",
		ChatID:   strconv.FormatInt(msg.Chat.ID, 10),
		Messages: []canonical.Message{canonicalMsg},
		Metadata: map[string]string{
			"telegram_message_id": strconv.Itoa(msg.MessageID),
			"telegram_user_id":    strconv.FormatInt(msg.From.ID, 10),
		},
	})
}

func (a *Adapter) sendResponse(env bus.Envelope) {
	for _, msg := range env.Messages {
		text := extractText(msg)
		if text == "" {
			continue
		}

		// Chunk long messages (Telegram 4096 char limit).
		chunks := chunkText(text, maxMessageLen)
		for _, chunk := range chunks {
			a.sendMessage(env.ChatID, chunk)
		}
	}
}

func (a *Adapter) sendMessage(chatID, text string) error {
	params := url.Values{
		"chat_id":    {chatID},
		"text":       {text},
		"parse_mode": {"Markdown"},
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

// normalizeMessage converts a Telegram message to canonical form.
func normalizeMessage(msg *TelegramMessage) canonical.Message {
	var content []canonical.Content

	if msg.Text != "" {
		content = append(content, canonical.Content{Type: "text", Text: msg.Text})
	}

	if msg.Caption != "" && msg.Text == "" {
		content = append(content, canonical.Content{Type: "text", Text: msg.Caption})
	}

	// Handle photos (take the largest).
	if len(msg.Photo) > 0 {
		// Photos are sorted by size, last is largest.
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

// Telegram API types.

type Update struct {
	UpdateID int              `json:"update_id"`
	Message  *TelegramMessage `json:"message"`
}

type TelegramMessage struct {
	MessageID int           `json:"message_id"`
	From      *TelegramUser `json:"from"`
	Chat      TelegramChat  `json:"chat"`
	Text      string        `json:"text"`
	Caption   string        `json:"caption"`
	Photo     []PhotoSize   `json:"photo"`
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
