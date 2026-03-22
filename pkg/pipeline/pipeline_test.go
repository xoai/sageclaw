package pipeline

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestDebouncer_BatchesMessages(t *testing.T) {
	var flushed []canonical.Message
	var mu sync.Mutex
	done := make(chan struct{})

	d := NewDebouncer(50*time.Millisecond, func(chatID string, msgs []canonical.Message) {
		mu.Lock()
		flushed = append(flushed, msgs...)
		mu.Unlock()
		close(done)
	})

	// Add 3 messages rapidly.
	d.Add("chat1", canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg1"}}})
	d.Add("chat1", canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg2"}}})
	d.Add("chat1", canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "msg3"}}})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for flush")
	}

	mu.Lock()
	if len(flushed) != 3 {
		t.Fatalf("expected 3 messages in batch, got %d", len(flushed))
	}
	mu.Unlock()
}

func TestDebouncer_MediaBypass(t *testing.T) {
	var flushedCount int
	var mu sync.Mutex
	done := make(chan struct{}, 2)

	d := NewDebouncer(500*time.Millisecond, func(chatID string, msgs []canonical.Message) {
		mu.Lock()
		flushedCount++
		mu.Unlock()
		done <- struct{}{}
	})

	// Add a text message first.
	d.Add("chat1", canonical.Message{Role: "user", Content: []canonical.Content{{Type: "text", Text: "before"}}})

	// Add an image — should flush immediately (including the pending text).
	d.Add("chat1", canonical.Message{Role: "user", Content: []canonical.Content{{Type: "image", Source: &canonical.ImageSource{Type: "base64"}}}})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for media bypass flush")
	}

	mu.Lock()
	if flushedCount < 1 {
		t.Fatal("expected at least 1 flush from media bypass")
	}
	mu.Unlock()
}

func TestClassifyIntent_Commands(t *testing.T) {
	tests := []struct {
		text    string
		intent  IntentType
		command string
	}{
		{"/start", IntentCommand, "start"},
		{"/help", IntentCommand, "help"},
		{"/status", IntentCommand, "status"},
		{"/stop", IntentCommand, "stop"},
		{"hello there", IntentAgent, ""},
		{"how do I deploy this?", IntentAgent, ""},
	}

	for _, tt := range tests {
		msgs := []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: tt.text}}},
		}
		result := ClassifyIntent(msgs)
		if result.Type != tt.intent {
			t.Errorf("text %q: expected %s, got %s", tt.text, tt.intent, result.Type)
		}
		if tt.command != "" && result.Command != tt.command {
			t.Errorf("text %q: expected command %s, got %s", tt.text, tt.command, result.Command)
		}
	}
}

func TestClassifyIntent_EmptyMessages(t *testing.T) {
	result := ClassifyIntent(nil)
	if result.Type != IntentAgent {
		t.Fatalf("expected agent intent for empty messages, got %s", result.Type)
	}
}

func TestLaneScheduler_SerializesPerSession(t *testing.T) {
	var mu sync.Mutex
	var order []string

	scheduler := NewLaneScheduler(DefaultLaneLimits(), func(ctx context.Context, req RunRequest) {
		mu.Lock()
		order = append(order, req.SessionID+"-start")
		mu.Unlock()
		time.Sleep(20 * time.Millisecond) // Simulate work.
		mu.Lock()
		order = append(order, req.SessionID+"-end")
		mu.Unlock()
	})

	ctx := t.Context()

	// Schedule 2 requests for same session — should serialize.
	scheduler.Schedule(ctx, LaneMain, RunRequest{SessionID: "sess1", Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "first"}}}}})
	scheduler.Schedule(ctx, LaneMain, RunRequest{SessionID: "sess1", Messages: []canonical.Message{{Role: "user", Content: []canonical.Content{{Type: "text", Text: "second"}}}}})

	// Wait for completion.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(order) < 4 {
		t.Fatalf("expected 4 events, got %d: %v", len(order), order)
	}

	// First request should complete before second starts.
	if order[0] != "sess1-start" || order[1] != "sess1-end" {
		t.Fatalf("expected serialized execution, got: %v", order)
	}
}
