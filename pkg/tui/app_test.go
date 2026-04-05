package tui

import (
	"encoding/json"
	"testing"
)

func TestParseToolCall(t *testing.T) {
	raw := json.RawMessage(`{"id":"tc-123","name":"list_files","input":{"path":"."}}`)
	id, name, input := parseToolCall(raw)

	if id != "tc-123" {
		t.Errorf("expected id tc-123, got %q", id)
	}
	if name != "list_files" {
		t.Errorf("expected name list_files, got %q", name)
	}
	if input != `{"path":"."}` {
		t.Errorf("expected JSON input, got %q", input)
	}
}

func TestParseToolCall_StringInput(t *testing.T) {
	raw := json.RawMessage(`{"id":"tc-1","name":"exec","input":"ls -la"}`)
	id, name, input := parseToolCall(raw)

	if id != "tc-1" {
		t.Errorf("expected id tc-1, got %q", id)
	}
	if name != "exec" {
		t.Errorf("expected name exec, got %q", name)
	}
	if input != `"ls -la"` {
		t.Errorf("expected quoted string input, got %q", input)
	}
}

func TestParseToolCall_Invalid(t *testing.T) {
	raw := json.RawMessage(`invalid`)
	_, name, _ := parseToolCall(raw)
	if name != "unknown" {
		t.Errorf("expected unknown for invalid JSON, got %q", name)
	}
}

func TestParseToolResult(t *testing.T) {
	raw := json.RawMessage(`{"tool_call_id":"tc-123","content":"main.go\nREADME.md","is_error":false}`)
	toolCallID, content, isError := parseToolResult(raw)

	if toolCallID != "tc-123" {
		t.Errorf("expected tool_call_id tc-123, got %q", toolCallID)
	}
	if content != "main.go\nREADME.md" {
		t.Errorf("expected content, got %q", content)
	}
	if isError {
		t.Error("expected is_error false")
	}
}

func TestParseToolResult_Error(t *testing.T) {
	raw := json.RawMessage(`{"tool_call_id":"tc-456","content":"exit code 1","is_error":true}`)
	toolCallID, content, isError := parseToolResult(raw)

	if toolCallID != "tc-456" {
		t.Errorf("expected tc-456, got %q", toolCallID)
	}
	if content != "exit code 1" {
		t.Errorf("expected error content, got %q", content)
	}
	if !isError {
		t.Error("expected is_error true")
	}
}

func TestParseConsentRequest(t *testing.T) {
	raw := json.RawMessage(`{"nonce":"abc-123","tool_name":"delete_file","explanation":"This will delete a file","risk_level":"sensitive"}`)
	req := parseConsentRequest(raw)

	if req.Nonce != "abc-123" {
		t.Errorf("expected nonce abc-123, got %q", req.Nonce)
	}
	if req.ToolName != "delete_file" {
		t.Errorf("expected tool_name delete_file, got %q", req.ToolName)
	}
	if req.Explanation != "This will delete a file" {
		t.Errorf("expected explanation, got %q", req.Explanation)
	}
	if req.RiskLevel != "sensitive" {
		t.Errorf("expected risk_level sensitive, got %q", req.RiskLevel)
	}
}

func TestParseConsentRequest_Invalid(t *testing.T) {
	raw := json.RawMessage(`invalid`)
	req := parseConsentRequest(raw)
	if req.ToolName != "unknown" {
		t.Errorf("expected unknown for invalid JSON, got %q", req.ToolName)
	}
}
