package rpc

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestEventStream_AppendIncrementsSeq(t *testing.T) {
	es := NewEventStream(1024)

	s1 := es.Append("sess1", "chunk", []byte(`{"text":"a"}`))
	s2 := es.Append("sess1", "run.completed", []byte(`{"done":true}`))
	s3 := es.Append("sess2", "chunk", []byte(`{"text":"b"}`))

	if s1 != 1 || s2 != 2 || s3 != 3 {
		t.Fatalf("expected seq 1,2,3 got %d,%d,%d", s1, s2, s3)
	}
}

func TestEventStream_AfterReturnsEventsAfterSeq(t *testing.T) {
	es := NewEventStream(1024)
	es.Append("s1", "run.started", []byte(`{}`))
	es.Append("s1", "chunk", []byte(`{"text":"hi"}`))
	es.Append("s1", "run.completed", []byte(`{"done":true}`))

	events, ok := es.After(1)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// After(1) should return events with seq > 1, but chunk is filtered.
	// seq=2 is chunk (filtered), seq=3 is run.completed (kept).
	if len(events) != 1 {
		t.Fatalf("expected 1 event (chunk filtered), got %d", len(events))
	}
	if events[0].Seq != 3 {
		t.Fatalf("expected seq=3, got %d", events[0].Seq)
	}
}

func TestEventStream_AfterFiltersChunkEvents(t *testing.T) {
	es := NewEventStream(1024)
	es.Append("s1", "run.started", []byte(`{}`))
	es.Append("s1", "chunk", []byte(`{"text":"a"}`))
	es.Append("s1", "chunk", []byte(`{"text":"b"}`))
	es.Append("s1", "chunk", []byte(`{"text":"c"}`))
	es.Append("s1", "run.completed", []byte(`{}`))

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	// 5 events total, 3 chunks filtered → 2 remaining.
	if len(events) != 2 {
		t.Fatalf("expected 2 events (3 chunks filtered), got %d", len(events))
	}
	for _, e := range events {
		if e.Type == "chunk" {
			t.Fatal("chunk event should have been filtered")
		}
	}
}

func TestEventStream_AfterReturnsFalseWhenTooOld(t *testing.T) {
	es := NewEventStream(4) // tiny buffer
	// Fill buffer past capacity.
	for i := 0; i < 10; i++ {
		es.Append("s1", "run.started", []byte(`{}`))
	}

	// Ask for events after seq=1 — that's been evicted.
	events, ok := es.After(1)
	if ok {
		t.Fatal("expected ok=false for evicted seq")
	}
	if events != nil {
		t.Fatal("expected nil events")
	}
}

func TestEventStream_RingBufferWraps(t *testing.T) {
	cap := 8
	es := NewEventStream(cap)

	// Write more than capacity.
	for i := 0; i < 20; i++ {
		es.Append("s1", "run.started", []byte(fmt.Sprintf(`{"i":%d}`, i)))
	}

	// Should still have the last 8 events (seq 13-20).
	events, ok := es.After(12)
	if !ok {
		t.Fatal("expected ok=true for recent events")
	}
	if len(events) != 8 {
		t.Fatalf("expected 8 events, got %d", len(events))
	}
	if events[0].Seq != 13 {
		t.Fatalf("expected first event seq=13, got %d", events[0].Seq)
	}
	if events[7].Seq != 20 {
		t.Fatalf("expected last event seq=20, got %d", events[7].Seq)
	}
}

func TestEventStream_ConcurrentAppendSafe(t *testing.T) {
	es := NewEventStream(1024)
	var wg sync.WaitGroup

	n := 100
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			es.Append("s1", "run.started", []byte(fmt.Sprintf(`{"i":%d}`, i)))
		}(i)
	}
	wg.Wait()

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != n {
		t.Fatalf("expected %d events, got %d", n, len(events))
	}

	// Verify monotonic sequence.
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("non-monotonic seq at index %d: %d <= %d",
				i, events[i].Seq, events[i-1].Seq)
		}
	}
}

func TestEventStream_MultiSessionInterleave(t *testing.T) {
	es := NewEventStream(1024)
	es.Append("sess-a", "run.started", []byte(`{}`))
	es.Append("sess-b", "run.started", []byte(`{}`))
	es.Append("sess-a", "run.completed", []byte(`{}`))
	es.Append("sess-b", "tool.call", []byte(`{}`))

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}

	// Verify interleaved session IDs.
	expectedSessions := []string{"sess-a", "sess-b", "sess-a", "sess-b"}
	for i, e := range events {
		if e.SessionID != expectedSessions[i] {
			t.Fatalf("event %d: expected session %q, got %q",
				i, expectedSessions[i], e.SessionID)
		}
	}
}

func TestEventStream_AfterZeroReturnsAll(t *testing.T) {
	es := NewEventStream(1024)
	es.Append("s1", "run.started", []byte(`{}`))
	es.Append("s1", "run.completed", []byte(`{}`))

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
}

func TestEventStream_EmptyBufferAfterZero(t *testing.T) {
	es := NewEventStream(1024)

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true for empty buffer")
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestEventStream_DataPreserved(t *testing.T) {
	es := NewEventStream(1024)
	original := map[string]any{"type": "run.completed", "session_id": "s1"}
	data, _ := json.Marshal(original)
	es.Append("s1", "run.completed", data)

	events, ok := es.After(0)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}

	var decoded map[string]any
	if err := json.Unmarshal(events[0].Data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal stored data: %v", err)
	}
	if decoded["type"] != "run.completed" {
		t.Fatalf("expected type=run.completed, got %v", decoded["type"])
	}
}
