package rpc

import "sync"

// EventStream manages a global sequenced event ring buffer.
// All sessions share one buffer with a global monotonic seq.
// This avoids Last-Event-ID ambiguity across sessions.
type EventStream struct {
	mu     sync.Mutex
	events []sequencedEvent
	seq    int64
	cap    int
	head   int
	count  int
}

type sequencedEvent struct {
	Seq       int64
	SessionID string
	Type      string
	Data      []byte
}

// NewEventStream creates a ring buffer with the given capacity.
func NewEventStream(capacity int) *EventStream {
	if capacity <= 0 {
		capacity = 1024
	}
	return &EventStream{
		events: make([]sequencedEvent, capacity),
		cap:    capacity,
	}
}

// Append stores an event and returns the assigned global seq.
// Used when the caller already has finalized data.
func (es *EventStream) Append(sessionID, eventType string, data []byte) int64 {
	es.mu.Lock()
	defer es.mu.Unlock()

	es.seq++
	es.store(es.seq, sessionID, eventType, data)
	return es.seq
}

// Reserve atomically reserves the next sequence number.
// Must be followed by Store() to place the event in the buffer.
func (es *EventStream) Reserve() int64 {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.seq++
	return es.seq
}

// Store places an event with a previously reserved seq into the buffer.
func (es *EventStream) Store(seq int64, sessionID, eventType string, data []byte) {
	es.mu.Lock()
	defer es.mu.Unlock()
	es.store(seq, sessionID, eventType, data)
}

func (es *EventStream) store(seq int64, sessionID, eventType string, data []byte) {
	ev := sequencedEvent{
		Seq:       seq,
		SessionID: sessionID,
		Type:      eventType,
		Data:      data,
	}
	es.events[es.head] = ev
	es.head = (es.head + 1) % es.cap
	if es.count < es.cap {
		es.count++
	}
}

// After returns events with seq > afterSeq for catch-up replay.
// Filters out "chunk" events (ephemeral — not worth replaying).
// Returns (nil, false) if afterSeq is older than the buffer's oldest.
func (es *EventStream) After(afterSeq int64) ([]sequencedEvent, bool) {
	es.mu.Lock()
	defer es.mu.Unlock()

	if es.count == 0 {
		return []sequencedEvent{}, true
	}

	// Find the oldest event in the buffer.
	oldest := (es.head - es.count + es.cap) % es.cap
	oldestSeq := es.events[oldest].Seq

	// afterSeq == oldestSeq-1 is valid: the client needs events starting from
	// oldestSeq, which IS in the buffer. Only fail if truly expired.
	if afterSeq > 0 && afterSeq < oldestSeq-1 {
		return nil, false
	}

	var result []sequencedEvent
	for i := 0; i < es.count; i++ {
		idx := (oldest + i) % es.cap
		ev := es.events[idx]
		if ev.Seq <= afterSeq {
			continue
		}
		// Filter out chunk events — they're ephemeral token fragments.
		if ev.Type == "chunk" {
			continue
		}
		result = append(result, ev)
	}
	return result, true
}
