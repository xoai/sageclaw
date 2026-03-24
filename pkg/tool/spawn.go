package tool

import (
	"context"
	"encoding/json"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const maxSpawnDepth = 2

type spawnDepthKey struct{}

// RegisterSpawn registers the spawn subagent tool.
func RegisterSpawn(reg *Registry) {
	reg.RegisterWithGroup("spawn", "Spawn a subagent to handle a subtask",
		json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Task prompt for the subagent"}},"required":["prompt"]}`),
		GroupOrchestration, RiskSensitive, "builtin", spawnTool())
}

func spawnTool() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Prompt string `json:"prompt"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// Check recursion depth.
		depth, _ := ctx.Value(spawnDepthKey{}).(int)
		if depth >= maxSpawnDepth {
			return errorResult("max spawn depth reached (2)"), nil
		}

		// v0.1: spawn is a placeholder — actual scheduling happens in the pipeline.
		// The agent loop will check for spawn tool calls and route them to the subagent lane.
		return &canonical.ToolResult{Content: "Subagent spawned for: " + params.Prompt + "\n[Note: subagent execution is handled by the pipeline scheduler]"}, nil
	}
}

// WithSpawnDepth adds spawn depth to context.
func WithSpawnDepth(ctx context.Context, depth int) context.Context {
	return context.WithValue(ctx, spawnDepthKey{}, depth)
}

// SpawnDepth returns the current spawn depth from context.
func SpawnDepth(ctx context.Context) int {
	depth, _ := ctx.Value(spawnDepthKey{}).(int)
	return depth
}
