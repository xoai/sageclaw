package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"sync"

	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/auth"
	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/channel"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/tool"
	"github.com/xoai/sageclaw/pkg/tunnel"
	"github.com/xoai/sageclaw/web"
)

// Request is a JSON-RPC request.
type Request struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
	ID     any             `json:"id,omitempty"`
}

// Response is a JSON-RPC response.
type Response struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	ID     any    `json:"id,omitempty"`
}

// Server is the WebSocket RPC server.
type Server struct {
	store          store.Store
	memEngine      memory.MemoryEngine
	graphEngine    memory.GraphEngine
	msgBus         bus.MessageBus
	toolRegistry   *tool.Registry
	auth           *auth.Auth
	httpServer     *http.Server
	clients        map[*wsConn]bool
	mu             sync.RWMutex
	tunnel         *tunnel.Tunnel
	pairing        *security.PairingManager
	budgetEngine   *provider.BudgetEngine
	router         *provider.Router
	chanMgr        *channel.Manager
	agentsDir      string
	encKey         []byte // Credential encryption key.
	startTime      time.Time
	providerHealth map[string]string
}

// wsConn is a minimal WebSocket connection using the standard upgrade.
type wsConn struct {
	// Using http.ResponseWriter with Flusher for SSE-style communication.
	// True WebSocket requires golang.org/x/net or a hand-rolled upgrade.
	// For v0.3, we use Server-Sent Events as a simpler alternative
	// that doesn't require any dependency.
	writer  http.ResponseWriter
	flusher http.Flusher
	done    chan struct{}
}

// Config for the RPC server.
type Config struct {
	ListenAddr string // Default: ":9090"
}

// ServerOption configures optional Server features.
type ServerOption func(*Server)

// WithGraphEngine adds graph capabilities to the server.
func WithGraphEngine(g memory.GraphEngine) ServerOption {
	return func(s *Server) { s.graphEngine = g }
}

// WithToolRegistry adds tool browsing to the server.
func WithToolRegistry(r *tool.Registry) ServerOption {
	return func(s *Server) { s.toolRegistry = r }
}

// WithProviderHealth adds provider health info.
func WithProviderHealth(h map[string]string) ServerOption {
	return func(s *Server) { s.providerHealth = h }
}

// WithTunnel adds tunnel management to the server.
func WithTunnel(t *tunnel.Tunnel) ServerOption {
	return func(s *Server) { s.tunnel = t }
}

// WithAgentsDir sets the agents directory for file-based config.
func WithAgentsDir(dir string) ServerOption {
	return func(s *Server) { s.agentsDir = dir }
}

// WithPairing adds channel pairing management.
func WithPairing(pm *security.PairingManager) ServerOption {
	return func(s *Server) { s.pairing = pm }
}

// WithBudgetEngine adds budget tracking to the server.
func WithBudgetEngine(be *provider.BudgetEngine) ServerOption {
	return func(s *Server) { s.budgetEngine = be }
}

// WithEncryptionKey sets the credential encryption key.
func WithEncryptionKey(key []byte) ServerOption {
	return func(s *Server) { s.encKey = key }
}

// WithRouter adds the model router for hot-reload of providers.
func WithRouter(r *provider.Router) ServerOption {
	return func(s *Server) { s.router = r }
}

// WithChannelManager adds channel hot-reload capability.
func WithChannelManager(m *channel.Manager) ServerOption {
	return func(s *Server) { s.chanMgr = m }
}

// NewServer creates a new RPC server.
func NewServer(s store.Store, mem memory.MemoryEngine, msgBus bus.MessageBus, config Config, opts ...ServerOption) *Server {
	if config.ListenAddr == "" {
		config.ListenAddr = ":9090"
	}

	// Initialize auth.
	authMgr, _ := auth.New(s.DB())

	srv := &Server{
		store:          s,
		memEngine:      mem,
		msgBus:         msgBus,
		auth:           authMgr,
		clients:        make(map[*wsConn]bool),
		startTime:      time.Now(),
		providerHealth: map[string]string{},
	}
	for _, opt := range opts {
		opt(srv)
	}

	mux := http.NewServeMux()

	// Auth (public).
	mux.HandleFunc("GET /api/auth/check", srv.handleAuthCheck)
	mux.HandleFunc("POST /api/auth/setup", srv.handleAuthSetup)
	mux.HandleFunc("POST /api/auth/login", srv.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", srv.handleAuthLogout)

	// Status (public for health checks).
	mux.HandleFunc("GET /api/status", srv.authGuard(srv.handleStatus))

	// Agents (authenticated).
	mux.HandleFunc("GET /api/agents", srv.authGuard(srv.handleAgentsList))
	mux.HandleFunc("POST /api/agents", srv.authGuard(srv.handleAgentsCreate))
	mux.HandleFunc("PUT /api/agents/", srv.authGuard(srv.handleAgentsUpdate))
	mux.HandleFunc("DELETE /api/agents/", srv.authGuard(srv.handleAgentsDelete))

	// Providers (authenticated).
	mux.HandleFunc("GET /api/providers", srv.authGuard(srv.handleProvidersList))
	mux.HandleFunc("POST /api/providers", srv.authGuard(srv.handleProvidersCreate))
	mux.HandleFunc("DELETE /api/providers/", srv.authGuard(srv.handleProvidersDelete))
	mux.HandleFunc("GET /api/providers/models", srv.authGuard(srv.handleProvidersModels))

	// Combos (authenticated).
	mux.HandleFunc("GET /api/combos", srv.authGuard(srv.handleCombosList))
	mux.HandleFunc("POST /api/combos", srv.authGuard(srv.handleCombosCreate))
	mux.HandleFunc("DELETE /api/combos/", srv.authGuard(srv.handleCombosDelete))

	// Channels (authenticated).
	mux.HandleFunc("GET /api/channels", srv.authGuard(srv.handleChannelsList))
	mux.HandleFunc("POST /api/channels/configure", srv.authGuard(srv.handleChannelConfigure))

	// Teams (authenticated).
	mux.HandleFunc("GET /api/teams", srv.authGuard(srv.handleTeamsList))
	mux.HandleFunc("POST /api/teams", srv.authGuard(srv.handleTeamsCreate))
	mux.HandleFunc("PUT /api/teams/", srv.authGuard(srv.handleTeamsUpdate))
	mux.HandleFunc("DELETE /api/teams/", srv.authGuard(srv.handleTeamsDelete))
	mux.HandleFunc("GET /api/teams/tasks/", srv.authGuard(srv.handleTeamsTasks))

	// Skills (authenticated).
	mux.HandleFunc("GET /api/skills", srv.authGuard(srv.handleSkillsList))

	// Settings (authenticated).
	mux.HandleFunc("PUT /api/settings/password", srv.authGuard(srv.handleSettingsPassword))
	mux.HandleFunc("GET /api/settings/export", srv.authGuard(srv.handleSettingsExport))
	mux.HandleFunc("POST /api/settings/import", srv.authGuard(srv.handleSettingsImport))

	// Cron (authenticated).
	mux.HandleFunc("GET /api/cron", srv.authGuard(srv.handleCronList))
	mux.HandleFunc("POST /api/cron", srv.authGuard(srv.handleCronCreate))
	mux.HandleFunc("DELETE /api/cron/", srv.authGuard(srv.handleCronDelete))
	mux.HandleFunc("POST /api/cron/", srv.authGuard(srv.handleCronTrigger))

	// Delegation (authenticated).
	mux.HandleFunc("GET /api/delegation/links", srv.authGuard(srv.handleDelegationLinks))
	mux.HandleFunc("POST /api/delegation/links", srv.authGuard(srv.handleDelegationLinksCreate))
	mux.HandleFunc("DELETE /api/delegation/links/", srv.authGuard(srv.handleDelegationLinksDelete))
	mux.HandleFunc("GET /api/delegation/history", srv.authGuard(srv.handleDelegationHistory))

	// Memory CRUD (authenticated).
	mux.HandleFunc("GET /api/memory/", srv.authGuard(srv.handleMemoryGet))
	mux.HandleFunc("PUT /api/memory/", srv.authGuard(srv.handleMemoryUpdate))
	mux.HandleFunc("DELETE /api/memory/", srv.authGuard(srv.handleMemoryDelete))

	// Graph (authenticated).
	mux.HandleFunc("GET /api/graph/", srv.authGuard(srv.handleGraphGet))
	mux.HandleFunc("POST /api/graph/link", srv.authGuard(srv.handleGraphLink))
	mux.HandleFunc("DELETE /api/graph/link", srv.authGuard(srv.handleGraphUnlink))

	// Audit (authenticated).
	mux.HandleFunc("GET /api/audit", srv.authGuard(srv.handleAuditQuery))
	mux.HandleFunc("GET /api/audit/stats", srv.authGuard(srv.handleAuditStats))

	// Tools (authenticated).
	mux.HandleFunc("GET /api/tools", srv.authGuard(srv.handleToolsList))

	// Credentials (authenticated).
	mux.HandleFunc("GET /api/credentials", srv.authGuard(srv.handleCredentialsList))
	mux.HandleFunc("POST /api/credentials", srv.authGuard(srv.handleCredentialsStore))
	mux.HandleFunc("DELETE /api/credentials/", srv.authGuard(srv.handleCredentialsDelete))

	// Skill management (authenticated).
	mux.HandleFunc("POST /api/skills/install", srv.authGuard(srv.handleSkillInstall))
	mux.HandleFunc("POST /api/skills/reload", srv.authGuard(srv.handleSkillReload))
	mux.HandleFunc("DELETE /api/skills/", srv.authGuard(srv.handleSkillDelete))

	// Templates (authenticated).
	mux.HandleFunc("GET /api/templates", srv.authGuard(srv.handleTemplatesList))
	mux.HandleFunc("POST /api/templates/apply", srv.authGuard(srv.handleTemplatesApply))

	// Sessions management (authenticated).
	mux.HandleFunc("DELETE /api/sessions/", srv.authGuard(srv.handleSessionDelete))
	mux.HandleFunc("POST /api/sessions/", srv.authGuard(srv.handleSessionArchive))

	// Agent config v2 — file-based (authenticated).
	mux.HandleFunc("GET /api/v2/agents", srv.authGuard(srv.handleAgentsListV2))
	mux.HandleFunc("POST /api/v2/agents", srv.authGuard(srv.handleAgentCreateV2))
	mux.HandleFunc("GET /api/v2/agents/", srv.authGuard(srv.handleAgentGetFull))
	mux.HandleFunc("PUT /api/v2/agents/", srv.authGuard(srv.handleAgentUpdateV2))
	mux.HandleFunc("DELETE /api/v2/agents/", srv.authGuard(srv.handleAgentDeleteV2))
	mux.HandleFunc("GET /api/v2/agents/tools", srv.authGuard(srv.handleAgentTools))

	// Pairing (authenticated).
	mux.HandleFunc("POST /api/pairing/generate", srv.authGuard(srv.handlePairingGenerate))
	mux.HandleFunc("GET /api/pairing", srv.authGuard(srv.handlePairingList))
	mux.HandleFunc("GET /api/pairing/status", srv.authGuard(srv.handlePairingStatus))
	mux.HandleFunc("DELETE /api/pairing/", srv.authGuard(srv.handlePairingDelete))

	// Tunnel (authenticated).
	mux.HandleFunc("GET /api/tunnel/status", srv.authGuard(srv.handleTunnelStatus))
	mux.HandleFunc("POST /api/tunnel/start", srv.authGuard(srv.handleTunnelStart))
	mux.HandleFunc("POST /api/tunnel/stop", srv.authGuard(srv.handleTunnelStop))

	// MCP transports (authenticated).
	if srv.toolRegistry != nil {
		mcpSSE := mcp.NewSSEServer(srv.toolRegistry)
		mux.HandleFunc("GET /mcp/sse", srv.authGuard(mcpSSE.HandleSSE))
		mux.HandleFunc("POST /mcp/messages", srv.authGuard(mcpSSE.HandleMessages))
		mcpHTTP := mcp.NewHTTPServer(srv.toolRegistry)
		mux.HandleFunc("POST /mcp", srv.authGuard(mcpHTTP.HandleRequest))
	}

	// Budget (authenticated).
	mux.HandleFunc("GET /api/budget/summary", srv.authGuard(srv.handleBudgetSummary))
	mux.HandleFunc("GET /api/budget/config", srv.authGuard(srv.handleBudgetConfig))
	mux.HandleFunc("PUT /api/budget/config", srv.authGuard(srv.handleBudgetConfigUpdate))
	mux.HandleFunc("GET /api/budget/history", srv.authGuard(srv.handleBudgetHistory))
	mux.HandleFunc("GET /api/budget/alerts", srv.authGuard(srv.handleBudgetAlerts))
	mux.HandleFunc("GET /api/budget/alerts/unread", srv.authGuard(srv.handleBudgetAlertsUnread))
	mux.HandleFunc("POST /api/budget/alerts/", srv.authGuard(srv.handleBudgetAlertAck))
	mux.HandleFunc("GET /api/budget/top-models", srv.authGuard(srv.handleBudgetTopModels))

	// Health (public).
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	// Existing RPC + SSE.
	mux.HandleFunc("POST /rpc", srv.authGuard(srv.handleRPC))
	mux.HandleFunc("GET /events", srv.authGuard(srv.handleSSE))

	// Serve embedded dashboard.
	distFS, err := fs.Sub(web.DistFS, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(distFS))
		mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
			// SPA fallback: serve index.html for unmatched routes.
			path := r.URL.Path
			if path != "/" {
				// Try to serve the actual file first.
				if _, err := fs.Stat(distFS, path[1:]); err != nil {
					// File not found — serve index.html for SPA routing.
					r.URL.Path = "/"
				}
			}
			fileServer.ServeHTTP(w, r)
		})
	}

	srv.httpServer = &http.Server{
		Addr:    config.ListenAddr,
		Handler: mux,
	}

	return srv
}

// Start begins the RPC server.
func (s *Server) Start(ctx context.Context) error {
	go func() {
		log.Printf("rpc: listening on %s", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("rpc: server error: %v", err)
		}
	}()
	return nil
}

// Stop shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

// EventHandler returns an agent.EventHandler that broadcasts to SSE clients.
func (s *Server) EventHandler() agent.EventHandler {
	return func(e agent.Event) {
		data, _ := json.Marshal(map[string]any{
			"type":       e.Type,
			"session_id": e.SessionID,
			"agent_id":   e.AgentID,
			"text":       e.Text,
			"iteration":  e.Iteration,
		})
		s.broadcast(data)
	}
}

func (s *Server) broadcast(data []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for client := range s.clients {
		select {
		case <-client.done:
			continue
		default:
		}
		// SSE format.
		fmt.Fprintf(client.writer, "data: %s\n\n", data)
		client.flusher.Flush()
	}
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	conn := &wsConn{writer: w, flusher: flusher, done: make(chan struct{})}

	s.mu.Lock()
	s.clients[conn] = true
	s.mu.Unlock()

	// Send heartbeat every 15s to prevent connection timeout.
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				flusher.Flush()
			case <-r.Context().Done():
				return
			case <-conn.done:
				return
			}
		}
	}()

	// Keep connection open until client disconnects.
	<-r.Context().Done()

	s.mu.Lock()
	delete(s.clients, conn)
	close(conn.done)
	s.mu.Unlock()
}

func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response{Error: "invalid request", ID: nil})
		return
	}

	result, err := s.dispatch(r.Context(), req)
	if err != nil {
		writeJSON(w, Response{Error: err.Error(), ID: req.ID})
		return
	}
	writeJSON(w, Response{Result: result, ID: req.ID})
}

func (s *Server) dispatch(ctx context.Context, req Request) (any, error) {
	switch req.Method {
	case "sessions.list":
		return s.sessionsList(ctx, req.Params)
	case "sessions.get":
		return s.sessionsGet(ctx, req.Params)
	case "sessions.messages":
		return s.sessionsMessages(ctx, req.Params)
	case "memory.search":
		return s.memorySearch(ctx, req.Params)
	case "memory.list":
		return s.memoryList(ctx, req.Params)
	case "chat.send":
		return s.chatSend(ctx, req.Params)
	default:
		return nil, fmt.Errorf("unknown method: %s", req.Method)
	}
}

func (s *Server) sessionsList(ctx context.Context, params json.RawMessage) (any, error) {
	rows, err := s.store.DB().QueryContext(ctx,
		`SELECT id, channel, chat_id, agent_id, created_at, updated_at FROM sessions ORDER BY updated_at DESC LIMIT 20`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []map[string]any
	for rows.Next() {
		var id, channel, chatID, agentID, createdAt, updatedAt string
		rows.Scan(&id, &channel, &chatID, &agentID, &createdAt, &updatedAt)
		sessions = append(sessions, map[string]any{
			"id": id, "channel": channel, "chat_id": chatID,
			"agent_id": agentID, "created_at": createdAt, "updated_at": updatedAt,
		})
	}
	return sessions, nil
}

func (s *Server) sessionsGet(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct{ ID string `json:"id"` }
	json.Unmarshal(params, &p)
	return s.store.GetSession(ctx, p.ID)
}

func (s *Server) sessionsMessages(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		ID    string `json:"id"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal(params, &p)
	if p.Limit == 0 {
		p.Limit = 50
	}
	return s.store.GetMessages(ctx, p.ID, p.Limit)
}

func (s *Server) memorySearch(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	json.Unmarshal(params, &p)
	if p.Limit == 0 {
		p.Limit = 10
	}
	return s.memEngine.Search(ctx, p.Query, memory.SearchOptions{Limit: p.Limit})
}

func (s *Server) memoryList(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Tags  []string `json:"tags"`
		Limit int      `json:"limit"`
	}
	json.Unmarshal(params, &p)
	if p.Limit == 0 {
		p.Limit = 20
	}
	return s.memEngine.List(ctx, p.Tags, p.Limit, 0)
}

func (s *Server) chatSend(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		Channel string `json:"channel"`
		Text    string `json:"text"`
		AgentID string `json:"agent_id"`
	}
	json.Unmarshal(params, &p)
	if p.Channel == "" {
		p.Channel = "web"
	}

	envelope := bus.Envelope{
		Channel: p.Channel,
		ChatID:  "web-client",
		Messages: []canonical.Message{
			{Role: "user", Content: []canonical.Content{{Type: "text", Text: p.Text}}},
		},
	}

	// If agent_id specified, include it in metadata so the pipeline routes correctly.
	if p.AgentID != "" {
		envelope.AgentID = p.AgentID
	}

	s.msgBus.PublishInbound(ctx, envelope)
	return map[string]string{"status": "sent"}, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

