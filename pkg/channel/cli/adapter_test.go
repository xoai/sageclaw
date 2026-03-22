package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	localbus "github.com/xoai/sageclaw/pkg/bus/local"
	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestCLI_SendsInboundMessage(t *testing.T) {
	input := strings.NewReader("Hello SageClaw\n")
	output := &bytes.Buffer{}

	adapter := New(WithIO(input, output))
	msgBus := localbus.New()

	var received []bus.Envelope
	done := make(chan struct{}, 1)
	msgBus.SubscribeInbound(context.Background(), func(env bus.Envelope) {
		received = append(received, env)
		select {
		case done <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.Start(ctx, msgBus)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for inbound message")
	}

	if len(received) == 0 {
		t.Fatal("expected inbound message")
	}

	env := received[0]
	if env.Channel != "cli" {
		t.Fatalf("expected cli channel, got %s", env.Channel)
	}
	if env.ChatID != "cli-local" {
		t.Fatalf("expected cli-local chat ID, got %s", env.ChatID)
	}
	if len(env.Messages) == 0 {
		t.Fatal("expected messages")
	}
	text := env.Messages[0].Content[0].Text
	if text != "Hello SageClaw" {
		t.Fatalf("expected 'Hello SageClaw', got %s", text)
	}
}

func TestCLI_ReceivesOutboundResponse(t *testing.T) {
	input := strings.NewReader("") // No input.
	output := &bytes.Buffer{}

	adapter := New(WithIO(input, output))
	msgBus := localbus.New()

	ctx := context.Background()
	adapter.Start(ctx, msgBus)

	// Give the read loop time to start.
	time.Sleep(50 * time.Millisecond)

	// Simulate agent response.
	msgBus.PublishOutbound(ctx, bus.Envelope{
		Channel: "cli",
		ChatID:  chatID,
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hello! I'm SageClaw."}}},
		},
	})

	time.Sleep(100 * time.Millisecond)

	out := output.String()
	if !strings.Contains(out, "sageclaw> Hello! I'm SageClaw.") {
		t.Fatalf("expected response in output, got: %s", out)
	}
}

func TestCLI_MultiLineInput(t *testing.T) {
	input := strings.NewReader("first line\\\nsecond line\n")
	output := &bytes.Buffer{}

	adapter := New(WithIO(input, output))
	msgBus := localbus.New()

	var received []bus.Envelope
	done := make(chan struct{}, 1)
	msgBus.SubscribeInbound(context.Background(), func(env bus.Envelope) {
		received = append(received, env)
		select {
		case done <- struct{}{}:
		default:
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter.Start(ctx, msgBus)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if len(received) == 0 {
		t.Fatal("expected message")
	}
	text := received[0].Messages[0].Content[0].Text
	if !strings.Contains(text, "first line") || !strings.Contains(text, "second line") {
		t.Fatalf("expected multi-line content, got: %s", text)
	}
}
