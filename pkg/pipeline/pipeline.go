package pipeline

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/xoai/sageclaw/pkg/activity"
	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/tool"
	"github.com/xoai/sageclaw/pkg/agentcfg"
	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/store"
)

// CostRecorderFunc is called after each agent run with usage data.
type CostRecorderFunc func(ctx context.Context, sessionID, agentID, providerName, model string, usage canonical.Usage)

// PostRunFunc is called after loop.Run() completes with the context used for the run.
// Used by the team executor to flush pending dispatch queues.
type PostRunFunc func(ctx context.Context, agentID string)

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
	postRun          PostRunFunc           // Optional: called after loop.Run().

	// lastMetadata caches the most recent envelope metadata per composite key,
	// used to thread user_message_id into session metadata for reactions.
	metadataMu   sync.Mutex
	lastMetadata map[string]map[string]string
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
	PostRun         PostRunFunc                         // Optional: called after loop.Run() with context + agentID.
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
		postRun:         config.PostRun,
		lastMetadata:    make(map[string]map[string]string),
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

	_, err := p.nonceManager.Validate(nonce)
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

	// Broadcast to ALL loops (including ephemeral task loops) since the agent
	// may be running in an ephemeral loop not registered under its main ID.
	// The consent token is nonce-specific, so only the waiting loop acts on it.
	p.loopPool.InjectAll(canonical.Message{
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

		// Cache envelope metadata (e.g., telegram_message_id) for userMsgID threading.
		p.metadataMu.Lock()
		if p.lastMetadata[key] == nil {
			p.lastMetadata[key] = make(map[string]string)
		}
		for k, v := range env.Metadata {
			p.lastMetadata[key][k] = v
		}
		// Thread pre-resolved SessionID through debouncer via metadata.
		if env.SessionID != "" {
			p.lastMetadata[key]["_session_id"] = env.SessionID
		}
		p.metadataMu.Unlock()

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

	// If agentID is still "default" or empty, try LoopPool configs.
	if agentID == "default" || agentID == "" {
		if agentID == "default" && p.loopPool != nil && p.loopPool.GetConfig("default") != nil {
			log.Printf("pipeline: using pipeline default agent %q (exists in pool)", agentID)
		} else if p.loopPool != nil {
			// Pick the first available agent from the pool (sorted alphabetically).
			ids := p.loopPool.AgentIDs()
			if len(ids) > 0 {
				agentID = ids[0]
				log.Printf("pipeline: fallback to first available agent %q (original was %q)", agentID, "default")
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

	// Extract envelope metadata early — needed for pre-resolved SessionID and userMsgID.
	compositeKey := channelKey(channel, kind, chatID, threadID, requestAgentID)
	p.metadataMu.Lock()
	envMeta := p.lastMetadata[compositeKey]
	delete(p.lastMetadata, compositeKey) // consumed
	p.metadataMu.Unlock()

	// Check for pre-resolved SessionID (set by RPC chatSend).
	var sess *store.Session
	var err error
	if preSessionID := envMeta["_session_id"]; preSessionID != "" {
		sess, err = p.store.GetSession(ctx, preSessionID)
		if err == nil && sess != nil {
			log.Printf("pipeline: using pre-resolved session %s", preSessionID)
			goto sessionResolved
		}
		log.Printf("pipeline: pre-resolved session %s not found, falling back to find-or-create", preSessionID)
	}

	// Find or create session with kind and thread awareness.
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

sessionResolved:
	// Update session label with agent display name if different from ID.
	agentName := p.resolveAgentName(agentID)
	if agentName != agentID {
		p.store.DB().ExecContext(ctx, `UPDATE sessions SET label = ? WHERE id = ? AND label LIKE ?`,
			agentName+" on "+channel, sess.ID, agentID+" on %")
	}

	// Thread user_message_id from envelope metadata into session metadata.
	if envMeta != nil {
		userMsgID := extractUserMessageID(envMeta)
		if userMsgID != "" {
			p.store.UpdateSessionMetadata(ctx, sess.ID, map[string]string{
				"user_message_id": userMsgID,
			})
		}
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

	// Filter workflow_activity content blocks — these are metadata for the
	// tool timeline display, not conversation content. Must not enter LLM context.
	allMsgs = filterWorkflowActivity(allMsgs)

	// S5: Agent loop — select the right loop for this agent.
	loop := p.loopPool.Get(req.AgentID)
	if loop == nil {
		log.Printf("pipeline: no agent loop for %s, sending error to user", req.AgentID)
		// Send error feedback to user instead of silently dropping.
		if sess, err := p.store.GetSession(ctx, req.SessionID); err == nil && sess != nil {
			p.bus.PublishOutbound(ctx, bus.Envelope{
				Channel: sess.Channel,
				ChatID:  sess.ChatID,
				Kind:    sess.Kind,
				Messages: []canonical.Message{{
					Role: "assistant",
					Content: []canonical.Content{{
						Type: "text",
						Text: fmt.Sprintf("Agent %q is not available. Please check the agent configuration or rebind this channel to an existing agent.", req.AgentID),
					}},
				}},
			})
		}
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
		// Inject pending dispatch queue into context for post-turn dispatch pattern.
		// The team_tasks tool detects this and queues tasks instead of dispatching immediately.
		// Queue is injected for all agents (not just leads) — ttCreate's requireRole("lead")
		// gate ensures only leads push tasks. This avoids coupling pipeline to team role knowledge.
		queue := tool.NewPendingDispatchQueue()
		runCtx := tool.WithPendingDispatch(ctx, queue)
		result = loop.Run(runCtx, req.SessionID, allMsgs)

		// Flush pending dispatch queue after the agent's turn completes.
		if p.postRun != nil && queue.Len() > 0 {
			p.postRun(runCtx, req.AgentID)
		}
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

// extractUserMessageID finds the platform message ID from envelope metadata.
// Each channel adapter stores its platform-specific key (e.g., telegram_message_id).
func extractUserMessageID(meta map[string]string) string {
	for _, key := range []string{
		"telegram_message_id",
		"discord_message_id",
		"whatsapp_message_id",
		"zalo_message_id",
		"zalobot_message_id",
	} {
		if v := meta[key]; v != "" {
			return v
		}
	}
	return ""
}

// filterWorkflowActivity strips workflow_activity content blocks from messages
// before LLM submission. These are metadata for the tool timeline display —
// they must not enter the LLM context window.
func filterWorkflowActivity(msgs []canonical.Message) []canonical.Message {
	var result []canonical.Message
	for _, m := range msgs {
		var filtered []canonical.Content
		for _, c := range m.Content {
			if c.Type != "workflow_activity" {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) > 0 {
			m.Content = filtered
			result = append(result, m)
		}
		// Messages with ONLY workflow_activity content are dropped entirely.
	}
	return result
}
