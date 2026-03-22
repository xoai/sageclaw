package middleware

import (
	"context"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestPreResponseLog_PassesThrough(t *testing.T) {
	mw := PreResponseLog()
	data := &HookData{
		HookPoint: HookPreResponse,
		Response: &canonical.Response{
			Usage: canonical.Usage{InputTokens: 100, OutputTokens: 50},
		},
	}

	called := false
	err := mw(context.Background(), data, func(ctx context.Context, data *HookData) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected next to be called")
	}
}

func TestPreResponse_ModifyResponse(t *testing.T) {
	// A middleware that appends a disclaimer.
	disclaimer := func(ctx context.Context, data *HookData, next NextFunc) error {
		if data.HookPoint != HookPreResponse {
			return next(ctx, data)
		}
		if data.Response != nil && len(data.Response.Messages) > 0 {
			msg := &data.Response.Messages[0]
			for i, c := range msg.Content {
				if c.Type == "text" {
					msg.Content[i].Text += "\n\n_[AI-generated response]_"
				}
			}
		}
		return next(ctx, data)
	}

	data := &HookData{
		HookPoint: HookPreResponse,
		Response: &canonical.Response{
			Messages: []canonical.Message{{
				Role:    "assistant",
				Content: []canonical.Content{{Type: "text", Text: "Hello world"}},
			}},
		},
	}

	disclaimer(context.Background(), data, func(ctx context.Context, data *HookData) error { return nil })

	text := data.Response.Messages[0].Content[0].Text
	if text != "Hello world\n\n_[AI-generated response]_" {
		t.Fatalf("expected modified text, got: %s", text)
	}
}
