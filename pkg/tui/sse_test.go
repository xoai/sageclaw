package tui

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSSEStream_ParsesEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "id: 1\ndata: {\"type\":\"chunk\",\"session_id\":\"s1\",\"text\":\"hello\",\"event_seq\":1}\n\n")
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "", "")
	defer stream.close()

	select {
	case msg := <-stream.events:
		evt, ok := msg.(SSEEventMsg)
		if !ok {
			t.Fatalf("expected SSEEventMsg, got %T: %v", msg, msg)
		}
		if evt.Event.Type != "chunk" {
			t.Errorf("expected type chunk, got %s", evt.Event.Type)
		}
		if evt.Event.Text != "hello" {
			t.Errorf("expected text hello, got %s", evt.Event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestSSEStream_FiltersSession(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "id: 1\ndata: {\"type\":\"chunk\",\"session_id\":\"other\",\"text\":\"skip\"}\n\n")
		fmt.Fprint(w, "id: 2\ndata: {\"type\":\"chunk\",\"session_id\":\"s1\",\"text\":\"match\"}\n\n")
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "s1", "")
	defer stream.close()

	select {
	case msg := <-stream.events:
		evt, ok := msg.(SSEEventMsg)
		if !ok {
			t.Fatalf("expected SSEEventMsg, got %T", msg)
		}
		if evt.Event.Text != "match" {
			t.Errorf("expected 'match', got %s", evt.Event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSEStream_HandlesSync(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, "data: {\"type\":\"sync\",\"reason\":\"events_expired\"}\n\n")
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "", "")
	defer stream.close()

	select {
	case msg := <-stream.events:
		_, ok := msg.(SSESyncMsg)
		if !ok {
			t.Fatalf("expected SSESyncMsg, got %T", msg)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSEStream_IgnoresHeartbeats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprint(w, ": heartbeat\n\ndata: {\"type\":\"chunk\",\"text\":\"after-hb\"}\n\n")
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "", "")
	defer stream.close()

	select {
	case msg := <-stream.events:
		evt, ok := msg.(SSEEventMsg)
		if !ok {
			t.Fatalf("expected SSEEventMsg, got %T", msg)
		}
		if evt.Event.Text != "after-hb" {
			t.Errorf("expected 'after-hb', got %s", evt.Event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSEStream_SendsLastEventID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastID := r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fmt.Fprintf(w, "data: {\"type\":\"chunk\",\"text\":\"%s\"}\n\n", lastID)
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "", "42")
	defer stream.close()

	select {
	case msg := <-stream.events:
		evt := msg.(SSEEventMsg)
		if evt.Event.Text != "42" {
			t.Errorf("expected Last-Event-ID '42', got %s", evt.Event.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSEStream_MultipleEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(200)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, "data: {\"type\":\"chunk\",\"text\":\"chunk-%d\",\"event_seq\":%d}\n\n", i, i+1)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	c := NewTUIClient(srv.URL)
	stream := newSSEStream(c, "", "")
	defer stream.close()

	for i := 0; i < 5; i++ {
		select {
		case msg := <-stream.events:
			evt, ok := msg.(SSEEventMsg)
			if !ok {
				t.Fatalf("event %d: expected SSEEventMsg, got %T", i, msg)
			}
			expected := fmt.Sprintf("chunk-%d", i)
			if evt.Event.Text != expected {
				t.Errorf("event %d: expected %q, got %q", i, expected, evt.Event.Text)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}
