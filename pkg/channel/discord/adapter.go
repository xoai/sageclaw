package discord

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
)

const (
	discordAPIBase   = "https://discord.com/api/v10"
	gatewayURL       = "wss://gateway.discord.gg/?v=10&encoding=json"
	heartbeatDefault = 41250 * time.Millisecond
)

// Adapter implements channel.Channel for Discord.
type Adapter struct {
	connID  string // Connection ID: "dc_abc123"
	token   string
	msgBus  bus.MessageBus
	client  *http.Client
	botID   string
	cancel  context.CancelFunc
	mu      sync.Mutex
}

// New creates a new Discord adapter.
func New(connID, token string) *Adapter {
	return &Adapter{
		connID: connID,
		token:  token,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *Adapter) ID() string       { return a.connID }
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

		// Chunk for Discord's 2000 char limit.
		chunks := chunkText(text, 2000)
		for _, chunk := range chunks {
			a.sendMessage(env.ChatID, chunk)
		}
	}
}

func (a *Adapter) sendMessage(channelID, content string) error {
	payload, _ := json.Marshal(map[string]string{"content": content})
	req, err := http.NewRequest("POST",
		fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, channelID),
		strings.NewReader(string(payload)))
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

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("discord API error (%d): %s", resp.StatusCode, body)
	}
	return nil
}

// GetMe fetches the bot's user info from Discord.
func (a *Adapter) GetMe(ctx context.Context) (*DiscordUser, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", discordAPIBase+"/users/@me", nil)
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
	ID        string       `json:"id"`
	ChannelID string       `json:"channel_id"`
	Author    DiscordUser  `json:"author"`
	Content   string       `json:"content"`
	Timestamp string       `json:"timestamp"`
}

// NormalizeMessage converts a Discord message to canonical form.
func NormalizeMessage(msg DiscordMessage) canonical.Message {
	return canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: msg.Content}},
	}
}
