package tui

import (
	"strings"
	"testing"
)

func TestRenderUserMessage(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderUserMessage("Hello world", 80)

	if !strings.Contains(out, "You") {
		t.Error("user message should contain 'You' label")
	}
	if !strings.Contains(out, "Hello world") {
		t.Error("user message should contain the text")
	}
}

func TestRenderAssistantMessage(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderAssistantMessage("**bold** text", "Agent", 80)

	if !strings.Contains(out, "Agent") {
		t.Error("assistant message should contain agent label")
	}
	// Glamour should render bold as ANSI.
	if !strings.Contains(out, "bold") {
		t.Error("assistant message should contain the text content")
	}
}

func TestRenderStreamingMessage(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderStreamingMessage("partial text", "Agent", 80)

	if !strings.Contains(out, "Agent ...") {
		t.Error("streaming message should show 'Agent ...' label")
	}
	if !strings.Contains(out, "partial text") {
		t.Error("streaming message should contain the text")
	}
}

func TestRenderToolCall_Running(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderToolCall("list_files", `{"path":"."}`, ToolRunning, "", 80)

	if !strings.Contains(out, "list_files") {
		t.Error("tool call should show tool name")
	}
	if !strings.Contains(out, "⟳") {
		t.Error("running tool should show spinner icon")
	}
}

func TestRenderToolCall_Success(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderToolCall("list_files", `{"path":"."}`, ToolSuccess, "main.go\nREADME.md", 80)

	if !strings.Contains(out, "✓") {
		t.Error("successful tool should show check icon")
	}
	if !strings.Contains(out, "list_files") {
		t.Error("tool name should be displayed")
	}
}

func TestRenderToolCall_Error(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderToolCall("exec", `{"cmd":"fail"}`, ToolError, "exit 1", 80)

	if !strings.Contains(out, "✗") {
		t.Error("error tool should show error icon")
	}
	if !strings.Contains(out, "exit 1") {
		t.Error("error result should be displayed")
	}
}

func TestRenderThinking(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderThinking("I need to check the files", 80)

	if !strings.Contains(out, "Thinking") {
		t.Error("thinking block should contain label")
	}
	if !strings.Contains(out, "check the files") {
		t.Error("thinking block should contain text")
	}
}

func TestRenderStatusBar(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderStatusBar("claude-sonnet-4", "anthropic", 1200, "3.4s", true, 80)

	if !strings.Contains(out, "claude-sonnet-4") {
		t.Error("status bar should show model name")
	}
	if !strings.Contains(out, "1.2K tokens") {
		t.Error("status bar should show formatted token count")
	}
	if !strings.Contains(out, "connected") {
		t.Error("status bar should show connection status")
	}
}

func TestRenderStatusBar_Disconnected(t *testing.T) {
	r := NewRenderer(80, ThemeDark)
	out := r.RenderStatusBar("", "", 0, "", false, 80)

	if !strings.Contains(out, "reconnecting") {
		t.Error("disconnected status bar should show 'reconnecting'")
	}
}

func TestFormatTokens(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{500, "500 tokens"},
		{1000, "1.0K tokens"},
		{1200, "1.2K tokens"},
		{15000, "15.0K tokens"},
	}
	for _, tt := range tests {
		got := formatTokens(tt.n)
		if got != tt.want {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestTruncateLines(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5\nline6\nline7"
	out := truncateLines(text, 3, 80)

	if !strings.Contains(out, "line1") {
		t.Error("should contain first lines")
	}
	if !strings.Contains(out, "4 lines collapsed") {
		t.Error("should show collapsed count")
	}
}

func TestFormatToolInput_JSON(t *testing.T) {
	out := formatToolInput(`{"path":".","recursive":true}`, 60)
	if out == "" {
		t.Error("should format JSON input")
	}
}

func TestFormatToolInput_NonJSON(t *testing.T) {
	out := formatToolInput("just a string", 60)
	if out != "just a string" {
		t.Errorf("non-JSON should pass through, got %q", out)
	}
}
