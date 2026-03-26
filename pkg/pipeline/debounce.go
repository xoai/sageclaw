package pipeline

import (
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const defaultDebounceWindow = 1000 * time.Millisecond

// Debouncer batches messages within a time window.
type Debouncer struct {
	window  time.Duration
	pending map[string]*pendingBatch
	mu      sync.Mutex
	flush   func(chatID string, msgs []canonical.Message)
}

type pendingBatch struct {
	messages []canonical.Message
	timer    *time.Timer
}

// NewDebouncer creates a new debouncer.
func NewDebouncer(window time.Duration, flush func(chatID string, msgs []canonical.Message)) *Debouncer {
	if window == 0 {
		window = defaultDebounceWindow
	}
	return &Debouncer{
		window:  window,
		pending: make(map[string]*pendingBatch),
		flush:   flush,
	}
}

// Add adds a message for a chat. If it contains an image or audio, flush immediately.
func (d *Debouncer) Add(chatID string, msg canonical.Message) {
	// Media bypass: images and audio skip debounce.
	if hasImage(msg) || canonical.HasAudio(msg) {
		d.flushImmediate(chatID, msg)
		return
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	batch, exists := d.pending[chatID]
	if !exists {
		batch = &pendingBatch{}
		d.pending[chatID] = batch
	}

	batch.messages = append(batch.messages, msg)

	// Reset or create timer.
	if batch.timer != nil {
		batch.timer.Stop()
	}
	batch.timer = time.AfterFunc(d.window, func() {
		d.flushBatch(chatID)
	})
}

func (d *Debouncer) flushImmediate(chatID string, msg canonical.Message) {
	d.mu.Lock()
	// Flush any pending messages first.
	batch, exists := d.pending[chatID]
	if exists {
		if batch.timer != nil {
			batch.timer.Stop()
		}
		msgs := append(batch.messages, msg)
		delete(d.pending, chatID)
		d.mu.Unlock()
		d.flush(chatID, msgs)
		return
	}
	d.mu.Unlock()
	d.flush(chatID, []canonical.Message{msg})
}

func (d *Debouncer) flushBatch(chatID string) {
	d.mu.Lock()
	batch, exists := d.pending[chatID]
	if !exists {
		d.mu.Unlock()
		return
	}
	msgs := batch.messages
	delete(d.pending, chatID)
	d.mu.Unlock()

	if len(msgs) > 0 {
		d.flush(chatID, msgs)
	}
}

func hasImage(msg canonical.Message) bool {
	for _, c := range msg.Content {
		if c.Type == "image" || c.Source != nil {
			return true
		}
	}
	return false
}
