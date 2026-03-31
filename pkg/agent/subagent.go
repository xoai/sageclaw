package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/xoai/sageclaw/pkg/canonical"
)

// SubagentConfig holds configuration for the subagent manager.
type SubagentConfig struct {
	MaxChildrenPerAgent int           // Max concurrent subagents per parent agent. Default: 5.
	MaxConcurrent       int           // Global concurrent subagent limit. Default: 8.
	DefaultTimeout      time.Duration // Default timeout per subagent. Default: 5 min.
}

// SubagentTask represents a spawned subagent's state.
type SubagentTask struct {
	ID          string
	ParentAgent string // Agent that spawned this.
	SessionID   string // Parent's session.
	Task        string // Task prompt.
	Label       string // Human-readable label.
	Mode        string // "sync" or "async".
	Status      string // "running", "completed", "failed", "cancelled".
	Result      string // Output text.
	Error       string // If failed.
	StartedAt   time.Time
	CompletedAt *time.Time
	Cancel      context.CancelFunc `json:"-"`
	Usage       canonical.Usage
}

// SubagentManager manages spawned subagent tasks.
type SubagentManager struct {
	mu        sync.RWMutex
	tasks     map[string]*SubagentTask // taskID → task
	config    SubagentConfig
	loopPool  *LoopPool
	onEvent   EventHandler
	globalSem chan struct{} // Global concurrency limit.
}

// NewSubagentManager creates a manager with the given config and loop pool.
func NewSubagentManager(cfg SubagentConfig, pool *LoopPool, onEvent EventHandler) *SubagentManager {
	if cfg.MaxChildrenPerAgent <= 0 {
		cfg.MaxChildrenPerAgent = 5
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 8
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	return &SubagentManager{
		tasks:     make(map[string]*SubagentTask),
		config:    cfg,
		loopPool:  pool,
		onEvent:   onEvent,
		globalSem: make(chan struct{}, cfg.MaxConcurrent),
	}
}

// SetLoopPool sets the loop pool after construction (for deferred wiring).
func (m *SubagentManager) SetLoopPool(pool *LoopPool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loopPool = pool
}

// Spawn creates a subagent task. In sync mode, blocks until completion.
// In async mode, returns the task ID immediately.
func (m *SubagentManager) Spawn(ctx context.Context, parentAgentID, sessionID, task, label, mode string) (string, string, error) {
	if mode == "" {
		mode = "async"
	}

	taskID := uuid.NewString()[:12]
	if label == "" {
		label = taskID
	}

	// Create subagent context — independent of parent (survives parent run end).
	subCtx, cancel := context.WithTimeout(context.Background(), m.config.DefaultTimeout)

	st := &SubagentTask{
		ID:          taskID,
		ParentAgent: parentAgentID,
		SessionID:   sessionID,
		Task:        task,
		Label:       label,
		Mode:        mode,
		Status:      "running",
		StartedAt:   time.Now(),
		Cancel:      cancel,
	}

	// Per-agent limit check + register atomically to prevent race.
	m.mu.Lock()
	count := m.countRunningLocked(parentAgentID, sessionID)
	if count >= m.config.MaxChildrenPerAgent {
		m.mu.Unlock()
		cancel()
		return "", "", fmt.Errorf("per-agent subagent limit reached (%d/%d)", count, m.config.MaxChildrenPerAgent)
	}
	m.tasks[taskID] = st
	m.mu.Unlock()

	if mode == "sync" {
		// Block until completion.
		m.execute(subCtx, st, parentAgentID)
		if st.Status == "failed" {
			return taskID, "", fmt.Errorf("subagent failed: %s", st.Error)
		}
		return taskID, st.Result, nil
	}

	// Async: launch goroutine.
	go m.execute(subCtx, st, parentAgentID)
	return taskID, "", nil
}

// execute runs the subagent task.
func (m *SubagentManager) execute(ctx context.Context, st *SubagentTask, parentAgentID string) {
	// Acquire global semaphore (inside execute so sync mode doesn't hold it across blocking).
	select {
	case m.globalSem <- struct{}{}:
		// Acquired.
	case <-ctx.Done():
		m.mu.Lock()
		st.Status = "failed"
		st.Error = "global subagent limit reached or context cancelled"
		now := time.Now()
		st.CompletedAt = &now
		m.mu.Unlock()
		st.Cancel()
		return
	}

	defer func() {
		<-m.globalSem // Release global slot.
		st.Cancel()   // Clean up context.
	}()

	// Create ephemeral loop with tool deny list.
	if m.loopPool == nil {
		m.mu.Lock()
		st.Status = "failed"
		st.Error = "loop pool not available"
		now := time.Now()
		st.CompletedAt = &now
		m.mu.Unlock()
		st.Cancel()
		return
	}
	loop := m.loopPool.NewTaskLoopWithDeny(parentAgentID, []string{"spawn", "subagents", "delegate", "team_tasks"})
	if loop == nil {
		m.mu.Lock()
		st.Status = "failed"
		st.Error = fmt.Sprintf("agent %q not configured", parentAgentID)
		now := time.Now()
		st.CompletedAt = &now
		m.mu.Unlock()
		st.Cancel()
		log.Printf("[subagent] %s: no loop for agent %s", st.ID, parentAgentID)
		return
	}

	// Run the loop with an ephemeral session ID (for logging).
	// Set ConsentSessionID so consent events route to the parent's channel.
	messages := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: st.Task}}},
	}
	ephemeralSession := fmt.Sprintf("subagent-%s", st.ID)
	if st.SessionID != "" {
		loop.SetConsentSessionID(st.SessionID)
	}
	result := loop.Run(ctx, ephemeralSession, messages)

	m.mu.Lock()
	now := time.Now()
	st.CompletedAt = &now
	st.Usage = result.Usage
	if result.Error != nil {
		st.Status = "failed"
		st.Error = result.Error.Error()
	} else {
		st.Status = "completed"
		st.Result = extractSubagentResponse(result.Messages)
	}
	m.mu.Unlock()

	log.Printf("[subagent] %s (%s) %s for %s", st.ID, st.Label, st.Status, parentAgentID)
	m.onEvent(Event{
		Type:      EventSubagentCompleted,
		AgentID:   parentAgentID,
		SessionID: st.SessionID,
		Text:      st.ID,
	})
}

// ConsumeCompleted returns and removes completed/failed subagent tasks for injection.
func (m *SubagentManager) ConsumeCompleted(parentAgentID, sessionID string) []SubagentTask {
	m.mu.Lock()
	defer m.mu.Unlock()

	var completed []SubagentTask
	for id, t := range m.tasks {
		if t.ParentAgent == parentAgentID && t.SessionID == sessionID &&
			(t.Status == "completed" || t.Status == "failed") {
			completed = append(completed, *t)
			delete(m.tasks, id)
		}
	}
	return completed
}

// List returns all subagent tasks for a parent agent/session (internal use).
func (m *SubagentManager) List(parentAgentID, sessionID string) []SubagentTask {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []SubagentTask
	for _, t := range m.tasks {
		if t.ParentAgent == parentAgentID && t.SessionID == sessionID {
			result = append(result, *t)
		}
	}
	return result
}

// Cancel cancels a specific subagent task.
func (m *SubagentManager) Cancel(taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.tasks[taskID]
	if !ok {
		return fmt.Errorf("subagent %s not found", taskID)
	}
	if t.Status != "running" {
		return nil // Already done.
	}
	t.Cancel()
	t.Status = "cancelled"
	now := time.Now()
	t.CompletedAt = &now
	return nil
}

// CancelAll cancels all subagents for a parent agent. If sessionID is empty,
// cancels all sessions.
func (m *SubagentManager) CancelAll(parentAgentID, sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tasks {
		if t.ParentAgent == parentAgentID && (sessionID == "" || t.SessionID == sessionID) && t.Status == "running" {
			t.Cancel()
			t.Status = "cancelled"
			now := time.Now()
			t.CompletedAt = &now
		}
	}
}

// Cleanup removes completed tasks older than maxAge (prevents memory leak).
func (m *SubagentManager) Cleanup(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, t := range m.tasks {
		if t.CompletedAt != nil && t.CompletedAt.Before(cutoff) {
			delete(m.tasks, id)
		}
	}
}

// Shutdown cancels all running subagents.
func (m *SubagentManager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, t := range m.tasks {
		if t.Status == "running" {
			t.Cancel()
			t.Status = "cancelled"
			now := time.Now()
			t.CompletedAt = &now
		}
	}
}

// countRunningLocked counts running tasks — caller must hold m.mu.
func (m *SubagentManager) countRunningLocked(parentAgentID, sessionID string) int {
	count := 0
	for _, t := range m.tasks {
		if t.ParentAgent == parentAgentID && t.SessionID == sessionID && t.Status == "running" {
			count++
		}
	}
	return count
}

// buildSubagentResultsMessage formats completed subagent results for injection
// into the parent's message history.
func buildSubagentResultsMessage(tasks []SubagentTask) canonical.Message {
	var sb strings.Builder
	sb.WriteString("<subagent-results>\n")
	for _, t := range tasks {
		if t.Status == "completed" {
			fmt.Fprintf(&sb, "<result label=%q status=\"completed\">\n%s\n</result>\n", t.Label, t.Result)
		} else {
			fmt.Fprintf(&sb, "<result label=%q status=%q error=%q></result>\n", t.Label, t.Status, t.Error)
		}
	}
	sb.WriteString("</subagent-results>\nSubagent tasks have completed. Synthesize the results above.")
	return canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: sb.String()}},
	}
}

// extractSubagentResponse extracts assistant text from run result messages.
func extractSubagentResponse(messages []canonical.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			for _, c := range messages[i].Content {
				if c.Type == "text" && c.Text != "" {
					return c.Text
				}
			}
		}
	}
	return ""
}
