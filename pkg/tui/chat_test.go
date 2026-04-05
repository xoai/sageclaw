package tui

import (
	"testing"
)

func TestChatState_String(t *testing.T) {
	tests := []struct {
		state ChatState
		want  string
	}{
		{ChatIdle, "idle"},
		{ChatSending, "sending"},
		{ChatStreaming, "streaming"},
		{ChatCompleting, "completing"},
		{ChatConsentNeeded, "consent"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("ChatState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestChatView_StateMachine(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	// Initial state.
	if cv.State() != ChatIdle {
		t.Fatalf("initial state should be ChatIdle, got %s", cv.State())
	}

	// IDLE → SENDING.
	cv.TransitionTo(ChatSending)
	if cv.State() != ChatSending {
		t.Fatalf("should be ChatSending, got %s", cv.State())
	}

	// SENDING → STREAMING (via StartAssistantMessage).
	cv.StartAssistantMessage("claude-sonnet-4", "anthropic")
	if cv.State() != ChatStreaming {
		t.Fatalf("should be ChatStreaming after StartAssistantMessage, got %s", cv.State())
	}

	// STREAMING → CONSENT_NEEDED.
	cv.TransitionTo(ChatConsentNeeded)
	if cv.State() != ChatConsentNeeded {
		t.Fatalf("should be ChatConsentNeeded, got %s", cv.State())
	}

	// Resume from consent → back to STREAMING.
	cv.ResumeFromConsent()
	if cv.State() != ChatStreaming {
		t.Fatalf("should resume to ChatStreaming, got %s", cv.State())
	}

	// STREAMING → IDLE (via CompleteAssistantMessage).
	cv.CompleteAssistantMessage()
	if cv.State() != ChatIdle {
		t.Fatalf("should be ChatIdle after complete, got %s", cv.State())
	}
}

func TestChatView_AddMessages(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddUserMessage("Hello")
	if len(cv.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.messages))
	}
	if cv.messages[0].Role != "user" || cv.messages[0].Content != "Hello" {
		t.Error("user message not stored correctly")
	}
	if !cv.messages[0].Complete {
		t.Error("user message should be complete")
	}
}

func TestChatView_StreamingFlow(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	// Start streaming.
	cv.StartAssistantMessage("model", "provider")
	if len(cv.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.messages))
	}
	if cv.messages[0].Complete {
		t.Error("streaming message should not be complete")
	}

	// Append chunks.
	cv.AppendChunk("Hello ")
	cv.AppendChunk("world")
	if cv.messages[0].Content != "Hello world" {
		t.Errorf("expected 'Hello world', got %q", cv.messages[0].Content)
	}

	// Complete.
	cv.CompleteAssistantMessage()
	if !cv.messages[0].Complete {
		t.Error("message should be complete after CompleteAssistantMessage")
	}
}

func TestChatView_ToolCallFlow(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddToolCall("list_files", `{"path":"."}`)
	cv.SetLastToolCallID("tc-1")
	if len(cv.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.messages))
	}
	if cv.messages[0].ToolStatus != ToolRunning {
		t.Error("tool should start as running")
	}
	if cv.messages[0].ToolCallID != "tc-1" {
		t.Error("tool call ID should be set")
	}

	cv.UpdateToolResult("tc-1", "main.go\nREADME.md", false)
	if cv.messages[0].ToolStatus != ToolSuccess {
		t.Error("tool should be success after result")
	}
	if cv.messages[0].Content != "main.go\nREADME.md" {
		t.Error("tool result content mismatch")
	}
}

func TestChatView_ToolCallError(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddToolCall("exec", `{"cmd":"fail"}`)
	cv.SetLastToolCallID("tc-2")
	cv.UpdateToolResult("tc-2", "exit code 1", true)

	if cv.messages[0].ToolStatus != ToolError {
		t.Error("tool should be error")
	}
}

func TestChatView_ToolCallIDMismatch(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddToolCall("list_files", `{"path":"."}`)
	cv.SetLastToolCallID("tc-1")

	// Wrong ID should not match.
	cv.UpdateToolResult("tc-wrong", "result", false)
	if cv.messages[0].ToolStatus != ToolRunning {
		t.Error("mismatched tool_call_id should not update tool status")
	}
}

func TestChatView_InputHistory(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddUserMessage("first")
	cv.AddUserMessage("second")
	cv.AddUserMessage("third")

	if len(cv.inputHistory) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(cv.inputHistory))
	}
	if cv.inputHistory[0] != "first" || cv.inputHistory[2] != "third" {
		t.Error("history order incorrect")
	}
}

func TestChatView_ConsentDuringCompleting(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	// Simulate: STREAMING → COMPLETING → CONSENT_NEEDED → back to COMPLETING.
	cv.TransitionTo(ChatStreaming)
	cv.TransitionTo(ChatCompleting)
	cv.TransitionTo(ChatConsentNeeded)

	if cv.State() != ChatConsentNeeded {
		t.Fatalf("should be consent, got %s", cv.State())
	}

	cv.ResumeFromConsent()
	if cv.State() != ChatCompleting {
		t.Fatalf("should resume to completing, got %s", cv.State())
	}
}

func TestChatView_AddThinking(t *testing.T) {
	cv := NewChatView(80, 24, ThemeDark)

	cv.AddThinking("Let me think about this...")
	if len(cv.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(cv.messages))
	}
	if cv.messages[0].Role != "thinking" {
		t.Error("should be thinking role")
	}
}
