package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// EvalFunc runs a generator-evaluator loop.
type EvalFunc func(ctx context.Context, prompt string, maxRounds int, threshold float64) (result string, score float64, rounds int, err error)

// RegisterEvaluate registers the evaluate tool.
func RegisterEvaluate(reg *Registry, evalFn EvalFunc) {
	reg.Register("evaluate", "Run a generator-evaluator loop for quality output",
		json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string","description":"Task prompt"},"max_rounds":{"type":"integer","description":"Max improvement rounds (default 3)","default":3},"threshold":{"type":"number","description":"Quality threshold 0-1 (default 0.7)","default":0.7}},"required":["prompt"]}`),
		evaluateTool(evalFn))
}

func evaluateTool(evalFn EvalFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var p struct {
			Prompt    string  `json:"prompt"`
			MaxRounds int     `json:"max_rounds"`
			Threshold float64 `json:"threshold"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		result, score, rounds, err := evalFn(ctx, p.Prompt, p.MaxRounds, p.Threshold)
		if err != nil {
			return errorResult("evaluate failed: " + err.Error()), nil
		}

		return &canonical.ToolResult{
			Content: fmt.Sprintf("Evaluate loop completed in %d round(s) (score: %.2f)\n\n%s", rounds, score, result),
		}, nil
	}
}
