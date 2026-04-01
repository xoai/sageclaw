package tool

import (
	"context"
	"strings"
	"testing"
)

// --- adaptiveMax tests ---

func TestAdaptiveMax_NoIteration(t *testing.T) {
	ctx := context.Background()
	got := adaptiveMax(ctx, 30000)
	if got != 30000 {
		t.Errorf("no iteration context: expected 30000, got %d", got)
	}
}

func TestAdaptiveMax_EarlyIteration(t *testing.T) {
	ctx := WithIteration(context.Background(), IterationInfo{Current: 2, Max: 10})
	got := adaptiveMax(ctx, 30000)
	if got != 30000 {
		t.Errorf("20%% iteration: expected 30000, got %d", got)
	}
}

func TestAdaptiveMax_MidIteration(t *testing.T) {
	ctx := WithIteration(context.Background(), IterationInfo{Current: 5, Max: 10})
	got := adaptiveMax(ctx, 30000)
	if got != 15000 {
		t.Errorf("50%% iteration: expected 15000, got %d", got)
	}
}

func TestAdaptiveMax_LateIteration(t *testing.T) {
	ctx := WithIteration(context.Background(), IterationInfo{Current: 8, Max: 10})
	got := adaptiveMax(ctx, 30000)
	if got != 7500 {
		t.Errorf("80%% iteration: expected 7500, got %d", got)
	}
}

func TestAdaptiveMax_Floor(t *testing.T) {
	ctx := WithIteration(context.Background(), IterationInfo{Current: 9, Max: 10})
	got := adaptiveMax(ctx, 4000)
	if got != 2000 {
		t.Errorf("small default at 90%%: expected floor 2000, got %d", got)
	}
}

func TestAdaptiveMax_Proportional(t *testing.T) {
	// Verify proportional behavior: 50K default should give 25K at 50%, 12.5K at 75%.
	ctx50 := WithIteration(context.Background(), IterationInfo{Current: 5, Max: 10})
	ctx75 := WithIteration(context.Background(), IterationInfo{Current: 8, Max: 10})
	if got := adaptiveMax(ctx50, 50000); got != 25000 {
		t.Errorf("50K at 50%%: expected 25000, got %d", got)
	}
	if got := adaptiveMax(ctx75, 50000); got != 12500 {
		t.Errorf("50K at 80%%: expected 12500, got %d", got)
	}
}

// --- capOutput tests ---

func TestCapOutput_FitsWithinBudget(t *testing.T) {
	text := "short text"
	got := capOutput(text, 100)
	if got != text {
		t.Errorf("expected unchanged text, got: %s", got)
	}
}

func TestCapOutput_TruncatesAtNewline(t *testing.T) {
	text := "line1\nline2\nline3\nline4\nline5"
	got := capOutput(text, 18) // cuts somewhere in line3
	if strings.Contains(got, "line4") {
		t.Error("should not contain line4")
	}
	if !strings.Contains(got, "line1") {
		t.Error("should contain line1")
	}
	if !strings.Contains(got, "[truncated:") {
		t.Error("should contain truncation notice")
	}
}

func TestCapOutput_EmptyString(t *testing.T) {
	got := capOutput("", 100)
	if got != "" {
		t.Errorf("expected empty, got: %s", got)
	}
}

// --- capOutputHeadTail tests ---

func TestCapOutputHeadTail_FitsWithinBudget(t *testing.T) {
	text := "short output"
	got := capOutputHeadTail(text, 100)
	if got != text {
		t.Errorf("expected unchanged, got: %s", got)
	}
}

func TestCapOutputHeadTail_NoKeywords_HeadOnly(t *testing.T) {
	// Generate text with no keywords in the tail.
	text := strings.Repeat("normal line of output\n", 200)
	got := capOutputHeadTail(text, 500)
	if strings.Contains(got, "head + tail") {
		t.Error("should NOT use head+tail when no keywords in tail")
	}
	if !strings.Contains(got, "[truncated:") {
		t.Error("should contain truncation notice")
	}
}

func TestCapOutputHeadTail_WithKeywords_SplitOutput(t *testing.T) {
	// Build output where tail contains "FAIL".
	head := strings.Repeat("test output line\n", 100)
	tail := "\n--- FAIL: TestSomething\n    expected 1, got 2\nFAIL\n"
	text := head + tail

	got := capOutputHeadTail(text, 500)
	if !strings.Contains(got, "head + tail") {
		t.Error("should use head+tail split when tail has keywords")
	}
	if !strings.Contains(got, "FAIL") {
		t.Error("tail with FAIL keyword should be preserved")
	}
	if !strings.Contains(got, "test output line") {
		t.Error("head should be preserved")
	}
}

func TestCapOutputHeadTail_ErrorInTail(t *testing.T) {
	head := strings.Repeat("building package...\n", 100)
	tail := "\npanic: runtime error: index out of range\ngoroutine 1 [running]:\nmain.main()\n"
	text := head + tail

	got := capOutputHeadTail(text, 600)
	if !strings.Contains(got, "panic") {
		t.Error("should preserve panic in tail")
	}
}
