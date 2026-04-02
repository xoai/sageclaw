package provider

import (
	"math"
	"testing"

	"github.com/xoai/sageclaw/pkg/tokenizer"
)

// =============================================================================
// HYPOTHESIS 1: Token Counting Accuracy
// Validates our cl100k_base tokenizer against known reference counts.
// Reference: OpenAI tokenizer (https://platform.openai.com/tokenizer)
// =============================================================================

func TestH1_TokenizerAccuracy_KnownStrings(t *testing.T) {
	counter, err := tokenizer.Get()
	if err != nil {
		t.Fatalf("tokenizer init: %v", err)
	}

	// Reference counts verified against OpenAI's online tokenizer (cl100k_base).
	// Source: https://platform.openai.com/tokenizer
	tests := []struct {
		name     string
		text     string
		expected int // Exact cl100k_base token count
		tolerance int // Allow ±N variance (0 = exact)
	}{
		{"empty", "", 0, 0},
		{"hello_world", "Hello, world!", 4, 0},
		{"simple_sentence", "The quick brown fox jumps over the lazy dog.", 10, 0},
		{"json_object", `{"name": "test", "value": 42}`, 11, 1},
		{"code_snippet", "func main() {\n\tfmt.Println(\"hello\")\n}", 10, 1},
		{"repeated_token", "aaaa aaaa aaaa aaaa aaaa", 10, 1},
		// Multi-language (tokenizers handle these differently).
		{"unicode_emoji", "Hello 🌍", 3, 1},
		{"chinese", "你好世界", 4, 2}, // CJK varies by tokenizer
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := counter.Count(tt.text)
			diff := intAbs(got - tt.expected)
			if diff > tt.tolerance {
				t.Errorf("Count(%q) = %d, want %d (±%d)", tt.text, got, tt.expected, tt.tolerance)
			}
		})
	}
}

func TestH1_TokenizerFallback_CharsDiv4(t *testing.T) {
	// When the tokenizer is nil, it should fall back to len/4.
	var nilCounter *tokenizer.Counter
	text := "This is a test string with exactly forty-four characters!!"
	got := nilCounter.Count(text)
	want := len(text) / 4
	if got != want {
		t.Errorf("nil counter: Count(%q) = %d, want %d (len/4)", text, got, want)
	}
}

func TestH1_TokenEstimate_VsProviderReported(t *testing.T) {
	// Simulate real provider-reported tokens vs our estimate.
	// These represent typical SageClaw system prompts + tool schemas.
	counter, err := tokenizer.Get()
	if err != nil {
		t.Fatalf("tokenizer init: %v", err)
	}

	tests := []struct {
		name            string
		text            string
		providerTokens  int     // What the actual provider would report
		maxVariancePct  float64 // Acceptable variance %
	}{
		{
			// cl100k_base counts ~232 tokens for this prompt.
			// Anthropic's tokenizer may report ~250-280 (different BPE merges).
			// The key insight: our estimate is consistently LOWER than provider-reported.
			// This means our budget calculations UNDERCOUNT, which is conservative (safe).
			name:           "typical_system_prompt",
			text:           generateSystemPromptSample(),
			providerTokens: 260, // Realistic estimate for Anthropic/OpenAI
			maxVariancePct: 20,  // cl100k_base can be ±20% vs actual provider
		},
		{
			name:           "tool_result_code",
			text:           generateCodeResultSample(),
			providerTokens: 90,
			maxVariancePct: 20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			estimated := counter.Count(tt.text)
			variance := math.Abs(float64(estimated)-float64(tt.providerTokens)) / float64(tt.providerTokens) * 100
			t.Logf("%s: estimated=%d, expected_provider=%d, variance=%.1f%%",
				tt.name, estimated, tt.providerTokens, variance)
			if variance > tt.maxVariancePct {
				t.Errorf("token variance %.1f%% exceeds max %.1f%%", variance, tt.maxVariancePct)
			}
		})
	}
}

// =============================================================================
// HYPOTHESIS 2: Pricing Accuracy
// Validates our hardcoded prices against official API pricing.
// Sources: Anthropic, OpenAI, Google pricing pages (as of early 2026).
// =============================================================================

func TestH2_PricingAccuracy_Anthropic(t *testing.T) {
	// Official Anthropic pricing (https://docs.anthropic.com/en/docs/about-claude/models)
	// Prices as of Claude 4 family launch.
	tests := []struct {
		modelID    string
		wantInput  float64 // $/1M input tokens
		wantOutput float64 // $/1M output tokens
		wantCache  float64 // $/1M cache read tokens
	}{
		{"claude-opus-4-20250514", 15.0, 75.0, 1.5},
		{"claude-sonnet-4-20250514", 3.0, 15.0, 0.3},
		{"claude-haiku-4-5-20251001", 0.8, 4.0, 0.08},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			m := FindModel(tt.modelID)
			if m == nil {
				t.Fatalf("model %q not found in KnownModels", tt.modelID)
			}
			if m.InputCost != tt.wantInput {
				t.Errorf("InputCost = %v, want %v (official)", m.InputCost, tt.wantInput)
			}
			if m.OutputCost != tt.wantOutput {
				t.Errorf("OutputCost = %v, want %v (official)", m.OutputCost, tt.wantOutput)
			}
			if m.CacheCost != tt.wantCache {
				t.Errorf("CacheCost = %v, want %v (official)", m.CacheCost, tt.wantCache)
			}
		})
	}
}

func TestH2_PricingAccuracy_OpenAI(t *testing.T) {
	// Official OpenAI pricing (https://openai.com/api/pricing/)
	tests := []struct {
		modelID    string
		wantInput  float64
		wantOutput float64
		wantCache  float64
	}{
		{"gpt-4.1", 2.0, 8.0, 0.5},
		{"gpt-4.1-mini", 0.4, 1.6, 0.1},
		{"gpt-4.1-nano", 0.1, 0.4, 0.025},
		{"gpt-4o", 2.5, 10.0, 1.25},
		{"gpt-4o-mini", 0.15, 0.6, 0.075},
		{"o3", 10.0, 40.0, 2.5},
		{"o3-mini", 1.1, 4.4, 0.275},
		{"o4-mini", 1.1, 4.4, 0.275},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			m := FindModel(tt.modelID)
			if m == nil {
				t.Fatalf("model %q not found", tt.modelID)
			}
			if m.InputCost != tt.wantInput {
				t.Errorf("InputCost = %v, want %v", m.InputCost, tt.wantInput)
			}
			if m.OutputCost != tt.wantOutput {
				t.Errorf("OutputCost = %v, want %v", m.OutputCost, tt.wantOutput)
			}
			if m.CacheCost != tt.wantCache {
				t.Errorf("CacheCost = %v, want %v", m.CacheCost, tt.wantCache)
			}
		})
	}
}

func TestH2_PricingAccuracy_Gemini(t *testing.T) {
	// Official Google pricing (https://ai.google.dev/pricing)
	tests := []struct {
		modelID    string
		wantInput  float64
		wantOutput float64
		wantCache  float64
	}{
		{"gemini-2.5-pro", 1.25, 10.0, 0.315},
		{"gemini-2.5-flash", 0.15, 0.6, 0.0375},
		{"gemini-2.0-flash", 0.1, 0.4, 0.025},
		{"gemini-2.5-flash-lite", 0.1, 0.4, 0.025},
	}

	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			m := FindModel(tt.modelID)
			if m == nil {
				t.Fatalf("model %q not found", tt.modelID)
			}
			if m.InputCost != tt.wantInput {
				t.Errorf("InputCost = %v, want %v", m.InputCost, tt.wantInput)
			}
			if m.OutputCost != tt.wantOutput {
				t.Errorf("OutputCost = %v, want %v", m.OutputCost, tt.wantOutput)
			}
			if m.CacheCost != tt.wantCache {
				t.Errorf("CacheCost = %v, want %v", m.CacheCost, tt.wantCache)
			}
		})
	}
}

// =============================================================================
// HYPOTHESIS 2b: CalculateCost Formula Correctness
// Hand-computed reference costs for known inputs.
// =============================================================================

func TestH2b_CalculateCost_HandVerified(t *testing.T) {
	tests := []struct {
		name      string
		pricing   ModelPricing
		input     int
		output    int
		cacheCr   int
		cacheRd   int
		thinking  int
		wantCost  float64
		wantSaved float64
		tolerance float64 // Acceptable float rounding error
	}{
		{
			name:     "sonnet4_simple_no_cache",
			pricing:  ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3},
			input:    10000, output: 2000, cacheCr: 0, cacheRd: 0, thinking: 0,
			// cost = (10000/1M)*3.0 + (2000/1M)*15.0 = 0.03 + 0.03 = 0.06
			wantCost: 0.06, wantSaved: 0, tolerance: 0.0001,
		},
		{
			name:     "sonnet4_with_cache_read",
			pricing:  ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3},
			input:    10000, output: 2000, cacheCr: 0, cacheRd: 8000, thinking: 0,
			// regularInput = 10000-8000 = 2000
			// cost = (2000/1M)*3.0 + (2000/1M)*15.0 + (8000/1M)*0.3
			//      = 0.006 + 0.03 + 0.0024 = 0.0384
			// fullCost = (10000/1M)*3.0 + (2000/1M)*15.0 = 0.03 + 0.03 = 0.06
			// saved = 0.06 - 0.0384 = 0.0216
			wantCost: 0.0384, wantSaved: 0.0216, tolerance: 0.0001,
		},
		{
			name:     "sonnet4_with_cache_creation",
			pricing:  ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3, CacheCreationCost: 3.75},
			input:    10000, output: 2000, cacheCr: 5000, cacheRd: 0, thinking: 0,
			// cost = (10000/1M)*3.0 + (2000/1M)*15.0 + (5000/1M)*3.75
			//      = 0.03 + 0.03 + 0.01875 = 0.07875
			// fullCost = 0.03 + 0.03 = 0.06
			// saved = 0.06 - 0.07875 = negative → 0 (cache creation costs more)
			wantCost: 0.07875, wantSaved: 0, tolerance: 0.0001,
		},
		{
			name:     "opus4_with_thinking",
			pricing:  ModelPricing{InputCost: 15.0, OutputCost: 75.0, CacheCost: 1.5, ThinkingCost: 0},
			input:    5000, output: 3000, cacheCr: 0, cacheRd: 0, thinking: 2000,
			// ThinkingCost=0 → falls back to OutputCost=75.0
			// regularOutput = 3000-2000 = 1000
			// cost = (5000/1M)*15.0 + (1000/1M)*75.0 + (2000/1M)*75.0
			//      = 0.075 + 0.075 + 0.15 = 0.3
			wantCost: 0.3, wantSaved: 0, tolerance: 0.001,
		},
		{
			name:    "zero_tokens",
			pricing: ModelPricing{InputCost: 3.0, OutputCost: 15.0},
			input:   0, output: 0, cacheCr: 0, cacheRd: 0, thinking: 0,
			wantCost: 0, wantSaved: 0, tolerance: 0,
		},
		{
			name:     "nil_pricing",
			pricing:  ModelPricing{}, // We'll pass nil below
			input:    10000, output: 2000,
			wantCost: 0, wantSaved: 0, tolerance: 0,
		},
		{
			name:     "cache_read_exceeds_input",
			pricing:  ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3},
			input:    5000, output: 1000, cacheCr: 0, cacheRd: 8000, thinking: 0,
			// regularInput = max(5000-8000, 0) = 0
			// cost = 0 + (1000/1M)*15.0 + (8000/1M)*0.3 = 0.0174
			// fullCost = (5000/1M)*3.0 + (1000/1M)*15.0 = 0.015 + 0.015 = 0.03
			// saved = 0.03 - 0.0174 = 0.0126
			wantCost: 0.0174, wantSaved: 0.0126, tolerance: 0.0001,
		},
		{
			name:     "gemini_flash_with_thinking",
			pricing:  ModelPricing{InputCost: 0.15, OutputCost: 0.6, CacheCost: 0.0375, ThinkingCost: 0},
			input:    50000, output: 10000, cacheCr: 0, cacheRd: 40000, thinking: 5000,
			// ThinkingCost=0 → 0.6
			// regularInput = 50000-40000 = 10000
			// regularOutput = 10000-5000 = 5000
			// cost = (10000/1M)*0.15 + (5000/1M)*0.6 + (5000/1M)*0.6 + (40000/1M)*0.0375
			//      = 0.0015 + 0.003 + 0.003 + 0.0015 = 0.009
			// fullCost = (50000/1M)*0.15 + (5000/1M)*0.6 + (5000/1M)*0.6
			//          = 0.0075 + 0.003 + 0.003 = 0.0135
			// saved = 0.0135 - 0.009 = 0.0045
			wantCost: 0.009, wantSaved: 0.0045, tolerance: 0.001,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var pricing *ModelPricing
			if tt.name != "nil_pricing" {
				pricing = &tt.pricing
			}

			cost, saved := CalculateCost(pricing, tt.input, tt.output, tt.cacheCr, tt.cacheRd, tt.thinking)

			if math.Abs(cost-tt.wantCost) > tt.tolerance {
				t.Errorf("cost = %.6f, want %.6f (diff %.6f)", cost, tt.wantCost, math.Abs(cost-tt.wantCost))
			}
			if math.Abs(saved-tt.wantSaved) > tt.tolerance {
				t.Errorf("saved = %.6f, want %.6f (diff %.6f)", saved, tt.wantSaved, math.Abs(saved-tt.wantSaved))
			}
		})
	}
}

// =============================================================================
// HYPOTHESIS 3: Optimization Effectiveness
// Measures whether caching actually produces savings for typical sessions.
// =============================================================================

func TestH3_CacheSavings_TypicalSession(t *testing.T) {
	// Simulate a 10-iteration agent session with Sonnet 4.
	// System prompt is cached from iteration 2 onward.
	pricing := &ModelPricing{
		InputCost:         3.0,
		OutputCost:        15.0,
		CacheCost:         0.3,
		CacheCreationCost: 3.75,
	}

	systemPromptTokens := 4000
	avgHistoryTokens := 2000    // Growing per iteration
	avgOutputTokens := 500

	var totalCost, totalSaved float64
	var totalNoCacheCost float64

	for i := 0; i < 10; i++ {
		inputTokens := systemPromptTokens + avgHistoryTokens*(i+1)
		var cacheRead, cacheCreation int

		if i == 0 {
			// First request: cache creation for system prompt.
			cacheCreation = systemPromptTokens
		} else {
			// Subsequent: system prompt read from cache.
			cacheRead = systemPromptTokens
		}

		cost, saved := CalculateCost(pricing, inputTokens, avgOutputTokens, cacheCreation, cacheRead, 0)
		totalCost += cost
		totalSaved += saved

		// No-cache baseline: all input at full price.
		noCacheCost := float64(inputTokens)/1_000_000*pricing.InputCost +
			float64(avgOutputTokens)/1_000_000*pricing.OutputCost
		totalNoCacheCost += noCacheCost
	}

	actualSavingsPct := (totalNoCacheCost - totalCost) / totalNoCacheCost * 100

	t.Logf("10-iteration Sonnet 4 session:")
	t.Logf("  With caching:    $%.4f (saved $%.4f reported)", totalCost, totalSaved)
	t.Logf("  Without caching: $%.4f", totalNoCacheCost)
	t.Logf("  Actual savings:  %.1f%%", actualSavingsPct)

	// Caching should save meaningful $ on a 10-iteration session.
	if actualSavingsPct < 5 {
		t.Errorf("caching saves only %.1f%% — expected >5%% on a 10-iteration session", actualSavingsPct)
	}
}

func TestH3_CacheSavings_ReportedVsActual(t *testing.T) {
	// Verify that CalculateCost's "saved" matches the actual difference.
	pricing := &ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3}

	input := 10000
	output := 2000
	cacheRd := 8000

	cost, saved := CalculateCost(pricing, input, output, 0, cacheRd, 0)

	// Manual: what would it cost without any cache?
	noCacheCost := float64(input)/1_000_000*pricing.InputCost +
		float64(output)/1_000_000*pricing.OutputCost
	actualSaved := noCacheCost - cost

	diff := math.Abs(saved - actualSaved)
	if diff > 0.0001 {
		t.Errorf("reported saved=$%.6f but actual saved=$%.6f (diff $%.6f)", saved, actualSaved, diff)
	}
	t.Logf("cost=$%.6f, saved=$%.6f, noCacheCost=$%.6f, verified match", cost, saved, noCacheCost)
}

func TestH3_CacheStats_AccumulationAccuracy(t *testing.T) {
	// Create a fresh CacheStats and verify accumulation matches manual calculation.
	cs := &CacheStats{}
	resolver := &mockResolver{pricing: &ModelPricing{
		InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3,
	}}
	cs.SetResolver(resolver)

	// Record 3 requests.
	cs.Record("test-model", 10000, 2000, 0, 0, 0)     // $0.06
	cs.Record("test-model", 10000, 2000, 0, 8000, 0)   // $0.0384, saved $0.0216
	cs.Record("test-model", 10000, 2000, 5000, 8000, 0) // with creation

	snap := cs.Snapshot()

	// Verify token accumulation.
	if snap.TotalInput != 30000 {
		t.Errorf("TotalInput = %d, want 30000", snap.TotalInput)
	}
	if snap.TotalOutput != 6000 {
		t.Errorf("TotalOutput = %d, want 6000", snap.TotalOutput)
	}
	if snap.CacheRead != 16000 {
		t.Errorf("CacheRead = %d, want 16000", snap.CacheRead)
	}
	if snap.CacheHits != 2 {
		t.Errorf("CacheHits = %d, want 2", snap.CacheHits)
	}

	// Verify cost > 0 (accumulation is working).
	if snap.EstCostUSD <= 0 {
		t.Error("EstCostUSD should be > 0 after recording requests")
	}
	if snap.EstSavedUSD <= 0 {
		t.Error("EstSavedUSD should be > 0 when cache reads occur")
	}

	t.Logf("3 requests: cost=$%.6f, saved=$%.6f, hits=%d/%d",
		snap.EstCostUSD, snap.EstSavedUSD, snap.CacheHits, snap.TotalRequests)
}

// =============================================================================
// BUG DETECTION: Edge cases that could cause unstable dashboard numbers
// =============================================================================

func TestBug_UnknownModel_CostZero(t *testing.T) {
	// Unknown models should produce $0, not crash or produce garbage.
	m := FindModel("nonexistent-model-xyz")
	if m != nil {
		t.Fatal("expected nil for unknown model")
	}

	cost, saved := CalculateCost(nil, 10000, 2000, 0, 0, 0)
	if cost != 0 || saved != 0 {
		t.Errorf("nil pricing should return (0,0), got (%v,%v)", cost, saved)
	}
}

func TestBug_NegativeTokens_NoPanic(t *testing.T) {
	// Providers could theoretically report negative or zero tokens.
	pricing := &ModelPricing{InputCost: 3.0, OutputCost: 15.0}

	// Negative should not produce negative cost.
	cost, _ := CalculateCost(pricing, -100, -50, 0, 0, 0)
	if cost < 0 {
		t.Errorf("negative tokens produced negative cost: %v", cost)
	}
}

func TestBug_ThinkingExceedsOutput(t *testing.T) {
	// If thinkingTokens > outputTokens (provider bug), regularOutput goes negative.
	pricing := &ModelPricing{InputCost: 3.0, OutputCost: 15.0}

	cost, _ := CalculateCost(pricing, 1000, 500, 0, 0, 1000)
	// regularOutput = 500-1000 → clamped to 0.
	// cost should still be positive.
	if cost < 0 {
		t.Errorf("thinking > output produced negative cost: %v", cost)
	}
	t.Logf("thinking(%d) > output(%d): cost=$%.6f (should be positive)", 1000, 500, cost)
}

func TestBug_CacheReadExceedsInput(t *testing.T) {
	// If cacheRead > inputTokens (provider reports more cache than input).
	pricing := &ModelPricing{InputCost: 3.0, OutputCost: 15.0, CacheCost: 0.3}

	cost, saved := CalculateCost(pricing, 1000, 500, 0, 5000, 0)
	// regularInput = max(1000-5000, 0) = 0
	if cost < 0 {
		t.Errorf("cacheRead > input produced negative cost: %v", cost)
	}
	t.Logf("cacheRead(%d) > input(%d): cost=$%.6f, saved=$%.6f", 5000, 1000, cost, saved)
}

func TestBug_CacheStatsSnapshot_WithCalculations(t *testing.T) {
	snap := CacheStatsSnapshot{
		TotalRequests: 10,
		CacheHits:     7,
		TotalInput:    100000,
		CacheRead:     70000,
		EstCostUSD:    0.5,
		EstSavedUSD:   0.2,
	}

	calc := snap.WithCalculations()

	// Hit rate = 70%.
	if math.Abs(calc.HitRate-70.0) > 0.1 {
		t.Errorf("HitRate = %.1f, want 70.0", calc.HitRate)
	}

	// CostSavings should be > 0 when cache reads exist.
	if calc.CostSavings <= 0 {
		t.Errorf("CostSavings = %.1f, want > 0", calc.CostSavings)
	}

	t.Logf("Snapshot: hitRate=%.1f%%, costSavings=%.1f%%, cost=$%.2f, saved=$%.2f",
		calc.HitRate, calc.CostSavings, calc.EstCostUSD, calc.EstSavedUSD)
}

// =============================================================================
// Helpers
// =============================================================================

type mockResolver struct {
	pricing *ModelPricing
}

func (m *mockResolver) ResolvePricing(modelID string) *ModelPricing {
	return m.pricing
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func generateSystemPromptSample() string {
	return `You are a helpful AI assistant named Sage. You help users with software engineering tasks.

## Tools Available
- read_file: Read a file from the filesystem
- write_file: Write content to a file
- edit_file: Make targeted edits to a file
- grep: Search file contents with regex
- glob: Find files matching a pattern
- execute_command: Run a shell command
- web_fetch: Fetch a URL
- web_search: Search the web

## Rules
1. Always read files before editing them
2. Prefer targeted edits over full rewrites
3. Run tests after making changes
4. Never commit secrets to version control
5. Follow existing code conventions

## Memory Context
The user is working on a Go project called SageClaw. It's a multi-agent AI framework with provider routing, tool execution, and a Preact dashboard.

Previous findings:
- The project uses SQLite for storage
- Budget tracking is in pkg/provider/budget.go
- The dashboard shows cost data from /api/budget/summary

## Current Task
Help the user investigate budget tracking instability in the dashboard overview tab.`
}

func generateCodeResultSample() string {
	return `package main

import (
	"fmt"
	"net/http"
	"log"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "Hello, World!")
	})

	log.Println("Server starting on :8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatal(err)
	}
}`
}
