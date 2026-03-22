package pipeline

import (
	"context"
	"log"
	"strings"

	"github.com/xoai/sageclaw/pkg/activity"
	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/store"
)

// CostRecorderFunc is called after each agent run with usage data.
type CostRecorderFunc func(ctx context.Context, sessionID, agentID, providerName, model string, usage canonical.Usage)

// Pipeline orchestrates the 5-stage message processing pipeline.
type Pipeline struct {
	bus              bus.MessageBus
	debouncer        *Debouncer
	scheduler        Scheduler
	store            store.Store
	agentLoop        *agent.Loop
	agentID          string
	preResponse      middleware.Middleware
	pairing          *security.PairingManager
	costRecorder     CostRecorderFunc
	activityTracker  *activity.Tracker
}

// Config for the pipeline.
type PipelineConfig struct {
	AgentID      string
	PreResponse  middleware.Middleware                // Optional: runs before outbound delivery.
	Pairing      *security.PairingManager            // Optional: channel pairing enforcement.
	CostRecorder CostRecorderFunc                    // Optional: records cost after each agent run.
}

// New creates a new pipeline.
func New(
	messageBus bus.MessageBus,
	scheduler Scheduler,
	store store.Store,
	agentLoop *agent.Loop,
	config PipelineConfig,
) *Pipeline {
	p := &Pipeline{
		bus:             messageBus,
		scheduler:       scheduler,
		store:           store,
		agentLoop:       agentLoop,
		agentID:         config.AgentID,
		preResponse:     config.PreResponse,
		pairing:         config.Pairing,
		costRecorder:    config.CostRecorder,
		activityTracker: activity.NewTracker(store.DB()),
	}

	// Create debouncer that feeds into intent classification.
	p.debouncer = NewDebouncer(0, p.onDebounced)

	return p
}

// channelKey encodes channel, chatID, and optional agentID into a single key for the debouncer.
func channelKey(channel, chatID, agentID string) string {
	if channel == "" {
		channel = "telegram"
	}
	if agentID == "" {
		return channel + "|" + chatID
	}
	return channel + "|" + chatID + "|" + agentID
}

// parseChannelKey splits the composite key back into channel, chatID, and agentID.
func parseChannelKey(key string) (channel, chatID, agentID string) {
	parts := strings.SplitN(key, "|", 3)
	switch len(parts) {
	case 3:
		return parts[0], parts[1], parts[2]
	case 2:
		return parts[0], parts[1], ""
	default:
		return "telegram", key, ""
	}
}

// Start subscribes to inbound messages and begins processing.
func (p *Pipeline) Start(ctx context.Context) error {
	return p.bus.SubscribeInbound(ctx, func(env bus.Envelope) {
		// S0: Channel pairing check.
		if p.pairing != nil {
			pairCtx := context.Background()

			// Check if already paired.
			if !p.pairing.IsPaired(pairCtx, env.Channel, env.ChatID) {
				// Check if the message is a pairing code.
				for _, msg := range env.Messages {
					for _, c := range msg.Content {
						if c.Type == "text" && c.Text != "" {
							if ok, _ := p.pairing.VerifyCode(pairCtx, env.Channel, env.ChatID, c.Text); ok {
								log.Printf("pairing: %s/%s paired successfully", env.Channel, env.ChatID)
								p.bus.PublishOutbound(pairCtx, bus.Envelope{
									Channel: env.Channel,
									ChatID:  env.ChatID,
									Messages: []canonical.Message{{
										Role: "assistant",
										Content: []canonical.Content{{Type: "text", Text: "Paired successfully! You can now chat with SageClaw."}},
									}},
								})
								return
							}
						}
					}
				}

				// Not paired and not a valid code — reject.
				log.Printf("pairing: rejected message from unpaired %s/%s", env.Channel, env.ChatID)
				p.bus.PublishOutbound(pairCtx, bus.Envelope{
					Channel: env.Channel,
					ChatID:  env.ChatID,
					Messages: []canonical.Message{{
						Role: "assistant",
						Content: []canonical.Content{{Type: "text", Text: "This bot is private. Ask the owner for a pairing code."}},
					}},
				})
				return
			}
		}

		// S1: Channel ingestion — messages already normalized by channel adapter.
		key := channelKey(env.Channel, env.ChatID, env.AgentID)
		for _, msg := range env.Messages {
			// S2: Debouncer.
			p.debouncer.Add(key, msg)
		}
	})
}

// onDebounced is called when the debouncer flushes a batch.
func (p *Pipeline) onDebounced(compositeKey string, msgs []canonical.Message) {
	channel, chatID, agentID := parseChannelKey(compositeKey)
	ctx := context.Background()

	// S3: Intent classification.
	intent := ClassifyIntent(msgs)

	switch intent.Type {
	case IntentCommand:
		p.handleCommand(ctx, chatID, intent.Command)
	case IntentAgent:
		p.routeToAgent(ctx, channel, chatID, agentID, msgs)
	}
}

func (p *Pipeline) handleCommand(ctx context.Context, chatID string, command string) {
	var response string
	switch command {
	case "start":
		response = "Welcome to SageClaw! I'm your personal AI agent. Send me a message to get started."
	case "help":
		response = "Available commands:\n/start - Start\n/help - Show this help\n/status - Check status\n/stop - Stop current task"
	case "status":
		response = "SageClaw is running. Send me a message to interact."
	case "stop":
		response = "Stopping current task."
	default:
		response = "Unknown command: " + command
	}

	p.bus.PublishOutbound(ctx, bus.Envelope{
		ChatID: chatID,
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: response}}},
		},
	})
}

func (p *Pipeline) routeToAgent(ctx context.Context, channel, chatID, requestAgentID string, msgs []canonical.Message) {
	// Determine which agent to use: envelope override > pipeline default.
	agentID := p.agentID
	if requestAgentID != "" {
		agentID = requestAgentID
	}

	// If agentID is still "default" but no such agent exists, try to find the first available agent.
	if agentID == "default" || agentID == "" {
		var firstAgent string
		p.store.DB().QueryRow(`SELECT id FROM agents ORDER BY id LIMIT 1`).Scan(&firstAgent)
		if firstAgent != "" {
			agentID = firstAgent
		}
	}

	// Find or create session.
	sess, err := p.store.FindSession(ctx, channel, chatID)
	if err != nil {
		// Create new session.
		sess, err = p.store.CreateSession(ctx, channel, chatID, agentID)
		if err != nil {
			log.Printf("failed to create session: %v", err)
			return
		}
	}

	// S4: Schedule on the main lane.
	req := RunRequest{
		SessionID: sess.ID,
		AgentID:   agentID,
		Messages:  msgs,
		Lane:      LaneMain,
	}

	if err := p.scheduler.Schedule(ctx, LaneMain, req); err != nil {
		log.Printf("failed to schedule: %v", err)
	}
}

// RunAgent is the function called by the scheduler to execute an agent run.
func (p *Pipeline) RunAgent(ctx context.Context, req RunRequest) {
	// Create Activity (ADR Rule D).
	activityID := ""
	if p.activityTracker != nil {
		id, err := p.activityTracker.Start(ctx, req.SessionID, req.AgentID, "", 0)
		if err == nil {
			activityID = id
		} else {
			log.Printf("failed to create activity: %v", err)
		}
	}

	// Load conversation history.
	history, err := p.store.GetMessages(ctx, req.SessionID, 100)
	if err != nil {
		log.Printf("failed to load history: %v", err)
		history = nil
	}

	// Append new messages.
	allMsgs := append(history, req.Messages...)

	// S5: Agent loop.
	result := p.agentLoop.Run(ctx, req.SessionID, allMsgs)

	// Update Activity with result.
	if p.activityTracker != nil && activityID != "" {
		p.activityTracker.RecordIteration(ctx, activityID,
			result.Usage.InputTokens, result.Usage.OutputTokens,
			result.Usage.CacheCreation, result.Usage.CacheRead, 0)
		if result.Error != nil {
			isTimeout := strings.Contains(result.Error.Error(), "timed out")
			p.activityTracker.Fail(ctx, activityID, result.Error.Error(), isTimeout)
		} else {
			summary := extractSummary(result.Messages)
			p.activityTracker.Complete(ctx, activityID, summary)
		}
	}

	// Record cost.
	if p.costRecorder != nil && (result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0) {
		provName, model := "", ""
		if p.agentLoop != nil {
			provName, model = p.agentLoop.ProviderAndModel()
		}
		p.costRecorder(ctx, req.SessionID, req.AgentID, provName, model, result.Usage)
	}

	// Session flush: persist all messages atomically.
	if len(result.Messages) > 0 {
		toStore := append(req.Messages, result.Messages...)
		if err := p.store.AppendMessages(ctx, req.SessionID, toStore); err != nil {
			log.Printf("failed to store messages: %v", err)
		}
	}

	// Run PreResponse middleware (if configured).
	if p.preResponse != nil && result.Error == nil {
		hookData := &middleware.HookData{
			HookPoint: middleware.HookPreResponse,
			Response: &canonical.Response{
				Messages: result.Messages,
				Usage:    result.Usage,
			},
			Metadata: map[string]any{"session_id": req.SessionID, "agent_id": req.AgentID},
		}
		if err := p.preResponse(ctx, hookData, func(ctx context.Context, data *middleware.HookData) error {
			return nil
		}); err != nil {
			log.Printf("preresponse middleware error: %v", err)
		} else if hookData.Response != nil {
			result.Messages = hookData.Response.Messages
		}
	}

	// Deliver response via outbound bus.
	if result.Error == nil && len(result.Messages) > 0 {
		// Find the last assistant text message.
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == "assistant" {
				sess, _ := p.store.GetSession(ctx, req.SessionID)
				chatID := ""
				if sess != nil {
					chatID = sess.ChatID
				}
				p.bus.PublishOutbound(ctx, bus.Envelope{
					SessionID: req.SessionID,
					ChatID:    chatID,
					Messages:  []canonical.Message{result.Messages[i]},
				})
				break
			}
		}
	}
}

// extractSummary returns the first 80 chars of the last assistant message.
func extractSummary(msgs []canonical.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			text := agent.ExtractText(msgs[i])
			if len(text) > 80 {
				return text[:80]
			}
			return text
		}
	}
	return ""
}
