package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/xoai/sageclaw/pkg/channel"
)

func TestRenderConsent_AllowOnce(t *testing.T) {
	input := strings.NewReader("y\n")
	var output bytes.Buffer
	var gotNonce, gotTier string
	var gotGranted bool

	a := New(
		WithIO(input, &output),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotNonce = nonce
			gotGranted = granted
			gotTier = tier
		}),
	)

	err := a.RenderConsent(context.Background(), channel.ConsentPromptRequest{
		Nonce:       "abc123",
		ToolName:    "shell_exec",
		Group:       "runtime",
		RiskLevel:   "sensitive",
		Explanation: "Can execute commands.",
	})
	if err != nil {
		t.Fatalf("RenderConsent: %v", err)
	}

	if gotNonce != "abc123" {
		t.Errorf("nonce = %q, want abc123", gotNonce)
	}
	if !gotGranted {
		t.Error("expected granted=true")
	}
	if gotTier != "once" {
		t.Errorf("tier = %q, want once", gotTier)
	}

	// Check output contains the prompt.
	out := output.String()
	if !strings.Contains(out, "Permission Request") {
		t.Error("output should contain 'Permission Request'")
	}
	if !strings.Contains(out, "shell_exec") {
		t.Error("output should contain tool name")
	}
}

func TestRenderConsent_Always(t *testing.T) {
	input := strings.NewReader("a\n")
	var output bytes.Buffer
	var gotTier string

	a := New(
		WithIO(input, &output),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotTier = tier
		}),
	)

	a.RenderConsent(context.Background(), channel.ConsentPromptRequest{
		Nonce:     "abc123",
		ToolName:  "web_fetch",
		Group:     "web",
		RiskLevel: "moderate",
	})

	if gotTier != "always" {
		t.Errorf("tier = %q, want always", gotTier)
	}
}

func TestRenderConsent_Deny(t *testing.T) {
	input := strings.NewReader("n\n")
	var output bytes.Buffer
	var gotGranted bool

	a := New(
		WithIO(input, &output),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotGranted = granted
		}),
	)

	a.RenderConsent(context.Background(), channel.ConsentPromptRequest{
		Nonce:     "abc123",
		ToolName:  "shell_exec",
		Group:     "runtime",
		RiskLevel: "sensitive",
	})

	if gotGranted {
		t.Error("expected granted=false for 'n'")
	}
}

func TestRenderConsent_InvalidInputDenies(t *testing.T) {
	input := strings.NewReader("maybe\n")
	var output bytes.Buffer
	var gotTier string

	a := New(
		WithIO(input, &output),
		WithConsentCallback(func(nonce string, granted bool, tier string) {
			gotTier = tier
		}),
	)

	a.RenderConsent(context.Background(), channel.ConsentPromptRequest{
		Nonce:     "abc123",
		ToolName:  "shell_exec",
		Group:     "runtime",
		RiskLevel: "sensitive",
	})

	if gotTier != "deny" {
		t.Errorf("invalid input should deny, got tier %q", gotTier)
	}
}
