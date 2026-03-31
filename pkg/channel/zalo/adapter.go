package zalo

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
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/channel/typing"
)

const zaloAPIBase = "https://openapi.zalo.me/v3.0/oa/message/cs"

// Adapter implements channel.Channel for Zalo Official Account.
type Adapter struct {
	connID      string // Connection ID: "zl_abc123"
	oaID        string
	secretKey   string
	accessToken string
	msgBus      bus.MessageBus
	client      *http.Client
	cancel      context.CancelFunc
	ownerUserID string             // Platform user ID of the connection owner.
	consentCB   ConsentCallback    // Called when user responds to consent prompt.
	ownerStore  channel.OwnerStore // For auto-capturing owner_user_id.

	// Tool activity: typing + single status message.
	typingMu   sync.Mutex
	typingCtrl map[string]*typing.Controller // sessionID → typing controller
	statusMu   sync.Mutex
	statusSent map[string]bool // sessionID → true if status message already sent
}

// Option configures the Zalo adapter.
type Option func(*Adapter)

// New creates a new Zalo OA adapter.
func New(connID, oaID, secretKey, accessToken string, opts ...Option) *Adapter {
	a := &Adapter{
		connID:      connID,
		oaID:        oaID,
		secretKey:   secretKey,
		accessToken: accessToken,
		client:      &http.Client{Timeout: 30 * time.Second},
		typingCtrl:  make(map[string]*typing.Controller),
		statusSent:  make(map[string]bool),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// NewFromCredentials creates a Zalo adapter from a credential map.
// Expected keys: "oa_id", "secret_key", "access_token".
func NewFromCredentials(connID string, creds map[string]string, opts ...Option) *Adapter {
	return New(connID, creds["oa_id"], creds["secret_key"], creds["access_token"], opts...)
}

func (a *Adapter) ID() string       { return a.connID }
func (a *Adapter) ConnID() string    { return a.connID }
func (a *Adapter) Platform() string  { return "zalo" }

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
		return // Already sent, can't edit.
	}
	a.sendMessage(chatID, update.Text)
}

func (a *Adapter) startTyping(sessionID, chatID string) {
	a.typingMu.Lock()
	if old, ok := a.typingCtrl[sessionID]; ok {
		old.Stop()
	}
	// Zalo OA API doesn't have a public typing indicator endpoint.
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

// RegisterWebhook registers Zalo webhook routes on the shared HTTP mux.
func (a *Adapter) RegisterWebhook(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhook/zalo", a.handleWebhook)
	mux.HandleFunc("GET /webhook/zalo", a.handleVerify)
	log.Printf("zalo: webhook routes registered (connection %s)", a.connID)
}

// UpdateWebhookURL logs the webhook URL for manual Zalo OA configuration.
// Zalo OA webhook registration requires the OA admin portal; programmatic
// registration is not supported via API.
func (a *Adapter) UpdateWebhookURL(ctx context.Context, webhookURL string) error {
	log.Printf("zalo[%s]: webhook URL updated: %s", a.connID, webhookURL)
	log.Printf("zalo[%s]: configure this URL in your Zalo OA admin portal → Webhook settings", a.connID)
	return nil
}

// Start subscribes to the message bus for outbound delivery.
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

	return nil
}

func (a *Adapter) Stop(ctx context.Context) error {
	if a.cancel != nil {
		a.cancel()
	}
	return nil
}

func (a *Adapter) handleVerify(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (a *Adapter) handleWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024*1024))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	if a.secretKey != "" {
		sig := r.Header.Get("X-ZEvent-Signature")
		if !a.verifySignature(body, sig) {
			http.Error(w, "invalid signature", http.StatusForbidden)
			return
		}
	}

	var event WebhookEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "parse error", http.StatusBadRequest)
		return
	}

	if event.EventName == "user_send_text" && event.Message.Text != "" {
		// Auto-capture owner on first inbound message.
		if event.Sender.ID != "" && a.ownerUserID == "" && a.ownerStore != nil {
			channel.CaptureOwner(r.Context(), a.ownerStore, a.connID, a.ownerUserID, event.Sender.ID)
			a.ownerUserID = event.Sender.ID
		}

		// Check for consent text reply.
		if a.HandleConsentText(event.Sender.ID, event.Message.Text) {
			w.WriteHeader(http.StatusOK)
			return
		}

		a.msgBus.PublishInbound(r.Context(), bus.Envelope{
			Channel:   a.connID,
			ChatID:    event.Sender.ID,
			Kind:      "dm", // Zalo OA only supports DM.
			Mentioned: true,
			Messages: []canonical.Message{
				{Role: "user", Content: []canonical.Content{{Type: "text", Text: event.Message.Text}}},
			},
			Metadata: map[string]string{
				"zalo_message_id": event.Message.MsgID,
				"zalo_user_id":    event.Sender.ID,
				"zalo_timestamp":  event.Timestamp,
			},
		})
	}

	w.WriteHeader(http.StatusOK)
}

func (a *Adapter) verifySignature(body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(a.secretKey))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (a *Adapter) sendResponse(env bus.Envelope) {
	defer a.markTypingDispatchIdle(env.SessionID)

	for _, msg := range env.Messages {
		if msg.Role != "assistant" {
			continue
		}
		var textParts []string
		for _, c := range msg.Content {
			if c.Type == "text" && c.Text != "" {
				textParts = append(textParts, c.Text)
			}
		}
		text := strings.Join(textParts, "\n")
		if text == "" {
			continue
		}

		a.sendMessage(env.ChatID, text)
	}
}

func (a *Adapter) sendMessage(userID, text string) error {
	payload := map[string]any{
		"recipient": map[string]string{"user_id": userID},
		"message":   map[string]string{"text": text},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", zaloAPIBase, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("access_token", a.accessToken)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("sending zalo message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("zalo API error: %s", string(respBody))
	}
	return nil
}

// Zalo webhook types.

type WebhookEvent struct {
	AppID     string  `json:"app_id"`
	EventName string  `json:"event_name"`
	Sender    Sender  `json:"sender"`
	Message   Message `json:"message"`
	Timestamp string  `json:"timestamp"`
}

type Sender struct {
	ID string `json:"id"`
}

type Message struct {
	Text string `json:"text"`
	MsgID string `json:"msg_id"`
}
