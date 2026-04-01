package agent

import (
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func makeTextMsg(role, text string) canonical.Message {
	return canonical.Message{
		Role:    role,
		Content: []canonical.Content{{Type: "text", Text: text}},
	}
}

func TestContextBudget_NewDefaults(t *testing.T) {
	b := NewContextBudget(200000, 8192)
	if b.contextWindow != 200000 {
		t.Errorf("expected context window 200000, got %d", b.contextWindow)
	}
	if b.calibrated {
		t.Error("expected not calibrated initially")
	}
	if b.historyBudget <= 0 {
		t.Error("expected positive history budget before calibration")
	}
}

func TestContextBudget_NewZeroContextWindow(t *testing.T) {
	b := NewContextBudget(0, 8192)
	if b.contextWindow != 200000 {
		t.Errorf("expected default 200000 for zero context window, got %d", b.contextWindow)
	}
}

func TestContextBudget_Calibrate(t *testing.T) {
	b := NewContextBudget(200000, 8192)

	history := []canonical.Message{
		makeTextMsg("user", "Hello, how are you?"),
		makeTextMsg("assistant", "I'm doing well, thanks for asking!"),
	}

	// Simulate: LLM reports 5000 input tokens.
	// History is small (~20 tokens), so overhead should be ~4980.
	b.Calibrate(5000, history)

	if !b.calibrated {
		t.Error("expected calibrated after Calibrate()")
	}

	// Overhead should be roughly 5000 - (small history).
	if b.overheadTokens < 4000 || b.overheadTokens > 5000 {
		t.Errorf("overhead %d seems wrong for 5000 input tokens with small history", b.overheadTokens)
	}

	// History budget should be capped at 25000 (sensible default).
	if b.historyBudget > 25000 {
		t.Errorf("historyBudget %d should be capped at 25000", b.historyBudget)
	}
}

func TestContextBudget_CalibrateOnlyOnce(t *testing.T) {
	b := NewContextBudget(200000, 8192)
	history := []canonical.Message{makeTextMsg("user", "hi")}

	b.Calibrate(5000, history)
	firstOverhead := b.overheadTokens

	b.Calibrate(10000, history) // Second call should be no-op.
	if b.overheadTokens != firstOverhead {
		t.Errorf("calibration changed on second call: %d → %d", firstOverhead, b.overheadTokens)
	}
}

func TestContextBudget_CalibrateSkipsInvalid(t *testing.T) {
	b := NewContextBudget(200000, 8192)
	history := []canonical.Message{makeTextMsg("user", "hi")}

	b.Calibrate(0, history)
	if b.calibrated {
		t.Error("should not calibrate with 0 input tokens")
	}

	b.Calibrate(-1, history)
	if b.calibrated {
		t.Error("should not calibrate with negative input tokens")
	}
}

func TestContextBudget_Usage(t *testing.T) {
	b := NewContextBudget(200000, 8192)
	history := []canonical.Message{makeTextMsg("user", "hi")}

	usage := b.Usage(history)
	if usage < 0 || usage > 1 {
		t.Errorf("usage with tiny history should be near 0, got %f", usage)
	}
}

func TestContextBudget_UsageZeroBudget(t *testing.T) {
	b := &ContextBudget{historyBudget: 0}
	history := []canonical.Message{makeTextMsg("user", "hi")}

	usage := b.Usage(history)
	if usage != 1.0 {
		t.Errorf("expected 1.0 for zero budget, got %f", usage)
	}
}

func TestContextBudget_CalibrateNegativeOverhead(t *testing.T) {
	b := NewContextBudget(200000, 8192)
	// Simulate: inputTokens < estimated history (shouldn't happen, but be safe).
	history := []canonical.Message{makeTextMsg("user", "hi")}
	b.Calibrate(1, history)

	if b.overheadTokens < 0 {
		t.Error("overhead should never be negative")
	}
	if b.historyBudget < 1000 {
		t.Errorf("history budget should be at least 1000, got %d", b.historyBudget)
	}
}
