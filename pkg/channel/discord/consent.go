package discord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/xoai/sageclaw/pkg/channel"
)

// ConsentCallback is called when a consent response is received.
type ConsentCallback func(nonce string, granted bool, tier string)

// SetOwnerUserID sets the owner user ID for consent verification.
func (a *Adapter) SetOwnerUserID(id string) { a.ownerUserID = id }

// SetConsentCallback sets the function called when consent is granted/denied.
func (a *Adapter) SetConsentCallback(cb ConsentCallback) { a.consentCB = cb }

// SetOwnerStore enables auto-capture of owner_user_id on first inbound message.
func (a *Adapter) SetOwnerStore(s channel.OwnerStore) { a.ownerStore = s }

// RenderConsent sends a consent prompt with Discord button components.
func (a *Adapter) RenderConsent(ctx context.Context, req channel.ConsentPromptRequest) error {
	riskEmoji := "⚠️"
	if req.RiskLevel == "sensitive" {
		riskEmoji = "🔴"
	}

	text := fmt.Sprintf(
		"%s **Permission Request**\n\nTool: `%s`\nGroup: **%s** (%s)\n\n%s",
		riskEmoji, req.ToolName, req.Group, req.RiskLevel, req.Explanation,
	)

	// Build button components.
	buttons := make([]actionRowButton, 0, len(req.Options))
	for _, opt := range req.Options {
		style := buttonStylePrimary
		if opt.Tier == "deny" {
			style = buttonStyleDanger
		} else if opt.Tier == "always" {
			style = buttonStyleSuccess
		}
		buttons = append(buttons, actionRowButton{
			Type:     2, // Button
			Style:    style,
			Label:    opt.Label,
			CustomID: fmt.Sprintf("consent:%s:%s", opt.Nonce, opt.Tier),
		})
	}

	payload := discordMessagePayload{
		Content: text,
		Components: []actionRow{
			{Type: 1, Components: buttons},
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshaling consent message: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/channels/%s/messages", discordAPIBase, req.ChatID),
		bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Authorization", "Bot "+a.token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("discord consent send: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("discord consent send HTTP %d", resp.StatusCode)
	}
	return nil
}

// HandleInteraction processes a Discord interaction (button click).
// Called by Gateway event handler or interaction endpoint.
func (a *Adapter) HandleInteraction(ctx context.Context, interaction Interaction) {
	if interaction.Type != interactionTypeComponent {
		return
	}
	if interaction.Data == nil || len(interaction.Data.CustomID) < 10 {
		return
	}

	// Parse "consent:{nonce}:{tier}"
	customID := interaction.Data.CustomID
	if len(customID) < 8 || customID[:8] != "consent:" {
		return
	}

	parts := splitN(customID[8:], ":", 2)
	if len(parts) != 2 {
		return
	}
	nonce, tier := parts[0], parts[1]

	// Verify sender is owner.
	senderID := ""
	if interaction.User != nil {
		senderID = interaction.User.ID
	} else if interaction.Member != nil && interaction.Member.User != nil {
		senderID = interaction.Member.User.ID
	}

	if a.ownerUserID != "" && senderID != a.ownerUserID {
		a.respondInteraction(ctx, interaction.ID, interaction.Token, "Only the bot owner can respond to consent requests.", true)
		return
	}

	granted := tier != "deny"

	if a.consentCB != nil {
		a.consentCB(nonce, granted, tier)
	}

	resultText := "✅ Allowed"
	if tier == "always" {
		resultText = "✅ Always allowed"
	} else if tier == "deny" {
		resultText = "❌ Denied"
	}
	a.respondInteraction(ctx, interaction.ID, interaction.Token, resultText, false)
}

// respondInteraction sends an interaction response to Discord.
func (a *Adapter) respondInteraction(ctx context.Context, interactionID, token, content string, ephemeral bool) {
	flags := 0
	if ephemeral {
		flags = 64 // EPHEMERAL
	}

	payload := interactionResponse{
		Type: 4, // CHANNEL_MESSAGE_WITH_SOURCE
		Data: interactionCallbackData{
			Content: content,
			Flags:   flags,
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "POST",
		fmt.Sprintf("%s/interactions/%s/%s/callback", discordAPIBase, interactionID, token),
		bytes.NewReader(body))
	if err != nil {
		log.Printf("discord: interaction response error: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// Interaction callbacks don't need Auth header (token in URL).

	resp, err := a.client.Do(req)
	if err != nil {
		log.Printf("discord: interaction response failed: %v", err)
		return
	}
	resp.Body.Close()
}

// splitN splits a string into at most n parts. Simple helper to avoid importing strings.
func splitN(s, sep string, n int) []string {
	var result []string
	for i := 0; i < n-1; i++ {
		idx := -1
		for j := 0; j < len(s); j++ {
			if j+len(sep) <= len(s) && s[j:j+len(sep)] == sep {
				idx = j
				break
			}
		}
		if idx < 0 {
			break
		}
		result = append(result, s[:idx])
		s = s[idx+len(sep):]
	}
	result = append(result, s)
	return result
}

// Discord component types.

const (
	buttonStylePrimary = 1
	buttonStyleSuccess = 3
	buttonStyleDanger  = 4
)

const (
	interactionTypeComponent = 3 // MESSAGE_COMPONENT
)

type discordMessagePayload struct {
	Content    string      `json:"content"`
	Components []actionRow `json:"components,omitempty"`
}

type actionRow struct {
	Type       int              `json:"type"` // 1 = ACTION_ROW
	Components []actionRowButton `json:"components"`
}

type actionRowButton struct {
	Type     int    `json:"type"`      // 2 = BUTTON
	Style    int    `json:"style"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id"`
}

// Interaction represents a Discord interaction event.
type Interaction struct {
	ID    string           `json:"id"`
	Type  int              `json:"type"`
	Token string           `json:"token"`
	Data  *InteractionData `json:"data"`
	User  *DiscordUser     `json:"user"`   // For DMs.
	Member *GuildMember    `json:"member"` // For guild channels.
}

type InteractionData struct {
	CustomID string `json:"custom_id"`
}

type GuildMember struct {
	User *DiscordUser `json:"user"`
}

type interactionResponse struct {
	Type int                     `json:"type"`
	Data interactionCallbackData `json:"data"`
}

type interactionCallbackData struct {
	Content string `json:"content"`
	Flags   int    `json:"flags,omitempty"`
}
