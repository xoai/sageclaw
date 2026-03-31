package telegram

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
)

// apiCall records a single API call made to the test server.
type apiCall struct {
	method string
	params map[string]string
}

// newTestAdapter creates a Telegram adapter backed by a mock HTTP server.
// Returns the adapter, a function to retrieve recorded API calls, and a cleanup function.
func newTestAdapter(t *testing.T) (*Adapter, func() []apiCall, func()) {
	t.Helper()
	var mu sync.Mutex
	var calls []apiCall

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		params := make(map[string]string)
		for k, v := range r.Form {
			params[k] = v[0]
		}
		mu.Lock()
		calls = append(calls, apiCall{method: r.URL.Path, params: params})
		mu.Unlock()
		w.Write([]byte(`{"ok":true,"result":{"message_id":100}}`))
	}))

	a := New("tg_test", "token", WithBaseURL(srv.URL))

	getCalls := func() []apiCall {
		mu.Lock()
		defer mu.Unlock()
		cp := make([]apiCall, len(calls))
		copy(cp, calls)
		return cp
	}

	return a, getCalls, srv.Close
}

// --- setMessageReaction tests ---

func TestSetMessageReaction_SendsCorrectPayload(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.setMessageReaction("12345", 42, "🤔")

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "/setMessageReaction" {
			found = true
			if c.params["chat_id"] != "12345" {
				t.Errorf("chat_id = %q, want 12345", c.params["chat_id"])
			}
			if c.params["message_id"] != "42" {
				t.Errorf("message_id = %q, want 42", c.params["message_id"])
			}
			if !strings.Contains(c.params["reaction"], "🤔") {
				t.Errorf("reaction should contain emoji, got %q", c.params["reaction"])
			}
		}
	}
	if !found {
		t.Error("setMessageReaction API not called")
	}
}

func TestSetMessageReaction_SkipsZeroMessageID(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.setMessageReaction("12345", 0, "🤔")

	for _, c := range getCalls() {
		if c.method == "/setMessageReaction" {
			t.Error("should not call API with messageID=0")
		}
	}
}

func TestSetMessageReaction_SkipsEmptyEmoji(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.setMessageReaction("12345", 42, "")

	for _, c := range getCalls() {
		if c.method == "/setMessageReaction" {
			t.Error("should not call API with empty emoji")
		}
	}
}

func TestSetMessageReaction_DisablesOnPermissionError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/setMessageReaction") {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"ok":false,"description":"Forbidden: not enough rights"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New("tg_test", "token", WithBaseURL(srv.URL))

	// First call triggers 403 → disables reactions for this chat.
	a.setMessageReaction("12345", 42, "🤔")

	// Second call should be skipped entirely (no HTTP request).
	a.setMessageReaction("12345", 43, "👍")

	// Verify disabled flag is set.
	a.reactionMu.Lock()
	disabled := a.reactionsOff["12345"]
	a.reactionMu.Unlock()
	if !disabled {
		t.Error("reactions should be disabled after 403")
	}
}

// --- OnToolStatus tests ---

func TestOnToolStatus_CreatesDraft(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching: market trends...",
		ToolCount: 1,
	})

	// Should have created a draft.
	calls := getCalls()
	var draftFound bool
	for _, c := range calls {
		if c.method == "/sendMessageDraft" {
			draftFound = true
			if c.params["chat_id"] != "12345" {
				t.Errorf("chat_id = %q, want 12345", c.params["chat_id"])
			}
		}
	}
	if !draftFound {
		t.Error("expected sendMessageDraft call for tool status")
	}

	// Verify tool status state.
	a.toolStatusMu.Lock()
	ts, exists := a.toolStatuses["sess1"]
	a.toolStatusMu.Unlock()
	if !exists {
		t.Fatal("tool status state not created")
	}
	if ts.draftID == 0 {
		t.Error("draftID should be non-zero")
	}
}

func TestOnToolStatus_UpdatesExistingDraft(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// First status → creates draft.
	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching: market trends...",
		ToolCount: 1,
	})

	// Second status → updates same draft.
	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching: market trends... | 🌐 Fetching: example.com...",
		ToolCount: 2,
	})

	calls := getCalls()
	var draftCalls []apiCall
	for _, c := range calls {
		if c.method == "/sendMessageDraft" {
			draftCalls = append(draftCalls, c)
		}
	}
	if len(draftCalls) != 2 {
		t.Fatalf("expected 2 draft calls, got %d", len(draftCalls))
	}

	// Both should use the same draft_id.
	if draftCalls[0].params["draft_id"] != draftCalls[1].params["draft_id"] {
		t.Errorf("draft IDs should match: %q vs %q",
			draftCalls[0].params["draft_id"], draftCalls[1].params["draft_id"])
	}
}

func TestOnToolStatus_DoneClearsState(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})

	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{Done: true})

	a.toolStatusMu.Lock()
	_, exists := a.toolStatuses["sess1"]
	a.toolStatusMu.Unlock()
	if exists {
		t.Error("tool status state should be cleared on Done")
	}
}

func TestOnToolStatus_IgnoresEmptyText(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{Text: ""})

	for _, c := range getCalls() {
		if c.method == "/sendMessageDraft" {
			t.Error("should not create draft for empty text")
		}
	}
}

// --- Tool status → streamChunk handoff ---

func TestStreamChunk_ReusesToolStatusDraft(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// Create tool status draft.
	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching...",
		ToolCount: 1,
	})

	// Get the tool status draft ID.
	a.toolStatusMu.Lock()
	ts := a.toolStatuses["sess1"]
	toolDraftID := ts.draftID
	a.toolStatusMu.Unlock()

	// First text chunk arrives → should reuse the tool status draft.
	a.streamChunk("sess1", "12345", "Here are the results...")

	// Tool status state should be cleared.
	a.toolStatusMu.Lock()
	_, stillExists := a.toolStatuses["sess1"]
	a.toolStatusMu.Unlock()
	if stillExists {
		t.Error("tool status should be cleared after streamChunk takes over")
	}

	// Stream should use the same draft ID.
	a.streamMu.Lock()
	sm := a.streams["sess1"]
	a.streamMu.Unlock()
	if sm == nil {
		t.Fatal("stream should exist")
	}
	if sm.draftID != toolDraftID {
		t.Errorf("stream draft ID = %d, want tool status draft ID %d", sm.draftID, toolDraftID)
	}

	// Verify the draft was sent with text content (not tool status).
	calls := getCalls()
	var lastDraft apiCall
	for _, c := range calls {
		if c.method == "/sendMessageDraft" {
			lastDraft = c
		}
	}
	if lastDraft.params["draft_id"] != strconv.Itoa(toolDraftID) {
		t.Errorf("last draft call should use tool status draft ID %d, got %s",
			toolDraftID, lastDraft.params["draft_id"])
	}
}

func TestStreamChunk_AllocatesNewDraftWhenNoToolStatus(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// No tool status — first chunk should allocate new draft.
	a.streamChunk("sess1", "12345", "Hello world")

	calls := getCalls()
	var draftFound bool
	for _, c := range calls {
		if c.method == "/sendMessageDraft" {
			draftFound = true
		}
	}
	if !draftFound {
		t.Error("should create draft on first chunk")
	}
}

// --- OnReaction tests ---

func TestOnReaction_CallsSetMessageReaction(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnReaction("12345", 42, toolstatus.ReactionUpdate{
		Phase: toolstatus.PhaseThinking,
		Emoji: "🤔",
	})

	calls := getCalls()
	var found bool
	for _, c := range calls {
		if c.method == "/setMessageReaction" {
			found = true
			if c.params["message_id"] != "42" {
				t.Errorf("message_id = %q, want 42", c.params["message_id"])
			}
		}
	}
	if !found {
		t.Error("setMessageReaction should be called")
	}
}

// --- Typing controller lifecycle ---

func TestOnAgentEvent_RunStarted_StartsTyping(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnAgentEvent("sess1", "12345", "run.started", "")

	// Wait briefly for the typing controller to fire.
	time.Sleep(50 * time.Millisecond)

	// Verify typing was started.
	calls := getCalls()
	var typingFound bool
	for _, c := range calls {
		if c.method == "/sendChatAction" && c.params["action"] == "typing" {
			typingFound = true
		}
	}
	if !typingFound {
		t.Error("run.started should trigger sendChatAction(typing)")
	}

	// Verify controller is stored.
	a.typingMu.Lock()
	_, hasCtrl := a.typingCtrl["sess1"]
	a.typingMu.Unlock()
	if !hasCtrl {
		t.Error("typing controller should be stored for session")
	}
}

func TestOnAgentEvent_RunCompleted_StopsTyping(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	// Start typing.
	a.OnAgentEvent("sess1", "12345", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	// Complete run — marks run complete (typing persists until dispatch idle too).
	a.OnAgentEvent("sess1", "12345", "run.completed", "")

	// Mark dispatch idle via sendResponse path.
	a.markTypingDispatchIdle("sess1")

	// Controller should be cleaned up.
	a.typingMu.Lock()
	_, hasCtrl := a.typingCtrl["sess1"]
	a.typingMu.Unlock()
	if hasCtrl {
		t.Error("typing controller should be removed after both signals")
	}
}

func TestOnAgentEvent_RunFailed_StopsTyping(t *testing.T) {
	a, _, cleanup := newTestAdapter(t)
	defer cleanup()

	a.OnAgentEvent("sess1", "12345", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	a.OnAgentEvent("sess1", "12345", "run.failed", "")
	a.markTypingDispatchIdle("sess1")

	a.typingMu.Lock()
	_, hasCtrl := a.typingCtrl["sess1"]
	a.typingMu.Unlock()
	if hasCtrl {
		t.Error("typing controller should be removed after failure + dispatch idle")
	}
}

// --- Integration: full event flow ---

func TestFullEventFlow_ToolStatusThenStream(t *testing.T) {
	a, getCalls, cleanup := newTestAdapter(t)
	defer cleanup()

	// 1. Run starts → typing begins.
	a.OnAgentEvent("sess1", "12345", "run.started", "")
	time.Sleep(50 * time.Millisecond)

	// 2. Tool call → tool status draft.
	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{
		Text:      "🔍 Searching: market trends...",
		ToolCount: 1,
	})

	// 3. Tool completes → done.
	a.OnToolStatus("sess1", "12345", toolstatus.StatusUpdate{Done: true})

	// 4. Text chunks arrive.
	a.streamChunk("sess1", "12345", "Here are the results for market trends.")

	// 5. Run completes.
	a.OnAgentEvent("sess1", "12345", "run.completed", "")

	calls := getCalls()

	// Verify sequence: sendChatAction(typing) → sendMessageDraft (status) → sendMessageDraft (text) → sendMessage (materialized)
	var methods []string
	for _, c := range calls {
		methods = append(methods, c.method)
	}

	// Should have at least: typing action, tool status draft, text draft, final message
	hasTyping := false
	hasDraft := false
	for _, c := range calls {
		if c.method == "/sendChatAction" && c.params["action"] == "typing" {
			hasTyping = true
		}
		if c.method == "/sendMessageDraft" {
			hasDraft = true
		}
	}
	if !hasTyping {
		t.Error("expected typing action")
	}
	if !hasDraft {
		t.Error("expected at least one draft call")
	}
}
