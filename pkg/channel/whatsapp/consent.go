package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/xoai/sageclaw/pkg/channel"
)

// ConsentCallback is called when a consent response is received.
type ConsentCallback func(nonce string, granted bool, tier string)

// WithOwnerUserID sets the owner user ID for consent verification.
func WithOwnerUserID(id string) Option {
	return func(a *Adapter) { a.ownerUserID = id }
}

// WithConsentCallback sets the function called when consent is granted/denied.
func WithConsentCallback(cb ConsentCallback) Option {
	return func(a *Adapter) { a.consentCB = cb }
}

// WithOwnerStore enables auto-capture of owner_user_id on first inbound message.
func WithOwnerStore(s channel.OwnerStore) Option {
	return func(a *Adapter) { a.ownerStore = s }
}

// RenderConsent sends a consent prompt with WhatsApp interactive buttons.
func (a *Adapter) RenderConsent(ctx context.Context, req channel.ConsentPromptRequest) error {
	riskEmoji := "⚠️"
	if req.RiskLevel == "sensitive" {
		riskEmoji = "🔴"
	}

	bodyText := fmt.Sprintf(
		"%s Permission Request\n\nTool: %s\nGroup: %s (%s)\n\n%s",
		riskEmoji, req.ToolName, req.Group, req.RiskLevel, req.Explanation,
	)

	// WhatsApp interactive buttons (max 3).
	buttons := make([]waButton, 0, len(req.Options))
	for _, opt := range req.Options {
		buttons = append(buttons, waButton{
			Type: "reply",
			Reply: waButtonReply{
				ID:    fmt.Sprintf("consent:%s:%s", opt.Nonce, opt.Tier),
				Title: opt.Label,
			},
		})
	}

	payload := map[string]any{
		"messaging_product": "whatsapp",
		"to":                req.ChatID,
		"type":              "interactive",
		"interactive": map[string]any{
			"type": "button",
			"body": map[string]string{"text": bodyText},
			"action": map[string]any{
				"buttons": buttons,
			},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling consent: %w", err)
	}

	url := fmt.Sprintf("%s/%s/messages", cloudAPIBase, a.phoneNumberID)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bearer "+a.accessToken)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("whatsapp consent send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("whatsapp consent send HTTP %d", resp.StatusCode)
	}
	return nil
}

// HandleConsentReply checks if an inbound message is a consent button reply
// and processes it. Returns true if the message was a consent response.
func (a *Adapter) HandleConsentReply(msg WAMessage) bool {
	if msg.Interactive == nil || msg.Interactive.ButtonReply == nil {
		return false
	}

	id := msg.Interactive.ButtonReply.ID
	if !strings.HasPrefix(id, "consent:") {
		return false
	}

	parts := strings.SplitN(id[8:], ":", 2)
	if len(parts) != 2 {
		return false
	}
	nonce, tier := parts[0], parts[1]

	// Verify sender is owner.
	if a.ownerUserID != "" && msg.From != a.ownerUserID {
		log.Printf("whatsapp: consent response from non-owner %s (expected %s)", msg.From, a.ownerUserID)
		return true // Consumed but not actioned.
	}

	granted := tier != "deny"
	if a.consentCB != nil {
		a.consentCB(nonce, granted, tier)
	}
	return true
}

// WhatsApp interactive button types.

type waButton struct {
	Type  string        `json:"type"`
	Reply waButtonReply `json:"reply"`
}

type waButtonReply struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
