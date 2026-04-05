package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// SSE message types sent to the bubbletea update loop.

// SSEEvent is a raw parsed SSE event from the server.
type SSEEvent struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id"`
	AgentID    string          `json:"agent_id"`
	Text       string          `json:"text"`
	Iteration  int             `json:"iteration"`
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	EventSeq   int64           `json:"event_seq"`
	ToolCall   json.RawMessage `json:"tool_call,omitempty"`
	ToolResult json.RawMessage `json:"tool_result,omitempty"`
	Consent    json.RawMessage `json:"consent,omitempty"`
}

// SSEEventMsg wraps a parsed SSE event for the bubbletea update loop.
type SSEEventMsg struct{ Event SSEEvent }

// SSEErrorMsg signals an SSE connection error.
type SSEErrorMsg struct{ Err error }

// SSESyncMsg signals that events were expired and a full reload is needed.
type SSESyncMsg struct{ Reason string }

// sseStream holds the persistent SSE connection state.
// Events are sent through a channel; the tea.Cmd reads one at a time.
type sseStream struct {
	events chan tea.Msg
	done   chan struct{}
}

// newSSEStream creates and starts a persistent SSE connection.
func newSSEStream(client *TUIClient, sessionFilter string, lastEventID string) *sseStream {
	s := &sseStream{
		events: make(chan tea.Msg, 64), // Buffer to avoid blocking the reader.
		done:   make(chan struct{}),
	}
	go s.readLoop(client, sessionFilter, lastEventID)
	return s
}

// readLoop maintains the SSE connection, reconnecting on failure.
func (s *sseStream) readLoop(client *TUIClient, sessionFilter string, lastEventID string) {
	defer close(s.events)

	attempt := 0
	for {
		select {
		case <-s.done:
			return
		default:
		}

		err := s.connectAndRead(client, sessionFilter, lastEventID)
		if err != nil {
			attempt++
			if attempt > 30 {
				s.send(SSEErrorMsg{Err: fmt.Errorf("SSE gave up after %d attempts: %w", attempt, err)})
				return
			}
			// Backoff before reconnect.
			delay := time.Duration(1<<min(attempt, 5)) * time.Second
			if delay > 30*time.Second {
				delay = 30 * time.Second
			}
			select {
			case <-time.After(delay):
			case <-s.done:
				return
			}
			continue
		}
		// Connection closed cleanly — reconnect immediately.
		attempt = 0
	}
}

// connectAndRead opens one SSE connection and reads events until it closes.
func (s *sseStream) connectAndRead(client *TUIClient, sessionFilter string, lastEventID string) error {
	url := client.BaseURL() + "/events"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}

	resp, err := client.SSEClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SSE status: %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	var dataLines []string

	for scanner.Scan() {
		select {
		case <-s.done:
			return nil
		default:
		}

		line := scanner.Text()

		// Heartbeat comment.
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Empty line = event boundary.
		if line == "" {
			if len(dataLines) > 0 {
				data := strings.Join(dataLines, "\n")
				dataLines = nil

				var event SSEEvent
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}

				if event.Type == "sync" {
					s.send(SSESyncMsg{Reason: "events_expired"})
					continue
				}

				if sessionFilter != "" && event.SessionID != "" && event.SessionID != sessionFilter {
					continue
				}

				// Track last event ID for reconnection.
				if event.EventSeq > 0 {
					lastEventID = fmt.Sprintf("%d", event.EventSeq)
				}

				s.send(SSEEventMsg{Event: event})
			}
			continue
		}

		if strings.HasPrefix(line, "id: ") || strings.HasPrefix(line, "id:") {
			// ID tracking handled via event.EventSeq.
		} else if strings.HasPrefix(line, "data: ") || strings.HasPrefix(line, "data:") {
			d := strings.TrimPrefix(line, "data: ")
			d = strings.TrimPrefix(d, "data:")
			dataLines = append(dataLines, d)
		}
	}

	return fmt.Errorf("SSE stream ended")
}

// send sends a message to the events channel, dropping if full.
func (s *sseStream) send(msg tea.Msg) {
	select {
	case s.events <- msg:
	default:
		// Channel full — drop oldest to prevent blocking.
		select {
		case <-s.events:
		default:
		}
		s.events <- msg
	}
}

// close stops the SSE stream.
func (s *sseStream) close() {
	select {
	case <-s.done:
	default:
		close(s.done)
	}
}

// SSESubscribe returns a tea.Cmd that reads the next event from the stream.
// Call this repeatedly from Update to drain events one at a time.
func SSESubscribe(stream *sseStream) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-stream.events
		if !ok {
			return SSEErrorMsg{Err: fmt.Errorf("SSE stream closed")}
		}
		return msg
	}
}
