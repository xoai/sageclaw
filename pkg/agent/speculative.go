package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

const speculativeCacheTTL = 30 * time.Second

// SpeculativePattern defines an anticipated tool call based on a trigger.
type SpeculativePattern struct {
	TriggerTool     string
	AnticipatedTool string
	DeriveInput     func(triggerInput json.RawMessage) json.RawMessage
}

type speculativeResult struct {
	Result    *canonical.ToolResult
	CreatedAt time.Time
	Hit       bool // True if the LLM actually requested this.
}

// SpeculativeEngine pre-executes anticipated tool calls based on known
// patterns. Results are cached and used if the LLM confirms the anticipated
// call within the TTL.
type SpeculativeEngine struct {
	registry *tool.Registry
	patterns []SpeculativePattern
	mu       sync.Mutex
	cache    map[string]*speculativeResult // key: toolName+inputHash
}

// NewSpeculativeEngine creates an engine with built-in patterns.
func NewSpeculativeEngine(registry *tool.Registry) *SpeculativeEngine {
	return &SpeculativeEngine{
		registry: registry,
		cache:    make(map[string]*speculativeResult),
		patterns: []SpeculativePattern{
			{
				TriggerTool:     "edit",
				AnticipatedTool: "read_file",
				DeriveInput:     derivePathInput,
			},
			{
				TriggerTool:     "write_file",
				AnticipatedTool: "read_file",
				DeriveInput:     derivePathInput,
			},
		},
	}
}

// derivePathInput extracts the "path" field from trigger input and wraps it
// for read_file (which expects {"path": "..."}).
func derivePathInput(triggerInput json.RawMessage) json.RawMessage {
	var params struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(triggerInput, &params); err != nil || params.Path == "" {
		return nil
	}
	derived, _ := json.Marshal(map[string]string{"path": params.Path})
	return derived
}

// OnToolResult checks patterns after each tool result and fires anticipated
// calls in the background.
func (se *SpeculativeEngine) OnToolResult(
	tc canonical.ToolCall,
	result *canonical.ToolResult,
	registry *tool.Registry,
	execute func(name string, input json.RawMessage) (*canonical.ToolResult, error),
) {
	for _, p := range se.patterns {
		if tc.Name != p.TriggerTool {
			continue
		}

		derivedInput := p.DeriveInput(tc.Input)
		if derivedInput == nil {
			continue
		}

		// Eligibility: anticipated tool must be concurrent-safe.
		if !registry.IsConcurrencySafe(p.AnticipatedTool) {
			continue
		}
		_, risk, _, ok := registry.GetMeta(p.AnticipatedTool)
		if !ok || (risk != tool.RiskSafe && risk != tool.RiskModerate) {
			continue
		}

		key := cacheKey(p.AnticipatedTool, derivedInput)

		// Don't re-speculate if already cached.
		se.mu.Lock()
		if _, exists := se.cache[key]; exists {
			se.mu.Unlock()
			continue
		}
		// Reserve the slot.
		se.cache[key] = nil
		se.mu.Unlock()

		// Fire speculative execution in background.
		go func(anticipated string, input json.RawMessage, cacheKey string) {
			res, err := execute(anticipated, input)
			if err != nil {
				se.mu.Lock()
				delete(se.cache, cacheKey) // Remove failed reservation.
				se.mu.Unlock()
				return
			}
			se.mu.Lock()
			se.cache[cacheKey] = &speculativeResult{
				Result:    res,
				CreatedAt: time.Now(),
			}
			se.mu.Unlock()
		}(p.AnticipatedTool, derivedInput, key)
	}
}

// CheckCache returns a cached speculative result if available and fresh.
// Returns nil if no cache hit.
func (se *SpeculativeEngine) CheckCache(tc canonical.ToolCall) *canonical.ToolResult {
	key := cacheKey(tc.Name, tc.Input)
	se.mu.Lock()
	defer se.mu.Unlock()

	sr, ok := se.cache[key]
	if !ok || sr == nil {
		return nil
	}

	// TTL check.
	if time.Since(sr.CreatedAt) > speculativeCacheTTL {
		delete(se.cache, key)
		return nil
	}

	// Double-check eligibility at cache hit time.
	if !se.registry.IsConcurrencySafe(tc.Name) {
		delete(se.cache, key)
		return nil
	}

	sr.Hit = true
	result := *sr.Result // Copy.
	result.ToolCallID = tc.ID
	delete(se.cache, key) // One-shot: remove after use.
	return &result
}

// Cleanup removes expired entries from the cache.
func (se *SpeculativeEngine) Cleanup() {
	se.mu.Lock()
	defer se.mu.Unlock()
	for key, sr := range se.cache {
		if sr == nil || time.Since(sr.CreatedAt) > speculativeCacheTTL {
			delete(se.cache, key)
		}
	}
}

func cacheKey(toolName string, input json.RawMessage) string {
	h := sha256.Sum256(input)
	return fmt.Sprintf("%s:%x", toolName, h[:8])
}
