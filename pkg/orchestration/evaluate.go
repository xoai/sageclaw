package orchestration

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

// EvalLoop runs a generator-evaluator iteration cycle.
type EvalLoop struct {
	generatorConfig agent.Config
	evaluatorConfig agent.Config
	prov            provider.Provider
	router          *provider.Router
	toolReg         *tool.Registry
	maxRounds       int
	threshold       float64
}

// NewEvalLoop creates an evaluate loop.
func NewEvalLoop(
	generatorConfig, evaluatorConfig agent.Config,
	prov provider.Provider,
	router *provider.Router,
	toolReg *tool.Registry,
	maxRounds int,
	threshold float64,
) *EvalLoop {
	if maxRounds <= 0 {
		maxRounds = 3
	}
	if threshold <= 0 {
		threshold = 0.7
	}
	return &EvalLoop{
		generatorConfig: generatorConfig,
		evaluatorConfig: evaluatorConfig,
		prov:            prov,
		router:          router,
		toolReg:         toolReg,
		maxRounds:       maxRounds,
		threshold:       threshold,
	}
}

// EvalResult holds the outcome of an evaluate loop.
type EvalResult struct {
	FinalOutput string
	Score       float64
	Rounds      int
	Feedback    []string
}

// Run executes the generator-evaluator cycle.
func (el *EvalLoop) Run(ctx context.Context, prompt string) (*EvalResult, error) {
	var lastOutput string
	var lastScore float64
	var allFeedback []string

	for round := 0; round < el.maxRounds; round++ {
		// Build generator prompt.
		genPrompt := prompt
		if round > 0 && len(allFeedback) > 0 {
			genPrompt += "\n\nPrevious attempt feedback:\n" + allFeedback[len(allFeedback)-1]
			genPrompt += "\n\nPlease improve your response based on this feedback."
		}

		// Run generator.
		genOutput, err := el.runAgent(ctx, el.generatorConfig, genPrompt)
		if err != nil {
			return nil, fmt.Errorf("generator round %d: %w", round, err)
		}
		lastOutput = genOutput

		// Run evaluator.
		evalPrompt := fmt.Sprintf(`Evaluate the following response to the prompt: "%s"

Response to evaluate:
%s

Score this response from 0.0 to 1.0 (where 1.0 is perfect).
First line of your response must be just the numeric score.
Then provide specific feedback for improvement.`, prompt, genOutput)

		evalOutput, err := el.runAgent(ctx, el.evaluatorConfig, evalPrompt)
		if err != nil {
			return nil, fmt.Errorf("evaluator round %d: %w", round, err)
		}

		// Parse score from first line.
		score, feedback := parseEvalOutput(evalOutput)
		lastScore = score
		allFeedback = append(allFeedback, feedback)

		if score >= el.threshold {
			return &EvalResult{
				FinalOutput: lastOutput,
				Score:       score,
				Rounds:      round + 1,
				Feedback:    allFeedback,
			}, nil
		}
	}

	// Max rounds reached — return best attempt.
	return &EvalResult{
		FinalOutput: lastOutput,
		Score:       lastScore,
		Rounds:      el.maxRounds,
		Feedback:    allFeedback,
	}, nil
}

func (el *EvalLoop) runAgent(ctx context.Context, config agent.Config, prompt string) (string, error) {
	var opts []agent.LoopOption
	if el.router != nil {
		opts = append(opts, agent.WithRouter(el.router))
	}

	loop := agent.NewLoop(config, el.prov, el.toolReg, nil, nil, nil, opts...)
	result := loop.Run(ctx, "eval-"+newID()[:8], []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: prompt}}},
	})

	if result.Error != nil {
		return "", result.Error
	}

	for i := len(result.Messages) - 1; i >= 0; i-- {
		if result.Messages[i].Role == "assistant" {
			return agent.ExtractText(result.Messages[i]), nil
		}
	}
	return "", fmt.Errorf("no response from agent")
}

func parseEvalOutput(output string) (float64, string) {
	lines := strings.SplitN(output, "\n", 2)
	if len(lines) == 0 {
		return 0, output
	}

	score, err := strconv.ParseFloat(strings.TrimSpace(lines[0]), 64)
	if err != nil {
		// Try to find a score pattern like "0.8" or "Score: 0.8"
		score = 0.5 // Default if unparseable.
	}

	feedback := output
	if len(lines) > 1 {
		feedback = strings.TrimSpace(lines[1])
	}

	return score, feedback
}
