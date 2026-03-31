package orchestration

import (
	"context"
	"fmt"
	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/tool"
)

const (
	maxDelegationDepth      = 3
	defaultDelegationTimeout = 300 // 5 minutes default per delegation.
)

type delegationDepthKey struct{}

// Delegator manages agent-to-agent delegation.
// Delegation links are read from the database per-request (DB-authoritative).
type Delegator struct {
	store    store.Store
	configs  map[string]agent.Config
	provider provider.Provider
	router   *provider.Router
	toolReg  *tool.Registry
}

// NewDelegator creates a new delegator.
func NewDelegator(
	s store.Store,
	configs map[string]agent.Config,
	prov provider.Provider,
	router *provider.Router,
	toolReg *tool.Registry,
) *Delegator {
	return &Delegator{
		store:    s,
		configs:  configs,
		provider: prov,
		router:   router,
		toolReg:  toolReg,
	}
}

// Delegate dispatches a task from one agent to another.
func (d *Delegator) Delegate(ctx context.Context, sourceID, targetID, prompt, mode string) (string, string, error) {
	// Check depth.
	depth, _ := ctx.Value(delegationDepthKey{}).(int)
	if depth >= maxDelegationDepth {
		return "", "", fmt.Errorf("max delegation depth (%d) reached", maxDelegationDepth)
	}

	// Find the link (reads from DB per-request).
	link, err := d.findLink(ctx, sourceID, targetID)
	if err != nil {
		return "", "", err
	}

	// Check concurrency.
	count, err := d.store.GetDelegationCount(ctx, link.ID)
	if err != nil {
		return "", "", fmt.Errorf("checking delegation count: %w", err)
	}
	if count >= link.MaxConcurrent {
		return "", "", fmt.Errorf("delegation link %s→%s at capacity (%d/%d)", sourceID, targetID, count, link.MaxConcurrent)
	}

	// Increment.
	if err := d.store.IncrementDelegation(ctx, link.ID); err != nil {
		return "", "", err
	}

	// Record.
	recordID := newID()
	record := store.DelegationRecord{
		ID:        recordID,
		LinkID:    link.ID,
		SourceID:  sourceID,
		TargetID:  targetID,
		Prompt:    prompt,
		Status:    "running",
		StartedAt: time.Now().UTC(),
	}
	d.store.RecordDelegation(ctx, record)

	// Get target agent config.
	targetConfig, ok := d.configs[targetID]
	if !ok {
		d.store.DecrementDelegation(ctx, link.ID)
		return "", "", fmt.Errorf("unknown target agent: %s", targetID)
	}

	if mode == "" {
		mode = link.Direction
	}

	// Apply per-link timeout (or default).
	timeoutSec := link.TimeoutSec
	if timeoutSec <= 0 {
		timeoutSec = defaultDelegationTimeout
	}

	if mode == "sync" {
		delegateCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
		defer cancel()
		result, err := d.runSync(delegateCtx, targetConfig, prompt, depth)
		now := time.Now().UTC()
		if err != nil {
			d.updateRecord(ctx, recordID, link.ID, "failed", err.Error(), &now)
			return recordID, "", err
		}
		d.updateRecord(ctx, recordID, link.ID, "completed", result, &now)
		return recordID, result, nil
	}

	// Async — run in goroutine with timeout.
	go func() {
		asyncCtx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
		defer cancel()
		result, err := d.runSync(asyncCtx, targetConfig, prompt, depth)
		now := time.Now().UTC()
		if err != nil {
			d.updateRecord(context.Background(), recordID, link.ID, "failed", err.Error(), &now)
		} else {
			d.updateRecord(context.Background(), recordID, link.ID, "completed", result, &now)
		}
	}()

	return recordID, "", nil // Async: result comes later.
}

// Status returns the current status of a delegation.
func (d *Delegator) Status(ctx context.Context, delegationID string) (*store.DelegationRecord, error) {
	return d.store.GetDelegationRecord(ctx, delegationID)
}

func (d *Delegator) findLink(ctx context.Context, sourceID, targetID string) (*store.DelegationLink, error) {
	links, err := d.store.GetDelegationLinks(ctx, sourceID)
	if err != nil {
		return nil, fmt.Errorf("querying delegation links: %w", err)
	}
	for i := range links {
		if links[i].TargetID == targetID {
			return &links[i], nil
		}
	}
	return nil, fmt.Errorf("no delegation link from %s to %s", sourceID, targetID)
}

func (d *Delegator) runSync(ctx context.Context, config agent.Config, prompt string, depth int) (string, error) {
	// Create a fresh agent loop for the target.
	var opts []agent.LoopOption
	if d.router != nil {
		opts = append(opts, agent.WithRouter(d.router))
	}

	loop := agent.NewLoop(config, d.provider, d.toolReg, nil, nil, nil, opts...)

	// Inject delegation depth.
	childCtx := context.WithValue(ctx, delegationDepthKey{}, depth+1)

	result := loop.Run(childCtx, "delegation-"+newID()[:8], []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: prompt}}},
	})

	if result.Error != nil {
		return "", result.Error
	}

	// Extract final text.
	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == "assistant" {
			return agent.ExtractText(result.Messages[i]), nil
		}
	}
	return "(no response)", nil
}

func (d *Delegator) updateRecord(ctx context.Context, recordID, linkID, status, result string, completedAt *time.Time) {
	d.store.DecrementDelegation(ctx, linkID)
	// Update the history record via direct SQL (the interface doesn't have an update method).
	d.store.DB().ExecContext(ctx,
		`UPDATE delegation_history SET status = ?, result = ?, completed_at = ? WHERE id = ?`,
		status, result, completedAt.Format(time.RFC3339), recordID)
}

func newID() string {
	return fmt.Sprintf("%x", time.Now().UnixNano())
}
