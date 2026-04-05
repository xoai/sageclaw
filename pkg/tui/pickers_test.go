package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAgentPicker_Select(t *testing.T) {
	agents := []AgentInfo{
		{ID: "a1", Name: "SageClaw", Role: "assistant", Avatar: "🤖"},
		{ID: "a2", Name: "Researcher", Role: "research", Avatar: "🔬"},
	}
	picker := NewAgentPicker(agents, 80, 24)

	// Press enter to select the first agent.
	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter")
	}

	msg := cmd()
	selected, ok := msg.(AgentSelectedMsg)
	if !ok {
		t.Fatalf("expected AgentSelectedMsg, got %T", msg)
	}
	if selected.Agent.ID != "a1" {
		t.Errorf("expected agent a1, got %q", selected.Agent.ID)
	}
}

func TestAgentPicker_Cancel(t *testing.T) {
	agents := []AgentInfo{{ID: "a1", Name: "Test"}}
	picker := NewAgentPicker(agents, 80, 24)

	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}

	msg := cmd()
	cancelled, ok := msg.(PickerCancelledMsg)
	if !ok {
		t.Fatalf("expected PickerCancelledMsg, got %T", msg)
	}
	if cancelled.Overlay != OverlayAgentPicker {
		t.Error("wrong overlay type in cancel")
	}
}

func TestSessionPicker_NewSession(t *testing.T) {
	sessions := []SessionInfo{
		{ID: "s1", Title: "Old chat", CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
	}
	picker := NewSessionPicker(sessions, 80, 24)

	// First item should be "New session".
	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter")
	}

	msg := cmd()
	selected, ok := msg.(SessionSelectedMsg)
	if !ok {
		t.Fatalf("expected SessionSelectedMsg, got %T", msg)
	}
	if !selected.IsNew {
		t.Error("first item should be new session")
	}
}

func TestSessionPicker_Cancel(t *testing.T) {
	picker := NewSessionPicker(nil, 80, 24)

	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected command from esc")
	}

	msg := cmd()
	_, ok := msg.(PickerCancelledMsg)
	if !ok {
		t.Fatalf("expected PickerCancelledMsg, got %T", msg)
	}
}

func TestModelPicker_Select(t *testing.T) {
	models := []ModelInfo{
		{ID: "claude-sonnet-4", Name: "Claude Sonnet 4", Provider: "anthropic", Tier: "strong"},
		{ID: "gpt-4o", Name: "GPT-4o", Provider: "openai", Tier: "strong"},
	}
	picker := NewModelPicker(models, 80, 24)

	_, cmd := picker.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected command from enter")
	}

	msg := cmd()
	selected, ok := msg.(ModelSelectedMsg)
	if !ok {
		t.Fatalf("expected ModelSelectedMsg, got %T", msg)
	}
	if selected.Model.ID != "claude-sonnet-4" {
		t.Errorf("expected first model, got %q", selected.Model.ID)
	}
}

func TestAgentItem_FilterValue(t *testing.T) {
	item := agentItem{info: AgentInfo{Name: "SageClaw", Role: "assistant"}}
	if got := item.FilterValue(); got != "SageClaw assistant" {
		t.Errorf("FilterValue() = %q", got)
	}
}

func TestSessionItem_Description_WithTokens(t *testing.T) {
	item := sessionItem{info: SessionInfo{
		TokensUsed: 1500,
		CreatedAt:  time.Now().Add(-30 * time.Minute).Format(time.RFC3339),
	}}
	desc := item.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
}

func TestSessionItem_NewSession(t *testing.T) {
	item := sessionItem{isNew: true}
	if item.Title() != "✨ New session" {
		t.Errorf("new session title: %q", item.Title())
	}
	if item.FilterValue() != "new session" {
		t.Errorf("new session filter: %q", item.FilterValue())
	}
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{10 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{2 * time.Hour, "2h ago"},
		{48 * time.Hour, "2d ago"},
	}
	for _, tt := range tests {
		got := relativeTime(time.Now().Add(-tt.d))
		if got != tt.want {
			t.Errorf("relativeTime(-%v) = %q, want %q", tt.d, got, tt.want)
		}
	}
}
