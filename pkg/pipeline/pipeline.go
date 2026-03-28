package pipeline

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/xoai/sageclaw/pkg/activity"
	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/agentcfg"
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
	loopPool         *agent.LoopPool
	agentID          string
	preResponse      middleware.Middleware
	pairing          *security.PairingManager
	costRecorder     CostRecorderFunc
	activityTracker  *activity.Tracker
	agentProvider    agentcfg.Provider
	liveSessionPool  agent.LiveSessionPool // Optional: for voice messaging.
	nonceManager     *agent.NonceManager   // Optional: for nonce-based consent.
}

// Config for the pipeline.
type PipelineConfig struct {
	AgentID         string
	LoopPool        *agent.LoopPool                     // Multi-agent loop pool.
	PreResponse     middleware.Middleware                // Optional: runs before outbound delivery.
	Pairing         *security.PairingManager            // Optional: channel pairing enforcement.
	CostRecorder    CostRecorderFunc                    // Optional: records cost after each agent run.
	AgentProvider   agentcfg.Provider                   // Optional: agent config lookup for channel filtering.
	LiveSessionPool agent.LiveSessionPool               // Optional: for voice messaging dispatch.
	NonceManager    *agent.NonceManager                 // Optional: for nonce-based consent.
}

// New creates a new pipeline.
func New(
	messageBus bus.MessageBus,
	scheduler Scheduler,
	store store.Store,
	config PipelineConfig,
) *Pipeline {
	p := &Pipeline{
		bus:             messageBus,
		scheduler:       scheduler,
		store:           store,
		loopPool:        config.LoopPool,
		agentID:         config.AgentID,
		preResponse:     config.PreResponse,
		pairing:         config.Pairing,
		costRecorder:    config.CostRecorder,
		activityTracker: activity.NewTracker(store.DB()),
		agentProvider:   config.AgentProvider,
		liveSessionPool: config.LiveSessionPool,
		nonceManager:    config.NonceManager,
	}

	// Create debouncer that feeds into intent classification.
	p.debouncer = NewDebouncer(0, p.onDebounced)

	return p
}

// InjectConsent sends a nonce-based consent response to the correct agent loop.
// Returns an error if the nonce is invalid, expired, or already consumed.
func (p *Pipeline) InjectConsent(nonce string, granted bool, tier string) error {
	if p.loopPool == nil || p.nonceManager == nil {
		return fmt.Errorf("consent infrastructure not available")
	}

	pending, err := p.nonceManager.Validate(nonce)
	if err != nil {
		return fmt.Errorf("invalid consent nonce: %w", err)
	}

	action := "deny"
	if granted {
		action = "grant"
	}
	if tier == "" {
		tier = "once"
	}
	token := fmt.Sprintf("__consent__%s_%s_%s", nonce, action, tier)

	p.loopPool.InjectTo(pending.AgentID, canonical.Message{
		Role:    "user",
		Content: []canonical.Content{{Type: "text", Text: token}},
	})
	return nil
}


// resolveAgentName returns the display name for an agent ID.
// Falls back to the raw ID if no provider is configured or agent not found.
func (p *Pipeline) resolveAgentName(agentID string) string {
	if p.agentProvider != nil {
		if cfg := p.agentProvider.Get(agentID); cfg != nil && cfg.Identity.Name != "" {
			return cfg.Identity.Name
		}
	}
	return agentID
}

// channelKey encodes channel, kind, chatID, threadID, and optional agentID into a single key for the debouncer.
// Kind and threadID are included so DM and group messages debounce independently.
func channelKey(channel, kind, chatID, threadID, agentID string) string {
	if channel == "" {
		channel = "_unknown"
	}
	if kind == "" {
		kind = "dm"
	}
	key := channel + "|" + kind + "|" + chatID
	if threadID != "" {
		key += "|" + threadID
	}
	if agentID != "" {
		key += "|@" + agentID
	}
	return key
}

// parseChannelKey splits the composite key back into channel, kind, chatID, threadID, and agentID.
func parseChannelKey(key string) (channel, kind, chatID, threadID, agentID string) {
	parts := strings.Split(key, "|")
	if len(parts) < 3 {
		return "", "dm", key, "", ""
	}
	channel = parts[0]
	kind = parts[1]
	chatID = parts[2]
	for _, p := range parts[3:] {
		if strings.HasPrefix(p, "@") {
			agentID = p[1:]
		} else if threadID == "" {
			threadID = p
		}
	}
	return
}

// checkPolicy validates whether a message should be processed based on connection policies.
// Returns true if the message should be processed, false if it should be dropped.
func (p *Pipeline) checkPolicy(ctx context.Context, env bus.Envelope) bool {
	conn, err := p.store.GetConnection(ctx, env.Channel)
	if err != nil {
		log.Printf("policy: unknown connection %s, allowing (legacy compat)", env.Channel)
		return true // Unknown connection → allow (legacy compat)
	}

	switch env.Kind {
	case "dm":
		if !conn.DmEnabled {
			log.Printf("policy: DM disabled for connection %s, dropping", env.Channel)
			return false
		}
	case "group":
		if !conn.GroupEnabled {
			log.Printf("policy: groups disabled for connection %s, dropping", env.Channel)
			return false
		}
		if !env.Mentioned {
			log.Printf("policy: not mentioned in group %s/%s, dropping", env.Channel, env.ChatID)
			return false
		}
	}

	return true
}

// Start subscribes to inbound messages and begins processing.
func (p *Pipeline) Start(ctx context.Context) error {
	return p.bus.SubscribeInbound(ctx, func(env bus.Envelope) {
		// S0a: Policy check (dm/group enabled, mention required).
		if !p.checkPolicy(context.Background(), env) {
			return
		}

		// S0b: Channel pairing check.
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
		kind := env.Kind
		if kind == "" {
			kind = "dm"
		}
		key := channelKey(env.Channel, kind, env.ChatID, env.ThreadID, env.AgentID)
		for _, msg := range env.Messages {
			// S2: Debouncer.
			p.debouncer.Add(key, msg)
		}
	})
}

// onDebounced is called when the debouncer flushes a batch.
func (p *Pipeline) onDebounced(compositeKey string, msgs []canonical.Message) {
	channel, kind, chatID, threadID, agentID := parseChannelKey(compositeKey)
	ctx := context.Background()

	// S3: Intent classification.
	intent := ClassifyIntent(msgs)

	switch intent.Type {
	case IntentCommand:
		p.handleCommand(ctx, channel, chatID, kind, intent.Command)
	case IntentAgent:
		p.routeToAgent(ctx, channel, chatID, kind, threadID, agentID, msgs)
	}
}

func (p *Pipeline) handleCommand(ctx context.Context, channel, chatID, kind string, command string) {
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
		Channel: channel,
		ChatID:  chatID,
		Kind:    kind,
		Messages: []canonical.Message{
			{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: response}}},
		},
	})
}

func (p *Pipeline) routeToAgent(ctx context.Context, channel, chatID, kind, threadID, requestAgentID string, msgs []canonical.Message) {
	if kind == "" {
		kind = "dm"
	}

	log.Printf("pipeline: routeToAgent channel=%q chatID=%q kind=%q threadID=%q requestAgentID=%q", channel, chatID, kind, threadID, requestAgentID)

	// Determine which agent to use: envelope override > connection binding > pipeline default.
	agentID := p.agentID
	platform := ""
	if requestAgentID != "" {
		agentID = requestAgentID
		log.Printf("pipeline: using envelope agent override %q", agentID)
	}

	// Look up connection binding if no explicit agent requested.
	if requestAgentID == "" {
		conn, err := p.store.GetConnection(ctx, channel)
		if err == nil && conn != nil {
			platform = conn.Platform
			if conn.AgentID != "" {
				agentID = conn.AgentID
				log.Printf("pipeline: connection %s bound to agent %q", channel, agentID)
			} else {
				// Unbound connection — refuse to respond.
				log.Printf("pipeline: connection %s has no agent bound, ignoring message", channel)
				return
			}
		} else {
			log.Printf("pipeline: GetConnection(%s) failed: %v", channel, err)
		}
	}

	// If agentID is still "default" or empty, try LoopPool configs first, then DB fallback.
	if agentID == "default" || agentID == "" {
		// Check if the LoopPool actually has a "default" config — if so, keep it.
		if agentID == "default" && p.loopPool != nil && p.loopPool.Get("default") != nil {
			log.Printf("pipeline: using pipeline default agent %q (exists in pool)", agentID)
		} else {
			var firstAgent string
			p.store.DB().QueryRow(`SELECT id FROM agents ORDER BY id LIMIT 1`).Scan(&firstAgent)
			log.Printf("pipeline: fallback agent from DB: %q (agentID was %q)", firstAgent, agentID)
			if firstAgent != "" {
				agentID = firstAgent
			}
		}
	}

	log.Printf("pipeline: resolved agentID=%q for %s/%s", agentID, channel, chatID)

	// Channel-type filtering: check if the agent is allowed to serve this channel.
	// Use platform from connection lookup, or fall back to the channel name itself
	// (covers web/cli channels that don't use connections).
	channelType := platform
	if channelType == "" {
		channelType = channel
	}
	if p.agentProvider != nil && channelType != "" {
		if !p.agentProvider.ServesChannel(agentID, channelType) {
			log.Printf("pipeline: agent %s does not serve channel type %s, ignoring", agentID, channelType)
			return
		}
	}

	// Find or create session with kind and thread awareness.
	var sess *store.Session
	var err error

	if threadID != "" {
		sess, err = p.store.FindSessionWithThread(ctx, channel, chatID, threadID)
		if err != nil {
			// Create thread sub-session linked to parent group session.
			sess, err = p.store.CreateSessionWithThread(ctx, channel, chatID, agentID, threadID)
			if err != nil {
				log.Printf("failed to create thread session: %v", err)
				return
			}
		}
	} else {
		sess, err = p.store.FindSessionWithKind(ctx, channel, chatID, kind)
		if err != nil {
			// Try legacy FindSession as fallback for old sessions without kind.
			sess, err = p.store.FindSession(ctx, channel, chatID)
		}
		if err != nil {
			// Create new kind-aware session.
			sess, err = p.store.CreateSessionWithKind(ctx, channel, chatID, agentID, kind)
			if err != nil {
				log.Printf("failed to create session: %v", err)
				return
			}
		} else if sess.AgentID != agentID {
			// Agent binding changed — close old session and start fresh.
			log.Printf("pipeline: agent rebind %s→%s for %s/%s, closing old session %s", sess.AgentID, agentID, channel, chatID, sess.ID)
			p.store.DB().ExecContext(ctx, `UPDATE sessions SET status = 'closed', updated_at = datetime('now') WHERE id = ?`, sess.ID)
			sess, err = p.store.CreateSessionWithKind(ctx, channel, chatID, agentID, kind)
			if err != nil {
				log.Printf("failed to create session after rebind: %v", err)
				return
			}
		}
	}

	// Update session label with agent display name if different from ID.
	agentName := p.resolveAgentName(agentID)
	if agentName != agentID {
		p.store.DB().ExecContext(ctx, `UPDATE sessions SET label = ? WHERE id = ? AND label LIKE ?`,
			agentName+" on "+channel, sess.ID, agentID+" on %")
	}

	// S4: Schedule on the main lane.
	req := RunRequest{
		SessionID: sess.ID,
		AgentID:   agentID,
		Messages:  msgs,
		Lane:      LaneMain,
		HasAudio:  canonical.MessagesHaveAudio(msgs),
	}

	// Voice capability check: if audio but agent can't handle voice,
	// send text rejection instead of scheduling.
	if req.HasAudio && p.agentProvider != nil {
		cfg := p.agentProvider.Get(agentID)
		if cfg == nil || !cfg.HasVoice() {
			agentName := p.resolveAgentName(agentID)
			p.bus.PublishOutbound(ctx, bus.Envelope{
				Channel: channel,
				ChatID:  chatID,
				Kind:    kind,
				Messages: []canonical.Message{{
					Role: "assistant",
					Content: []canonical.Content{{
						Type: "text",
						Text: fmt.Sprintf("%s doesn't support voice messages. Please send a text message instead.", agentName),
					}},
				}},
			})
			return
		}
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

	// S5: Agent loop — select the right loop for this agent.
	loop := p.loopPool.Get(req.AgentID)
	if loop == nil {
		log.Printf("pipeline: no agent loop for %s, skipping", req.AgentID)
		return
	}

	// Dispatch: voice or text path.
	var result agent.RunResult
	if req.HasAudio && loop.CanVoice() && p.liveSessionPool != nil {
		result = loop.RunVoice(ctx, req.SessionID, allMsgs, p.liveSessionPool)
	} else if req.HasAudio {
		// Voice dispatch unavailable — don't fall through to text path with audio.
		reason := "Voice messaging is not available right now."
		if !loop.CanVoice() {
			reason = "This agent's voice capability is not fully configured. Check that a Gemini API key is set and audio tools (ffmpeg) are installed."
		} else if p.liveSessionPool == nil {
			reason = "Voice sessions are not available. Ensure a Gemini API key is configured."
		}
		result = agent.RunResult{
			Messages: []canonical.Message{{
				Role:    "assistant",
				Content: []canonical.Content{{Type: "text", Text: reason}},
			}},
		}
	} else {
		// Pre-warm voice session in background on text messages for voice-capable agents.
		// When the user eventually sends voice, the WebSocket is already connected.
		if p.liveSessionPool != nil && loop.CanVoice() {
			if warmer, ok := p.liveSessionPool.(agent.LiveSessionWarmer); ok {
				if vcfg := loop.VoiceSessionConfig(); vcfg != nil {
					warmer.Warm(ctx, req.SessionID, *vcfg)
				}
			}
		}
		result = loop.Run(ctx, req.SessionID, allMsgs)
	}

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
		if p.loopPool != nil {
			provName, model = p.loopPool.ProviderAndModel(req.AgentID)
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

	// Auto-title: set session title from first user message (if not already set).
	if len(req.Messages) > 0 {
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				text := agent.ExtractText(msg)
				if text != "" {
					title := text
					if len(title) > 80 {
						title = title[:80]
					}
					p.store.UpdateSessionTitle(ctx, req.SessionID, title)
					break
				}
			}
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

	// On error with no response messages, send error feedback to the user.
	if result.Error != nil && len(result.Messages) == 0 {
		log.Printf("[%s] run error: %v", req.SessionID, result.Error)
		errMsg := "Sorry, I encountered an issue processing your request."
		if req.HasAudio {
			errMsg = "Sorry, I couldn't process your voice message. " + result.Error.Error()
		}
		result.Messages = []canonical.Message{{
			Role:    "assistant",
			Content: []canonical.Content{{Type: "text", Text: errMsg}},
		}}
		result.Error = nil // Clear error so the response gets delivered below.
	}

	// Deliver response via outbound bus.
	if result.Error == nil && len(result.Messages) > 0 {
		// Find the last assistant text message.
		for i := len(result.Messages) - 1; i >= 0; i-- {
			if result.Messages[i].Role == "assistant" {
				sess, _ := p.store.GetSession(ctx, req.SessionID)
				chatID := ""
				channel := ""
				kind := "dm"
				if sess != nil {
					chatID = sess.ChatID
					channel = sess.Channel
					if sess.Kind != "" {
						kind = sess.Kind
					}
				}
				p.bus.PublishOutbound(ctx, bus.Envelope{
					SessionID: req.SessionID,
					Channel:   channel,
					ChatID:    chatID,
					Kind:      kind,
					HasAudio:  canonical.HasAudio(result.Messages[i]),
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
