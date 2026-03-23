package whatsapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

const cloudAPIBase = "https://graph.facebook.com/v18.0"

// Adapter implements channel.Channel for WhatsApp Business (Cloud API).
type Adapter struct {
	connID        string // Connection ID: "wa_abc123"
	phoneNumberID string
	accessToken   string
	verifyToken   string
	appSecret     string
	listenAddr    string
	server        *http.Server
	msgBus        bus.MessageBus
	client        *http.Client
	cancel        context.CancelFunc
}

// Option configures the WhatsApp adapter.
type Option func(*Adapter)

// WithListenAddr sets the webhook listen address.
func WithListenAddr(addr string) Option {
	return func(a *Adapter) { a.listenAddr = addr }
}

// New creates a new WhatsApp adapter.
func New(connID, phoneNumberID, accessToken, verifyToken string, opts ...Option) *Adapter {
	a := &Adapter{
		connID:        connID,
		phoneNumberID: phoneNumberID,
		accessToken:   accessToken,
		verifyToken:   verifyToken,
		listenAddr:    ":8080",
		client:        &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) Platform() string  { return "whatsapp" }

func (a *Adapter) Start(ctx context.Context, msgBus bus.MessageBus) error {
	a.msgBus = msgBus
	adapterCtx, cancel := context.WithCancel(ctx)
	a.cancel = cancel

	// Subscribe to outbound — only process messages for this connection.
	msgBus.SubscribeOutbound(adapterCtx, func(env bus.Envelope) {
		if env.Channel == a.connID {
			a.sendResponse(env)
		}
	})

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook/whatsapp", a.handleWebhook)
	mux.HandleFunc("GET /webhook/whatsapp", a.handleVerify)

	a.server = &http.Server{Addr: a.listenAddr, Handler: mux}

	go func() {
		log.Printf("whatsapp: webhook listening on %s/webhook/whatsapp (connection %s)", a.listenAddr, a.connID)
		if err := a.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("whatsapp: server error: %v", err)
		}
	}()

	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	if a.server != nil {
		return a.server.Shutdown(ctx)
	}
	return nil
}

func (a *Adapter) handleVerify(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("hub.mode")
	token := r.URL.Query().Get("hub.verify_token")
	challenge := r.URL.Query().Get("hub.challenge")

	if mode == "subscribe" && token == a.verifyToken {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(challenge))
		return
	}
	http.Error(w, "verification failed", http.StatusForbidden)
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if a.appSecret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !a.verifySignature(body, sig) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	var payload WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	for _, entry := range payload.Entry {
		for _, change := range entry.Changes {
			if change.Field != "messages" {
				continue
			}
			for _, msg := range change.Value.Messages {
				if msg.Type == "text" && msg.Text.Body != "" {
					a.msgBus.PublishInbound(r.Context(), bus.Envelope{
						Channel: a.connID,
						ChatID:  msg.From,
						Messages: []canonical.Message{
							{Role: "user", Content: []canonical.Content{{Type: "text", Text: msg.Text.Body}}},
						},
						Metadata: map[string]string{
							"whatsapp_message_id": msg.ID,
							"whatsapp_from":       msg.From,
						},
					})
				}
			}
		}
	}

	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) verifySignature(body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig := strings.TrimPrefix(signature, "sha256=")
	mac := hmac.New(sha256.New, []byte(a.appSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
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
		if text != "" {
			a.sendMessage(env.ChatID, text)
		}
	}
}

func (a *Adapter) sendMessage(to, text string) error {
	payload, _ := json.Marshal(map[string]any{
		"messaging_product": "whatsapp",
		"to":                to,
		"type":              "text",
		"text":              map[string]string{"body": text},
	})

	url := fmt.Sprintf("%s/%s/messages", cloudAPIBase, a.phoneNumberID)
	req, err := http.NewRequest("POST", url, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+a.accessToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("whatsapp API error (%d): %s", resp.StatusCode, body)
	}
	return nil
}

// WhatsApp webhook types.

type WebhookPayload struct {
	Object string         `json:"object"`
	Entry  []WebhookEntry `json:"entry"`
}

type WebhookEntry struct {
	ID      string          `json:"id"`
	Changes []WebhookChange `json:"changes"`
}

type WebhookChange struct {
	Field string       `json:"field"`
	Value ChangeValue  `json:"value"`
}

type ChangeValue struct {
	Messages []WAMessage `json:"messages"`
}

type WAMessage struct {
	ID   string `json:"id"`
	From string `json:"from"`
	Type string `json:"type"`
	Text struct {
		Body string `json:"body"`
	} `json:"text"`
}
