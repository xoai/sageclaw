package zalobot

import (
	"context"
	"fmt"
	"log"
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

// RenderConsent sends a text-based consent prompt.
func (a *Adapter) RenderConsent(ctx context.Context, req channel.ConsentPromptRequest) error {
	riskEmoji := "⚠️"
	if req.RiskLevel == "sensitive" {
		riskEmoji = "🔴"
	}

	text := fmt.Sprintf(
		"%s Permission Request\n\n"+
			"Tool: %s\nGroup: %s (%s)\n\n"+
			"%s\n\n"+
			"Reply with one of:\n"+
			"ALLOW %s\n"+
			"ALWAYS %s\n"+
			"DENY %s",
		riskEmoji, req.ToolName, req.Group, req.RiskLevel,
		req.Explanation,
		req.Nonce, req.Nonce, req.Nonce,
	)

	return a.sendMessage(req.ChatID, text)
}

// ParseConsentReply checks if a message text is a consent response.
// Returns (nonce, granted, tier, matched).
func ParseConsentReply(text string) (string, bool, string, bool) {
	text = strings.TrimSpace(text)
	upper := strings.ToUpper(text)

	prefixes := []struct {
		prefix  string
		granted bool
		tier    string
	}{
		{"ALWAYS ", true, "always"},
		{"ALLOW ", true, "once"},
		{"DENY ", false, "deny"},
	}

	for _, p := range prefixes {
		if strings.HasPrefix(upper, p.prefix) {
			nonce := strings.TrimSpace(text[len(p.prefix):])
			if nonce != "" {
				return nonce, p.granted, p.tier, true
			}
		}
	}
	return "", false, "", false
}

// HandleConsentText checks if an inbound message is a consent response
// and processes it. Returns true if the message was consumed.
func (a *Adapter) HandleConsentText(senderID, text string) bool {
	nonce, granted, tier, matched := ParseConsentReply(text)
	if !matched {
		return false
	}

	// Verify sender is owner.
	if a.ownerUserID != "" && senderID != a.ownerUserID {
		log.Printf("zalobot: consent response from non-owner %s (expected %s)", senderID, a.ownerUserID)
		return true
	}

	if a.consentCB != nil {
		a.consentCB(nonce, granted, tier)
	}
	return true
}
