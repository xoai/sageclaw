package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/xoai/sageclaw/pkg/channel"
)

// ConsentCallback is called when a consent response is received.
type ConsentCallback func(nonce string, granted bool, tier string)

// WithConsentCallback sets the function called when consent is granted/denied.
func WithConsentCallback(cb ConsentCallback) Option {
	return func(a *Adapter) { a.consentCB = cb }
}

// RenderConsent prompts the user for consent via the terminal.
// Blocks until the user responds (y/n/a).
func (a *Adapter) RenderConsent(ctx context.Context, req channel.ConsentPromptRequest) error {
	riskLabel := "⚠️  MODERATE"
	if req.RiskLevel == "sensitive" {
		riskLabel = "🔴 SENSITIVE"
	}

	fmt.Fprintf(a.writer, "\n%s — Permission Request\n", riskLabel)
	fmt.Fprintf(a.writer, "  Tool:  %s\n", req.ToolName)
	fmt.Fprintf(a.writer, "  Group: %s (%s)\n", req.Group, req.RiskLevel)
	fmt.Fprintf(a.writer, "  %s\n\n", req.Explanation)
	fmt.Fprintf(a.writer, "Allow? (y)es once / (a)lways / (n)o: ")

	if !a.reader.Scan() {
		return fmt.Errorf("EOF reading consent response")
	}

	input := strings.TrimSpace(strings.ToLower(a.reader.Text()))

	var granted bool
	var tier string

	switch input {
	case "y", "yes":
		granted = true
		tier = "once"
		fmt.Fprintf(a.writer, "  → Allowed (this session)\n\n")
	case "a", "always":
		granted = true
		tier = "always"
		fmt.Fprintf(a.writer, "  → Always allowed\n\n")
	default:
		granted = false
		tier = "deny"
		fmt.Fprintf(a.writer, "  → Denied\n\n")
	}

	if a.consentCB != nil {
		a.consentCB(req.Nonce, granted, tier)
	}
	return nil
}
