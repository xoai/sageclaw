package orchestration

import (
	"context"
	"fmt"
	"testing"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

type evalMockProvider struct {
	callCount int
	responses []string
}

func (m *evalMockProvider) Name() string { return "mock" }
func (m *evalMockProvider) Chat(ctx context.Context, req *canonical.Request) (*canonical.Response, error) {
	if m.callCount >= len(m.responses) {
		return &canonical.Response{
			StopReason: "end_turn",
			Messages:   []canonical.Message{{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "fallback"}}}},
		}, nil
	}
	resp := m.responses[m.callCount]
	m.callCount++
	return &canonical.Response{
		StopReason: "end_turn",
		Messages:   []canonical.Message{{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: resp}}}},
	}, nil
}
func (m *evalMockProvider) ChatStream(ctx context.Context, req *canonical.Request) (<-chan provider.StreamEvent, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestEvalLoop_ConvergesInOneRound(t *testing.T) {
	prov := &evalMockProvider{
		responses: []string{
			"Here is my well-crafted response about AI safety.", // Generator round 1
			"0.9\nExcellent response. Comprehensive and well-structured.", // Evaluator round 1
		},
	}

	el := NewEvalLoop(
		agent.Config{AgentID: "gen", SystemPrompt: "Generate.", Model: "mock"},
		agent.Config{AgentID: "eval", SystemPrompt: "Evaluate.", Model: "mock"},
		prov, nil, tool.NewRegistry(), 3, 0.7,
	)

	result, err := el.Run(context.Background(), "Write about AI safety")
	if err != nil {
		t.Fatalf("eval loop: %v", err)
	}
	if result.Rounds != 1 {
		t.Fatalf("expected 1 round, got %d", result.Rounds)
	}
	if result.Score < 0.7 {
		t.Fatalf("expected score >= 0.7, got %.2f", result.Score)
	}
	if result.FinalOutput == "" {
		t.Fatal("expected output")
	}
}

func TestEvalLoop_ImprovesOverRounds(t *testing.T) {
	prov := &evalMockProvider{
		responses: []string{
			"First attempt — basic response.",                         // Gen round 1
			"0.4\nToo shallow. Needs more depth and examples.",       // Eval round 1
			"Improved response with depth, examples, and citations.", // Gen round 2
			"0.85\nMuch better. Good depth and examples.",            // Eval round 2
		},
	}

	el := NewEvalLoop(
		agent.Config{AgentID: "gen", Model: "mock"},
		agent.Config{AgentID: "eval", Model: "mock"},
		prov, nil, tool.NewRegistry(), 3, 0.7,
	)

	result, err := el.Run(context.Background(), "Write a detailed analysis")
	if err != nil {
		t.Fatalf("eval loop: %v", err)
	}
	if result.Rounds != 2 {
		t.Fatalf("expected 2 rounds, got %d", result.Rounds)
	}
	if len(result.Feedback) != 2 {
		t.Fatalf("expected 2 feedback entries, got %d", len(result.Feedback))
	}
}

func TestEvalLoop_MaxRounds(t *testing.T) {
	// Provider always gives low scores.
	responses := make([]string, 10)
	for i := 0; i < 10; i += 2 {
		responses[i] = fmt.Sprintf("Attempt %d", i/2+1)
		responses[i+1] = "0.2\nStill not good enough."
	}

	prov := &evalMockProvider{responses: responses}

	el := NewEvalLoop(
		agent.Config{AgentID: "gen", Model: "mock"},
		agent.Config{AgentID: "eval", Model: "mock"},
		prov, nil, tool.NewRegistry(), 3, 0.9,
	)

	result, err := el.Run(context.Background(), "Write something perfect")
	if err != nil {
		t.Fatalf("eval loop: %v", err)
	}
	if result.Rounds != 3 {
		t.Fatalf("expected 3 rounds (max), got %d", result.Rounds)
	}
}
