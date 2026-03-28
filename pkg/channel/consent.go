package channel

import "context"

// ConsentPrompt is implemented by channel adapters that can render
// consent prompts natively (e.g. Telegram inline keyboards, Discord buttons).
type ConsentPrompt interface {
	RenderConsent(ctx context.Context, req ConsentPromptRequest) error
}

// ConsentPromptRequest carries all info needed to render a consent prompt.
type ConsentPromptRequest struct {
	ChatID      string
	OwnerUserID string
	Nonce       string
	ToolName    string
	Group       string
	RiskLevel   string
	Explanation string
	IsGroup     bool
	Options     []ConsentOption
}

// ConsentOption represents one button/choice in a consent prompt.
type ConsentOption struct {
	Label string // "Allow once", "Always allow", "Deny"
	Tier  string // "once", "always", "deny"
	Nonce string // Same nonce, different callback data per option.
}

// DefaultConsentOptions returns the standard 3-option consent choices.
func DefaultConsentOptions(nonce string) []ConsentOption {
	return []ConsentOption{
		{Label: "Allow once", Tier: "once", Nonce: nonce},
		{Label: "Always allow", Tier: "always", Nonce: nonce},
		{Label: "Deny", Tier: "deny", Nonce: nonce},
	}
}
