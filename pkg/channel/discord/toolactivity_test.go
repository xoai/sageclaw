package discord

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
)

// apiCall records a single API call made to the test server.
type apiCall struct {
	method string // HTTP method (GET, POST, PATCH, PUT, DELETE)
	path   string // URL path
	body   string // Request body
}

// newTestAdapter creates a Discord adapter backed by a mock HTTP server.
func newTestAdapter(t *testing.T) (*Adapter, func() []apiCall, func()) {
	t.Helper()
	var mu sync.Mutex
	var calls []apiCall

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes := make([]byte, 0)
		if r.Body != nil {
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			bodyBytes = buf[:n]
		}
		mu.Lock()
		calls = append(calls, apiCall{
			method: r.Method,
			path:   r.URL.Path,
			body:   string(bodyBytes),
		})
		mu.Unlock()
		// Return a message response with an ID.
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "msg_12345"})
	}))

	a := New("dc_test", "token")
	a.apiBase = srv.URL

	getCalls := func() []apiCall {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]apiCall, len(calls))
		copy(cp, calls)
		return cp
	}

	return a, getCalls, srv.Close
}

// --- sendMessage tests ---

func TestSendMessage_ReturnsMessageID(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	msgID, err := a.sendMessage("chan1", "hello")
	if err != nil {
		t.Fatalf("sendMessage: %v", err)
	}
	if msgID != "msg_12345" {
		t.Errorf("message ID = %q, want msg_12345", msgID)
	}
}

// --- editMessage tests ---

func TestEditMessage_SendsPATCH(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	err := a.editMessage("chan1", "msg1", "updated text")
	if err != nil {
		t.Fatalf("editMessage: %v", err)
	}

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "PATCH" && strings.Contains(c.path, "/messages/msg1") {
			found = true
			if !strings.Contains(c.body, "updated text") {
				t.Errorf("body should contain updated text, got %q", c.body)
			}
		}
	}
	if !found {
		t.Error("PATCH request not found")
	}
}

// --- sendTypingIndicator tests ---

func TestSendTypingIndicator_SendsPOST(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	err := a.sendTypingIndicator("chan1")
	if err != nil {
		t.Fatalf("sendTypingIndicator: %v", err)
	}

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "POST" && strings.Contains(c.path, "/typing") {
			found = true
		}
	}
	if !found {
		t.Error("POST /typing not found")
	}
}

// --- addReaction tests ---

func TestAddReaction_SendsPUT(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	err := a.addReaction("chan1", "msg1", "🤔")
	if err != nil {
		t.Fatalf("addReaction: %v", err)
	}

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "PUT" && strings.Contains(c.path, "/reactions/") {
			found = true
		}
	}
	if !found {
		t.Error("PUT /reactions not found")
	}
}

func TestAddReaction_DisablesOnForbidden(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" && strings.Contains(r.URL.Path, "/reactions/") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"message":"Missing Permissions"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"id": "msg1"})
	}))
	defer srv.Close()

	a := New("dc_test", "token")
	a.apiBase = srv.URL

	a.addReaction("chan1", "msg1", "🤔")

	a.reactionMu.Lock()
	disabled := a.reactionsOff["chan1"]
	a.reactionMu.Unlock()
	if !disabled {
		t.Error("reactions should be disabled after 403")
	}
}

// --- removeReaction tests ---

func TestRemoveReaction_SendsDELETE(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.removeReaction("chan1", "msg1", "🤔")

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "DELETE" && strings.Contains(c.path, "/reactions/") {
			found = true
		}
	}
	if !found {
		t.Error("DELETE /reactions not found")
	}
}

// --- OnAgentEvent + typing tests ---

func TestOnAgentEvent_RunStarted_StartsTyping(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnAgentEvent("sess1", "chan1", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	calls := getCalls()
	var typingFound bool
	for _, c := range calls {
		if c.method == "POST" && strings.Contains(c.path, "/typing") {
			typingFound = true
		}
	}
	if !typingFound {
		t.Error("run.started should trigger typing indicator")
	}
}

func TestOnAgentEvent_RunCompleted_CleansUp(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnAgentEvent("sess1", "chan1", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	a.OnAgentEvent("sess1", "chan1", "run.completed", "")
	a.markTypingDispatchIdle("sess1")

	a.typingMu.Lock()
	_, hasCtrl := a.typingCtrl["sess1"]
	a.typingMu.Unlock()
	if hasCtrl {
		t.Error("typing controller should be removed")
	}
}

// --- Streaming tests ---

func TestStreamChunk_CreatesAndEditsMessage(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// First chunk — sends message.
	a.streamChunk("sess1", "chan1", "Hello ")
	time.Sleep(50 * time.Millisecond) // Let async message ID update happen.

	// Second chunk (after debounce) — edits.
	a.streamMu.Lock()
	if sm, ok := a.streams["sess1"]; ok {
		sm.lastEdit = time.Time{} // Reset throttle.
	}
	a.streamMu.Unlock()
	a.streamChunk("sess1", "chan1", "world!")

	calls := getCalls()
	var postCount, patchCount int
	for _, c := range calls {
		if c.method == "POST" && strings.Contains(c.path, "/messages") && !strings.Contains(c.path, "/typing") {
			postCount++
		}
		if c.method == "PATCH" {
			patchCount++
		}
	}
	if postCount < 1 {
		t.Error("expected at least one POST for initial message")
	}
	if patchCount < 1 {
		t.Error("expected at least one PATCH for edit")
	}
}

func TestEndStream_FinalizesMessage(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// Set up a stream with a known message ID.
	a.streamMu.Lock()
	a.streams["sess1"] = &streamState{
		channelID: "chan1",
		messageID: "msg_123",
		text:      "Final text",
		lastEdit:  time.Now(),
	}
	a.streamMu.Unlock()

	a.endStream("sess1")

	calls := getCalls()
	var patchFound bool
	for _, c := range calls {
		if c.method == "PATCH" && strings.Contains(c.body, "Final text") {
			patchFound = true
		}
	}
	if !patchFound {
		t.Error("endStream should PATCH with final text")
	}

	// Stream should be cleaned up.
	a.streamMu.Lock()
	_, exists := a.streams["sess1"]
	a.streamMu.Unlock()
	if exists {
		t.Error("stream should be removed after endStream")
	}
}

// --- OnToolStatus tests ---

func TestOnToolStatus_CreatesStatusMessage(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})
	time.Sleep(50 * time.Millisecond)

	calls := getCalls()
	var postFound bool
	for _, c := range calls {
		if c.method == "POST" && strings.Contains(c.body, "Searching") {
			postFound = true
		}
	}
	if !postFound {
		t.Error("OnToolStatus should send a message with status text")
	}

	// Should be tracked as tool status.
	a.streamMu.Lock()
	sm := a.streams["sess1"]
	a.streamMu.Unlock()
	if sm == nil || !sm.isToolStatus {
		t.Error("stream should be marked as tool status")
	}
}

func TestOnToolStatus_EditsOnUpdate(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// Create initial status.
	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})
	time.Sleep(50 * time.Millisecond)

	// Update status.
	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{
		Text:      "🔍 Searching... | 🌐 Fetching...",
		ToolCount: 2,
	})

	calls := getCalls()
	var patchFound bool
	for _, c := range calls {
		if c.method == "PATCH" && strings.Contains(c.body, "Fetching") {
			patchFound = true
		}
	}
	if !patchFound {
		t.Error("OnToolStatus update should PATCH the message")
	}
}

func TestOnToolStatus_DoneClearsToolStatusFlag(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})
	time.Sleep(50 * time.Millisecond)

	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{Done: true})

	a.streamMu.Lock()
	sm := a.streams["sess1"]
	a.streamMu.Unlock()
	if sm != nil && sm.isToolStatus {
		t.Error("tool status flag should be cleared on Done")
	}
}

// --- OnReaction tests ---

func TestOnReaction_AddsAndRemovesPrevious(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// First reaction.
	a.OnReaction("chan1", "msg1", toolstatus.ReactionUpdate{
		Phase: toolstatus.PhaseThinking,
		Emoji: "🤔",
	})

	// Second reaction — should remove the first, then add the new one.
	a.OnReaction("chan1", "msg1", toolstatus.ReactionUpdate{
		Phase: toolstatus.PhaseTool,
		Emoji: "🔥",
	})

	calls := getCalls()
	var putCount, deleteCount int
	for _, c := range calls {
		if c.method == "PUT" && strings.Contains(c.path, "/reactions/") {
			putCount++
		}
		if c.method == "DELETE" && strings.Contains(c.path, "/reactions/") {
			deleteCount++
		}
	}
	if putCount < 2 {
		t.Errorf("expected at least 2 PUT reactions, got %d", putCount)
	}
	if deleteCount < 1 {
		t.Errorf("expected at least 1 DELETE reaction (remove previous), got %d", deleteCount)
	}
}

func TestOnReaction_SkipsEmptyMsgID(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnReaction("chan1", "", toolstatus.ReactionUpdate{Emoji: "🤔"})

	for _, c := range getCalls() {
		if strings.Contains(c.path, "/reactions/") {
			t.Error("should not call reactions API with empty message ID")
		}
	}
}

// --- Integration: full flow ---

func TestFullFlow_ToolStatusThenStream(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// 1. Run starts.
	a.OnAgentEvent("sess1", "chan1", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	// 2. Tool status.
	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})
	time.Sleep(50 * time.Millisecond)

	// 3. Tool done.
	a.OnToolStatus("sess1", "chan1", toolstatus.StatusUpdate{Done: true})

	// 4. First text chunk — should reuse the tool status message.
	a.streamMu.Lock()
	sm := a.streams["sess1"]
	hasMsgID := sm != nil && sm.messageID != ""
	a.streamMu.Unlock()

	if hasMsgID {
		// Reset throttle to allow edit.
		a.streamMu.Lock()
		sm.lastEdit = time.Time{}
		a.streamMu.Unlock()
	}
	a.streamChunk("sess1", "chan1", "Results: the market is growing.")

	// 5. Run completes.
	a.OnAgentEvent("sess1", "chan1", "run.completed", "")

	calls := getCalls()
	var methods []string
	for _, c := range calls {
		methods = append(methods, c.method+" "+c.path)
	}

	// Should have typing, POST (status msg), PATCH (text edit or final).
	hasTyping := false
	hasPost := false
	for _, c := range calls {
		if c.method == "POST" && strings.Contains(c.path, "/typing") {
			hasTyping = true
		}
		if c.method == "POST" && strings.Contains(c.path, "/messages") && !strings.Contains(c.path, "/typing") {
			hasPost = true
		}
	}
	if !hasTyping {
		t.Error("expected typing indicator")
	}
	if !hasPost {
		t.Error("expected at least one POST message")
	}
}
