package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/tool"
)

const defaultMaxIterations = 25

// Config holds agent loop configuration.
type Config struct {
	AgentID       string
	SystemPrompt  string
	Model         string
	MaxTokens     int
	MaxIterations int
	Timeout       time.Duration // Wall clock timeout. Default: 300s.
	Tools         []string      // Tool names to enable.
}

// Loop runs the agent's think-act-observe cycle.
type Loop struct {
	config       Config
	provider     provider.Provider
	router       *provider.Router // Optional: tier-based routing (v0.2+).
	toolRegistry *tool.Registry
	preContext    middleware.Middleware
	postTool     middleware.Middleware
	onEvent      EventHandler

	mu         sync.Mutex
	injectChan chan canonical.Message // For steer/inject.
}

// LoopOption configures optional Loop features.
type LoopOption func(*Loop)

// WithRouter adds a model router for tier-based provider selection.
func WithRouter(r *provider.Router) LoopOption {
	return func(l *Loop) { l.router = r }
}

// NewLoop creates a new agent loop.
func NewLoop(
	config Config,
	prov provider.Provider,
	toolReg *tool.Registry,
	preContext middleware.Middleware,
	postTool middleware.Middleware,
	onEvent EventHandler,
	opts ...LoopOption,
) *Loop {
	if config.MaxIterations == 0 {
		config.MaxIterations = defaultMaxIterations
	}
	if config.MaxTokens == 0 {
		config.MaxTokens = 8192
	}
	if config.Timeout == 0 {
		config.Timeout = 300 * time.Second
	}
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	l := &Loop{
		config:       config,
		provider:     prov,
		toolRegistry: toolReg,
		preContext:    preContext,
		postTool:     postTool,
		onEvent:      onEvent,
		injectChan:   make(chan canonical.Message, 10),
	}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Inject adds a message to the loop's injection queue (steer/inject pattern).
func (l *Loop) Inject(msg canonical.Message) {
	select {
	case l.injectChan <- msg:
	default:
		// Buffer full, drop.
	}
}

// RunResult holds the outcome of an agent loop execution.
type RunResult struct {
	Messages []canonical.Message // All messages from the run (including tool calls/results).
	Usage    canonical.Usage     // Aggregated token usage.
	Error    error
}

// Run executes the agent loop for a given conversation history.
func (l *Loop) Run(ctx context.Context, sessionID string, history []canonical.Message) RunResult {
	// Wall clock timeout (ADR: Rule C).
	ctx, cancel := context.WithTimeout(ctx, l.config.Timeout)
	defer cancel()

	l.onEvent(Event{Type: EventRunStarted, SessionID: sessionID, AgentID: l.config.AgentID})

	var pendingMsgs []canonical.Message
	var totalUsage canonical.Usage

	// Sanitize history before starting.
	history = SanitizeHistory(history)

	maxIter := l.config.MaxIterations
	for iteration := 0; iteration < maxIter; iteration++ {
		// Check for injected messages (steer/inject).
		l.drainInjections(&history)

		// Run PreContext middleware.
		hookData := &middleware.HookData{
			HookPoint: middleware.HookPreContext,
			Messages:  history,
			Metadata:  map[string]any{"session_id": sessionID, "agent_id": l.config.AgentID},
		}
		if l.preContext != nil {
			l.preContext(ctx, hookData, func(ctx context.Context, data *middleware.HookData) error {
				return nil
			})
		}

		// Build the request.
		req := l.buildRequest(history, hookData.Injections)

		// Resolve provider and model via router (or use direct provider).
		activeProvider, activeModel := l.resolveProvider()
		req.Model = activeModel

		// Call LLM.
		resp, err := activeProvider.Chat(ctx, req)
		if err != nil {
			l.onEvent(Event{Type: EventRunFailed, SessionID: sessionID, Error: err, Iteration: iteration})
			return RunResult{Messages: pendingMsgs, Usage: totalUsage, Error: fmt.Errorf("LLM call failed (iteration %d): %w", iteration, err)}
		}

		// Accumulate usage.
		totalUsage.InputTokens += resp.Usage.InputTokens
		totalUsage.OutputTokens += resp.Usage.OutputTokens
		totalUsage.CacheCreation += resp.Usage.CacheCreation
		totalUsage.CacheRead += resp.Usage.CacheRead

		// Process the response.
		if len(resp.Messages) == 0 {
			break
		}
		assistantMsg := resp.Messages[0]
		history = append(history, assistantMsg)
		pendingMsgs = append(pendingMsgs, assistantMsg)

		// Emit text chunks.
		text := ExtractText(assistantMsg)
		if text != "" {
			l.onEvent(Event{Type: EventChunk, SessionID: sessionID, Text: text, Iteration: iteration})
		}

		// Check stop reason.
		if resp.StopReason == "end_turn" || resp.StopReason == "max_tokens" {
			break
		}

		// Handle tool calls.
		if resp.StopReason == "tool_use" || HasToolCalls(assistantMsg) {
			toolCalls := ExtractToolCalls(assistantMsg)
			var results []canonical.ToolResult

			for _, tc := range toolCalls {
				l.onEvent(Event{Type: EventToolCall, SessionID: sessionID, ToolCall: &tc, Iteration: iteration})

				// Execute tool.
				result, err := l.toolRegistry.Execute(ctx, tc.Name, tc.Input)
				if err != nil {
					result = &canonical.ToolResult{
						ToolCallID: tc.ID,
						Content:    fmt.Sprintf("Tool error: %v", err),
						IsError:    true,
					}
				} else {
					result.ToolCallID = tc.ID
				}

				// Run PostTool middleware.
				postData := &middleware.HookData{
					HookPoint:  middleware.HookPostTool,
					ToolCall:   &tc,
					ToolResult: result,
					Metadata:   map[string]any{"session_id": sessionID, "agent_id": l.config.AgentID},
				}
				if l.postTool != nil {
					l.postTool(ctx, postData, func(ctx context.Context, data *middleware.HookData) error {
						return nil
					})
					result = postData.ToolResult
				}

				l.onEvent(Event{Type: EventToolResult, SessionID: sessionID, ToolResult: result, Iteration: iteration})
				results = append(results, *result)
			}

			// Add tool results to history.
			toolResultMsg := BuildToolResultMessage(results)
			history = append(history, toolResultMsg)
			pendingMsgs = append(pendingMsgs, toolResultMsg)
			continue // Loop back for next iteration.
		}

		// Unknown stop reason — break.
		break
	}

	// Check if we timed out.
	if ctx.Err() == context.DeadlineExceeded {
		timeoutErr := fmt.Errorf("agent loop timed out after %s", l.config.Timeout)
		l.onEvent(Event{Type: EventRunFailed, SessionID: sessionID, Error: timeoutErr, Iteration: -1})
		return RunResult{Messages: pendingMsgs, Usage: totalUsage, Error: timeoutErr}
	}

	l.onEvent(Event{Type: EventRunCompleted, SessionID: sessionID, AgentID: l.config.AgentID})
	return RunResult{Messages: pendingMsgs, Usage: totalUsage}
}

func (l *Loop) buildRequest(history []canonical.Message, injections []string) *canonical.Request {
	system := l.config.SystemPrompt
	if len(injections) > 0 {
		system += "\n\n" + strings.Join(injections, "\n\n")
	}

	// Get tool definitions.
	var tools []canonical.ToolDef
	if len(l.config.Tools) > 0 {
		for _, name := range l.config.Tools {
			def, _, ok := l.toolRegistry.Get(name)
			if ok {
				tools = append(tools, def)
			}
		}
	} else {
		tools = l.toolRegistry.List()
	}

	return &canonical.Request{
		Model:     l.config.Model, // May be overridden by router in Run().
		System:    system,
		Messages:  history,
		Tools:     tools,
		MaxTokens: l.config.MaxTokens,
	}
}

// resolveProvider returns the provider and model to use, consulting the router if available.
func (l *Loop) resolveProvider() (provider.Provider, string) {
	if l.router != nil {
		tier := provider.Tier(l.config.Model)
		return l.router.Resolve(tier)
	}
	return l.provider, l.config.Model
}

// ProviderAndModel returns the current provider name and model for cost tracking.
func (l *Loop) ProviderAndModel() (string, string) {
	p, m := l.resolveProvider()
	if p != nil {
		return p.Name(), m
	}
	return "", l.config.Model
}

func (l *Loop) drainInjections(history *[]canonical.Message) {
	for {
		select {
		case msg := <-l.injectChan:
			*history = append(*history, msg)
		default:
			return
		}
	}
}
