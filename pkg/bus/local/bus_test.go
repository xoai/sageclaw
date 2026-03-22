package local

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestBus_PublishSubscribeInbound(t *testing.T) {
	b := New()
	ctx := context.Background()

	var received []bus.Envelope
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	b.SubscribeInbound(ctx, func(env bus.Envelope) {
		mu.Lock()
		received = append(received, env)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	b.PublishInbound(ctx, bus.Envelope{
		Channel: "test",
		ChatID:  "123",
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: "hello"}}},
		},
	})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 message, got %d", len(received))
	}
	if received[0].ChatID != "123" {
		t.Fatalf("expected chatID 123, got %s", received[0].ChatID)
	}
}

func TestBus_PublishSubscribeOutbound(t *testing.T) {
	b := New()
	ctx := context.Background()

	var received []bus.Envelope
	var mu sync.Mutex
	done := make(chan struct{}, 1)

	b.SubscribeOutbound(ctx, func(env bus.Envelope) {
		mu.Lock()
		received = append(received, env)
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	b.PublishOutbound(ctx, bus.Envelope{ChatID: "456"})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || received[0].ChatID != "456" {
		t.Fatalf("unexpected result: %v", received)
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	b := New()
	ctx := context.Background()

	var count1, count2 int
	var mu sync.Mutex
	done := make(chan struct{}, 2)

	b.SubscribeInbound(ctx, func(env bus.Envelope) {
		mu.Lock()
		count1++
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})
	b.SubscribeInbound(ctx, func(env bus.Envelope) {
		mu.Lock()
		count2++
		mu.Unlock()
		select {
		case done <- struct{}{}:
		default:
		}
	})

	b.PublishInbound(ctx, bus.Envelope{ChatID: "multi"})

	// Wait for both.
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for subscriber", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if count1 != 1 || count2 != 1 {
		t.Fatalf("expected both subscribers called once, got %d and %d", count1, count2)
	}
}
