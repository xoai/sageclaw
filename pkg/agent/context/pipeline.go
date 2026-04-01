package context

import (
	"context"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// PipelineConfig controls which layers are active and their thresholds.
type PipelineConfig struct {
	// Layer 1: Aggregate budget.
	AggregateBudgetChars int    // Max total chars per tool_result message (default: 20000).
	OverflowDir          string // Directory for overflow files.

	// Layer 2: Snip.
	SnipAgeIterations int  // Min iteration age before snipping (default: 8).
	SnipEnabled       bool // Default: true.

	// Layer 3: Micro-compact.
	MicroCompactAge     int  // Min iteration age for compression (default: 5).
	MicroCompactEnabled bool // Default: true.

	// Layer 4: Collapse.
	CollapseEnabled   bool    // Default: true.
	CollapseThreshold float64 // Budget usage ratio to trigger collapse (default: 0.7).

	// Overflow quota.
	OverflowMaxBytes int64 // Per-session overflow disk cap in bytes (default: 50MB).
}

// DefaultPipelineConfig returns sensible defaults for the context pipeline.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		AggregateBudgetChars: DefaultAggregateBudgetChars,
		SnipAgeIterations:    8,
		SnipEnabled:          true,
		MicroCompactAge:      5,
		MicroCompactEnabled:  true,
		CollapseEnabled:      true,
		CollapseThreshold:    0.7,
		OverflowMaxBytes:     50 * 1024 * 1024, // 50 MB
	}
}

// ContextPipeline orchestrates the multi-layer compaction pipeline.
type ContextPipeline struct {
	config        PipelineConfig
	overflowMgr   *OverflowManager
	llmCaller     LLMCaller      // Fast-tier model for micro-compact + summaries (may be nil).
	collapseStore *CollapseStore  // Summary storage for collapsed ranges.
	budgetTokens  int             // Token budget for collapse threshold (set after calibration).
}

// NewContextPipeline creates a new pipeline. Call SetLLMCaller after
// construction to enable LLM-powered layers (micro-compact, summaries).
func NewContextPipeline(config PipelineConfig) *ContextPipeline {
	var om *OverflowManager
	if config.OverflowDir != "" {
		om = NewOverflowManager(config.OverflowDir)
	}
	return &ContextPipeline{
		config:        config,
		overflowMgr:   om,
		collapseStore: NewCollapseStore(),
	}
}

// SetLLMCaller sets the fast-tier LLM caller for utility calls.
// Must be called before Prepare() for micro-compact to work.
func (p *ContextPipeline) SetLLMCaller(caller LLMCaller) {
	p.llmCaller = caller
}

// LLMCaller returns the pipeline's fast-tier LLM caller (may be nil).
func (p *ContextPipeline) LLMCaller() LLMCaller {
	return p.llmCaller
}

// SetBudgetTokens sets the token budget used for collapse threshold checks.
// Should be called after ContextBudget calibration with budget.HistoryBudget().
func (p *ContextPipeline) SetBudgetTokens(tokens int) {
	p.budgetTokens = tokens
}

// CollapseStore returns the pipeline's collapse store for external invalidation.
func (p *ContextPipeline) CollapseStore() *CollapseStore {
	return p.collapseStore
}

// OverflowManager returns the pipeline's overflow manager (may be nil).
func (p *ContextPipeline) OverflowManager() *OverflowManager {
	return p.overflowMgr
}

// Prepare runs the full pipeline on message history before an LLM call.
// Returns the view to send to the LLM. The original history slice elements
// are not mutated by content-replacing layers — annotations on existing
// messages ARE shared (pointer) so annotation writes persist across calls.
func (p *ContextPipeline) Prepare(
	sessionID string,
	history []canonical.Message,
	iteration int,
) []canonical.Message {
	return p.PrepareWithContext(context.Background(), sessionID, history, iteration)
}

// Pressure thresholds for gating LLM-involving layers.
// Below contentClearThreshold: no action beyond cheap layers.
// At contentClearThreshold: replace aged tool results with placeholders (no LLM).
// At llmCompactThreshold: LLM micro-compact + collapse activate.
const (
	contentClearThreshold = 0.5
	llmCompactThreshold   = 0.7
)

// PrepareWithContext is like Prepare but accepts a context for LLM calls.
func (p *ContextPipeline) PrepareWithContext(
	ctx context.Context,
	sessionID string,
	history []canonical.Message,
	iteration int,
) []canonical.Message {
	// Initialize annotations on the ORIGINAL history first, so the pointers
	// are shared between original and view (spec requirement: annotation
	// writes persist across calls).
	AnnotateIteration(history, iteration)
	for i := range history {
		EstimateTokens(&history[i])
	}

	// Shallow copy so content-replacing layers don't mutate the caller's slice.
	// Annotations are now shared pointers between history and view.
	view := make([]canonical.Message, len(history))
	copy(view, history)

	// --- Phase 1: Cheap layers (always run, no LLM) ---

	// Layer 1: Aggregate budget.
	view = applyAggregateBudget(sessionID, view, p.overflowMgr, p.config.AggregateBudgetChars)

	// Enforce overflow disk quota after writes.
	if p.overflowMgr != nil && p.config.OverflowMaxBytes > 0 {
		p.overflowMgr.EnforceQuota(sessionID, p.config.OverflowMaxBytes, history)
	}

	// Layer 2: Snip — remove aged read-only tool results.
	// Passes llmCaller for lazy summary generation (only for results being snipped).
	if p.config.SnipEnabled {
		view = applySnip(ctx, view, iteration, p.config.SnipAgeIterations, 3, p.llmCaller)
	}

	// --- Phase 2: Compute pressure AFTER cheap layers reduced the view ---
	pressure := computePressure(view, p.budgetTokens)

	// --- Phase 3: LLM-involving layers, gated by pressure ---

	// Content-clear: replace aged tool results with placeholders (no LLM).
	// Activates at moderate pressure (≥ 0.5, < 0.7).
	if pressure >= contentClearThreshold && pressure < llmCompactThreshold {
		if p.config.MicroCompactEnabled {
			view = applyContentClear(view, iteration, p.config.MicroCompactAge)
		}
	}

	// LLM micro-compact + collapse: activate at high pressure (≥ 0.7).
	if pressure >= llmCompactThreshold {
		if p.config.MicroCompactEnabled && p.llmCaller != nil {
			view = applyMicroCompact(ctx, view, iteration, p.config.MicroCompactAge, p.llmCaller)
		}
		if p.config.CollapseEnabled && p.budgetTokens > 0 {
			view = applyCollapse(ctx, sessionID, view, p.collapseStore, p.llmCaller,
				p.config.CollapseThreshold, p.budgetTokens)
		}
	}

	// Final: sanitize preserving annotations.
	view = SanitizePreservingAnnotations(view)

	return view
}

// computePressure returns the ratio of estimated tokens to budget.
// Returns 0.0 if budget is not calibrated (budgetTokens <= 0).
func computePressure(view []canonical.Message, budgetTokens int) float64 {
	if budgetTokens <= 0 {
		return 0.0 // Not calibrated yet — skip pressure-gated layers.
	}
	totalTokens := 0
	for i := range view {
		totalTokens += EstimateTokens(&view[i])
	}
	return float64(totalTokens) / float64(budgetTokens)
}

// SanitizePreservingAnnotations repairs orphaned tool_use/tool_result pairs
// like the standard sanitize, but preserves Annotations when copying messages.
func SanitizePreservingAnnotations(msgs []canonical.Message) []canonical.Message {
	// Build sets of all tool_use IDs and tool_result IDs.
	toolUseIDs := make(map[string]bool)
	toolResultIDs := make(map[string]bool)

	for _, msg := range msgs {
		for _, content := range msg.Content {
			if content.ToolCall != nil {
				toolUseIDs[content.ToolCall.ID] = true
			}
			if content.ToolResult != nil {
				toolResultIDs[content.ToolResult.ToolCallID] = true
			}
		}
	}

	var result []canonical.Message
	for _, msg := range msgs {
		var filteredContent []canonical.Content
		for _, content := range msg.Content {
			if content.ToolResult != nil && !toolUseIDs[content.ToolResult.ToolCallID] {
				continue // Orphaned tool_result — skip.
			}
			filteredContent = append(filteredContent, content)
		}

		if len(filteredContent) == 0 {
			continue
		}

		result = append(result, canonical.Message{
			Role:        msg.Role,
			Content:     filteredContent,
			Annotations: msg.Annotations, // Preserve annotations.
		})
	}

	// Pass 2: Collect orphaned tool_use IDs (no matching tool_result).
	var orphanedIDs []string
	orphanAfterMsg := make(map[int][]string) // msg index -> orphaned call IDs
	for i, msg := range result {
		for _, content := range msg.Content {
			if content.ToolCall != nil && !toolResultIDs[content.ToolCall.ID] {
				orphanedIDs = append(orphanedIDs, content.ToolCall.ID)
				orphanAfterMsg[i] = append(orphanAfterMsg[i], content.ToolCall.ID)
			}
		}
	}

	if len(orphanedIDs) == 0 {
		return result
	}

	// Pass 3: Build final slice with synthetic results inserted after orphan messages.
	final := make([]canonical.Message, 0, len(result)+len(orphanedIDs))
	for i, msg := range result {
		final = append(final, msg)
		for _, callID := range orphanAfterMsg[i] {
			final = append(final, canonical.Message{
				Role: "user",
				Content: []canonical.Content{{
					Type: "tool_result",
					ToolResult: &canonical.ToolResult{
						ToolCallID: callID,
						Content:    "[Result unavailable — message was pruned]",
					},
				}},
			})
		}
	}

	return final
}
