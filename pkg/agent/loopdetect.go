package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
)

// LoopVerdict is the result of loop detection analysis.
type LoopVerdict int

const (
	LoopOK   LoopVerdict = iota // No loop detected.
	LoopWarn                    // Inject a warning into the next prompt.
	LoopKill                    // Force-exit the agent loop.
)

const loopHistorySize = 30

// toolCallRecord tracks a single tool invocation for loop detection.
type toolCallRecord struct {
	toolName   string
	argsHash   [32]byte
	resultHash [32]byte
}

// ToolLoopState tracks recent tool calls and detects stuck loops.
//
// Three detection strategies (pattern source: GoClaw internal/agent/toolloop.go):
//  1. Identical loop: same tool + same args + same result
//  2. Same-result loop: same tool, different args, identical result
//  3. Read-only streak: long chain of non-mutating calls
type ToolLoopState struct {
	recent     []toolCallRecord // Circular buffer, max loopHistorySize.
	streakLen  int              // Consecutive non-mutating calls.
	streakUniq map[[32]byte]bool // Unique argsHashes in current streak.
}

// NewToolLoopState creates a new loop detector.
func NewToolLoopState() *ToolLoopState {
	return &ToolLoopState{
		streakUniq: make(map[[32]byte]bool),
	}
}

// Record logs a tool call. Call after every tool execution.
func (s *ToolLoopState) Record(name string, args json.RawMessage, result string, mutating bool) {
	argsHash := stableHash(args)
	resultHash := sha256.Sum256([]byte(result))

	s.recent = append(s.recent, toolCallRecord{
		toolName:   name,
		argsHash:   argsHash,
		resultHash: resultHash,
	})
	if len(s.recent) > loopHistorySize {
		s.recent = s.recent[1:]
	}

	if mutating {
		s.streakLen = 0
		s.streakUniq = make(map[[32]byte]bool)
	} else {
		s.streakLen++
		s.streakUniq[argsHash] = true
	}
}

// Check analyzes the most recent tool call for loop patterns.
// Returns a verdict and a human-readable reason (empty if OK).
func (s *ToolLoopState) Check(name string, args json.RawMessage, result string) (LoopVerdict, string) {
	argsHash := stableHash(args)
	resultHash := sha256.Sum256([]byte(result))

	// Strategy 1: Identical loop — same tool, same args, same result.
	identicalCount := 0
	for _, r := range s.recent {
		if r.toolName == name && r.argsHash == argsHash && r.resultHash == resultHash {
			identicalCount++
		}
	}
	if identicalCount >= 5 {
		return LoopKill, fmt.Sprintf("Identical loop: %s called %d times with same args and result. Stopping.", name, identicalCount)
	}
	if identicalCount >= 3 {
		return LoopWarn, fmt.Sprintf("You've called %s %d times with identical results. Try a different approach or tool.", name, identicalCount)
	}

	// Strategy 2: Same-result loop — same tool, different args, identical result.
	sameResultCount := 0
	for _, r := range s.recent {
		if r.toolName == name && r.resultHash == resultHash && r.argsHash != argsHash {
			sameResultCount++
		}
	}
	if sameResultCount >= 6 {
		return LoopKill, fmt.Sprintf("Same-result loop: %s returning identical results for %d different inputs. Stopping.", name, sameResultCount)
	}
	if sameResultCount >= 4 {
		return LoopWarn, fmt.Sprintf("Different inputs to %s are producing the same result. The approach may not be working.", name)
	}

	// Strategy 3: Read-only streak — consecutive non-mutating calls.
	if s.streakLen > 0 {
		uniquenessRatio := float64(len(s.streakUniq)) / float64(s.streakLen)

		if uniquenessRatio > 0.6 {
			// Exploration mode: reading many different things — moderate.
			if s.streakLen >= 36 {
				return LoopKill, "Extended read-only exploration without any action"
			}
			if s.streakLen >= 24 {
				return LoopWarn, "You've been reading without making changes. Consider acting on what you've learned."
			}
		} else {
			// Stuck mode: re-reading the same things — strict.
			if s.streakLen >= 12 {
				return LoopKill, "Stuck in read-only loop, re-reading same content"
			}
			if s.streakLen >= 8 {
				return LoopWarn, "You're re-reading the same content repeatedly. Make a decision and act."
			}
		}
	}

	return LoopOK, ""
}

// mutatingTools lists tools that create or modify state.
var mutatingTools = map[string]bool{
	"write_file":    true,
	"edit_file":     true,
	"create_file":   true,
	"memory_write":  true,
	"memory_delete": true,
	// exec is intentionally excluded — it's ambiguous (could be read-only like "ls"
	// or mutating like "rm"). GoClaw treats it as neutral.
}

// IsMutating returns true if the tool creates or modifies state.
func IsMutating(toolName string) bool {
	return mutatingTools[toolName]
}

// stableHash produces a deterministic hash of JSON by sorting keys.
func stableHash(raw json.RawMessage) [32]byte {
	if len(raw) == 0 {
		return sha256.Sum256(nil)
	}

	// Try to unmarshal as a map for stable key ordering.
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err == nil {
		stable := sortedJSON(obj)
		return sha256.Sum256([]byte(stable))
	}

	// Not an object — hash raw bytes.
	return sha256.Sum256(raw)
}

// sortedJSON produces a deterministic JSON string with sorted keys.
func sortedJSON(v any) string {
	switch val := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		result := "{"
		for i, k := range keys {
			if i > 0 {
				result += ","
			}
			keyJSON, _ := json.Marshal(k)
			result += string(keyJSON) + ":" + sortedJSON(val[k])
		}
		return result + "}"
	case []any:
		result := "["
		for i, elem := range val {
			if i > 0 {
				result += ","
			}
			result += sortedJSON(elem)
		}
		return result + "]"
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}
