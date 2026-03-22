package orchestration

import (
	"context"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
)

// Handoff transfers a conversation session from one agent to another.
type Handoff struct {
	store   store.Store
	configs map[string]string // agentID → name (for display)
}

// NewHandoff creates a handoff manager.
func NewHandoff(s store.Store, agentNames map[string]string) *Handoff {
	return &Handoff{store: s, configs: agentNames}
}

// Transfer changes the agent for a session and injects a system message.
func (h *Handoff) Transfer(ctx context.Context, sessionID, sourceAgentID, targetAgentID, reason string) error {
	// Verify target exists.
	if _, ok := h.configs[targetAgentID]; !ok {
		return fmt.Errorf("unknown target agent: %s", targetAgentID)
	}

	// Update session agent_id.
	_, err := h.store.DB().ExecContext(ctx,
		`UPDATE sessions SET agent_id = ?, updated_at = datetime('now') WHERE id = ?`,
		targetAgentID, sessionID)
	if err != nil {
		return fmt.Errorf("updating session agent: %w", err)
	}

	// Inject handoff message into history.
	sourceName := sourceAgentID
	if name, ok := h.configs[sourceAgentID]; ok {
		sourceName = name
	}
	targetName := targetAgentID
	if name, ok := h.configs[targetAgentID]; ok {
		targetName = name
	}

	handoffMsg := fmt.Sprintf("[Conversation handed off from %s to %s", sourceName, targetName)
	if reason != "" {
		handoffMsg += " because: " + reason
	}
	handoffMsg += "]"

	return h.store.AppendMessages(ctx, sessionID, []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: handoffMsg}}},
	})
}
