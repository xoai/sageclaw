package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"

	"github.com/xoai/sageclaw/pkg/channel"
)

// consentCallback is a function called when a consent response is received.
// Set via WithConsentCallback option.
type consentCallback func(nonce string, granted bool, tier string)

// RenderConsent sends a consent prompt as an inline keyboard message.
func (a *Adapter) RenderConsent(ctx context.Context, req channel.ConsentPromptRequest) error {
	// Build inline keyboard with 3 buttons.
	buttons := make([][]inlineKeyboardButton, 0, len(req.Options))
	for _, opt := range req.Options {
		callbackData := fmt.Sprintf("consent:%s:%s", opt.Nonce, opt.Tier)
		buttons = append(buttons, []inlineKeyboardButton{
			{Text: opt.Label, CallbackData: callbackData},
		})
	}

	keyboard, err := json.Marshal(inlineKeyboardMarkup{InlineKeyboard: buttons})
	if err != nil {
		return fmt.Errorf("marshaling keyboard: %w", err)
	}

	// Build consent message text.
	riskEmoji := "⚠️"
	if req.RiskLevel == "sensitive" {
		riskEmoji = "🔴"
	}

	text := fmt.Sprintf(
		"%s *Permission Request*\n\n"+
			"Tool: `%s`\n"+
			"Group: *%s* \\(%s\\)\n\n"+
			"%s",
		riskEmoji,
		escapeMarkdownV2(req.ToolName),
		escapeMarkdownV2(req.Group),
		escapeMarkdownV2(req.RiskLevel),
		escapeMarkdownV2(req.Explanation),
	)

	params := url.Values{
		"chat_id":      {req.ChatID},
		"text":         {text},
		"parse_mode":   {"MarkdownV2"},
		"reply_markup": {string(keyboard)},
	}

	_, err = a.apiPost(ctx, "/sendMessage", params)
	if err != nil {
		// Fallback: try without MarkdownV2.
		params.Set("text", fmt.Sprintf(
			"%s Permission Request\n\nTool: %s\nGroup: %s (%s)\n\n%s",
			riskEmoji, req.ToolName, req.Group, req.RiskLevel, req.Explanation,
		))
		params.Del("parse_mode")
		_, err = a.apiPost(ctx, "/sendMessage", params)
	}
	return err
}

// handleCallbackQuery processes an inline keyboard callback.
func (a *Adapter) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) {
	if cq == nil || cq.Data == "" {
		return
	}

	// Parse consent callback: "consent:{nonce}:{tier}"
	if !strings.HasPrefix(cq.Data, "consent:") {
		return
	}

	parts := strings.SplitN(cq.Data, ":", 3)
	if len(parts) != 3 {
		return
	}
	nonce, tier := parts[1], parts[2]

	// Verify sender is the connection owner.
	senderID := strconv.FormatInt(cq.From.ID, 10)
	if a.ownerUserID != "" && senderID != a.ownerUserID {
		a.answerCallbackQuery(ctx, cq.ID, "Only the bot owner can respond to consent requests.")
		return
	}

	// Determine grant/deny.
	granted := tier != "deny"

	// Call consent callback.
	if a.consentCB != nil {
		a.consentCB(nonce, granted, tier)
	}

	// Answer the callback query to dismiss the loading indicator.
	resultText := "Allowed"
	if tier == "always" {
		resultText = "Always allowed"
	} else if tier == "deny" {
		resultText = "Denied"
	}
	a.answerCallbackQuery(ctx, cq.ID, resultText)

	// Edit the original message to show the result (remove keyboard).
	if cq.Message != nil {
		a.editMessageRemoveKeyboard(ctx, cq.Message.Chat.ID, cq.Message.MessageID, resultText)
	}
}

// answerCallbackQuery acknowledges an inline keyboard callback.
func (a *Adapter) answerCallbackQuery(ctx context.Context, queryID, text string) {
	params := url.Values{
		"callback_query_id": {queryID},
		"text":              {text},
	}
	if _, err := a.apiPost(ctx, "/answerCallbackQuery", params); err != nil {
		log.Printf("telegram: answerCallbackQuery failed: %v", err)
	}
}

// editMessageRemoveKeyboard edits a message to remove its inline keyboard.
func (a *Adapter) editMessageRemoveKeyboard(ctx context.Context, chatID int64, messageID int, newText string) {
	empty, _ := json.Marshal(inlineKeyboardMarkup{InlineKeyboard: [][]inlineKeyboardButton{}})
	params := url.Values{
		"chat_id":      {strconv.FormatInt(chatID, 10)},
		"message_id":   {strconv.Itoa(messageID)},
		"text":         {newText},
		"reply_markup": {string(empty)},
	}
	if _, err := a.apiPost(ctx, "/editMessageText", params); err != nil {
		log.Printf("telegram: editMessageText failed: %v", err)
	}
}

// apiPost sends a POST request to the Telegram API and returns the response body.
func (a *Adapter) apiPost(ctx context.Context, method string, params url.Values) ([]byte, error) {
	resp, err := a.client.PostForm(a.baseURL+method, params)
	if err != nil {
		return nil, fmt.Errorf("telegram API %s: %w", method, err)
	}
	defer resp.Body.Close()

	body := make([]byte, 0, 1024)
	buf := make([]byte, 1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			body = append(body, buf[:n]...)
		}
		if readErr != nil {
			break
		}
	}

	if resp.StatusCode != 200 {
		return body, fmt.Errorf("telegram API %s HTTP %d: %s", method, resp.StatusCode, string(body))
	}
	return body, nil
}

// Telegram inline keyboard types.

type inlineKeyboardMarkup struct {
	InlineKeyboard [][]inlineKeyboardButton `json:"inline_keyboard"`
}

type inlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data"`
}

// CallbackQuery represents a Telegram callback query from an inline keyboard.
type CallbackQuery struct {
	ID      string           `json:"id"`
	From    TelegramUser     `json:"from"`
	Message *TelegramMessage `json:"message"`
	Data    string           `json:"data"`
}
