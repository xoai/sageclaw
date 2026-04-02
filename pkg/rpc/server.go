package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"sync"

	"time"

	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/agentcfg"
	"github.com/xoai/sageclaw/pkg/auth"
	"github.com/xoai/sageclaw/pkg/bus"
	"github.com/xoai/sageclaw/pkg/channel"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/security"
	mcpregistry "github.com/xoai/sageclaw/pkg/mcp/registry"
	"github.com/xoai/sageclaw/pkg/skillstore"
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
	tunnel         *tunnel.Client
	pairing        *security.PairingManager
	budgetEngine   *provider.BudgetEngine
	modelRegistry  *provider.ModelRegistry
	router         *provider.Router
	chanMgr        *channel.Manager
	mcpMgr         *mcp.Manager
	consentHandler func(nonce string, granted bool, tier string) error // Nonce-based consent callback.
	consentStore         consentGrantStore                                  // For grant list/revoke endpoints.
	pendingConsent []map[string]any                // Queued consent prompts awaiting response.
	skillStore     *skillstore.Store
	mcpRegistry    *mcpregistry.Registry
	agentsDir      string
	encKey         []byte // Credential encryption key.
	startTime      time.Time
	providerHealth map[string]string
	audioBasePath  string // Base path for audio file serving (e.g. "data/audio").
	totp           *auth.TOTP
	loginLimiter   *auth.LoginLimiter
	pendingTOTP    map[string]pendingTOTPEntry // nonce → entry
	loopPool       *agent.LoopPool
	teamReloadFunc func() // Called after team create/update/delete to hot-reload team config.
	workspace      string // Workspace root for file uploads.
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

// WithWorkspace sets the workspace root for file uploads.
func WithWorkspace(path string) ServerOption {
	return func(s *Server) { s.workspace = path }
}

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
func WithTunnel(t *tunnel.Client) ServerOption {
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

// WithModelRegistry adds model pricing registry to the server.
func WithModelRegistry(mr *provider.ModelRegistry) ServerOption {
	return func(s *Server) { s.modelRegistry = mr }
}

// WithEncryptionKey sets the credential encryption key.
func WithEncryptionKey(key []byte) ServerOption {
	return func(s *Server) {
		s.encKey = key
		// Update TOTP encryption key if TOTP was already created.
		if s.totp != nil {
			s.totp = auth.NewTOTP(s.store.DB(), key)
		}
	}
}

// WithRouter adds the model router for hot-reload of providers.
func WithRouter(r *provider.Router) ServerOption {
	return func(s *Server) { s.router = r }
}

// WithChannelManager adds channel hot-reload capability.
func WithChannelManager(m *channel.Manager) ServerOption {
	return func(s *Server) { s.chanMgr = m }
}

// WithMCPManager adds MCP server management to the RPC server.
func WithMCPManager(m *mcp.Manager) ServerOption {
	return func(s *Server) { s.mcpMgr = m }
}

// consentGrantStore is the interface for consent grant CRUD (subset of PersistentConsentStore).
type consentGrantStore interface {
	ListGrants(ownerID, platform string) ([]tool.ConsentGrant, error)
	RevokeByID(id string) error
}

// WithConsentStore sets the consent grant store for listing/revoking persistent grants.
func WithConsentStore(cs consentGrantStore) ServerOption {
	return func(s *Server) { s.consentStore = cs }
}

// WithConsentHandler sets the nonce-based consent callback.
func WithConsentHandler(fn func(nonce string, granted bool, tier string) error) ServerOption {
	return func(s *Server) { s.consentHandler = fn }
}

// WithSkillStore adds skill marketplace management to the server.
func WithSkillStore(ss *skillstore.Store) ServerOption {
	return func(s *Server) { s.skillStore = ss }
}

// WithMCPRegistry adds MCP marketplace registry to the server.
func WithMCPRegistry(reg *mcpregistry.Registry) ServerOption {
	return func(s *Server) { s.mcpRegistry = reg }
}

// WithAudioBasePath sets the base path for serving audio files.
func WithAudioBasePath(path string) ServerOption {
	return func(s *Server) { s.audioBasePath = path }
}

// pendingTOTPEntry stores a verified-password session awaiting TOTP completion.
type pendingTOTPEntry struct {
	expiresAt time.Time
}

// WireTunnelAuth sets up session invalidation: rotates the JWT secret
// on the first tunnel ready event (not on reconnects). This forces all
// existing sessions to re-login, enforcing TOTP if enabled.
// Reconnects reuse the same secret — no spurious logouts on transient disconnects.
func (s *Server) WireTunnelAuth() {
	if s.tunnel == nil || s.auth == nil {
		return
	}
	var rotated sync.Once
	s.tunnel.OnReady(func(url string) {
		rotated.Do(func() {
			if err := s.auth.RotateSecret(); err != nil {
				log.Printf("tunnel: failed to rotate JWT secret: %v", err)
			} else {
				log.Printf("tunnel: JWT secret rotated — existing sessions invalidated")
			}
		})
	})
}

// WireTunnelWebhooks sets up auto-webhook registration: when the tunnel
// becomes ready, iterates webhook-needing connections and logs the URL
// for each adapter that implements WebhookURLUpdater.
func (s *Server) WireTunnelWebhooks() {
	if s.tunnel == nil || s.chanMgr == nil {
		return
	}
	s.tunnel.OnReady(func(url string) {
		conns, err := s.store.ListConnections(context.Background(), store.ConnectionFilter{Status: "active"})
		if err != nil {
			log.Printf("tunnel: auto-webhook: failed to list connections: %v", err)
			return
		}
		for _, conn := range conns {
			if conn.Platform != "whatsapp" && conn.Platform != "zalo" && conn.Platform != "zalobot" {
				continue
			}
			ch := s.chanMgr.GetChannel(conn.ID)
			if ch == nil {
				continue
			}
			if updater, ok := ch.(channel.WebhookURLUpdater); ok {
				webhookURL := url + "/webhook/" + conn.Platform
				if err := updater.UpdateWebhookURL(context.Background(), webhookURL); err != nil {
					log.Printf("tunnel: auto-webhook failed for %s (%s): %v", conn.ID, conn.Platform, err)
				}
			}
		}
	})
}

// WithTOTP adds TOTP management to the server.
func WithTOTP(t *auth.TOTP) ServerOption {
	return func(s *Server) { s.totp = t }
}

// WithLoginLimiter adds login rate limiting to the server.
func WithLoginLimiter(l *auth.LoginLimiter) ServerOption {
	return func(s *Server) { s.loginLimiter = l }
}

// WithLoopPool adds the agent loop pool for live config updates.
func WithLoopPool(lp *agent.LoopPool) ServerOption {
	return func(s *Server) { s.loopPool = lp }
}

// WithTeamReload sets a callback invoked after team create/update/delete.
// The callback should reload team configs into the agent loop pool.
func WithTeamReload(fn func()) ServerOption {
	return func(s *Server) { s.teamReloadFunc = fn }
}

// NewServer creates a new RPC server.
func NewServer(s store.Store, mem memory.MemoryEngine, msgBus bus.MessageBus, config Config, opts ...ServerOption) *Server {
	if config.ListenAddr == "" {
		config.ListenAddr = ":9090"
	}

	// Initialize auth — fail closed if auth setup errors.
	authMgr, authErr := auth.New(s.DB())
	if authErr != nil {
		log.Printf("WARNING: auth initialization failed: %v — all endpoints will require auth setup", authErr)
	}

	srv := &Server{
		store:          s,
		memEngine:      mem,
		msgBus:         msgBus,
		auth:           authMgr,
		totp:           auth.NewTOTP(s.DB(), nil), // encKey set via WithEncryptionKey
		loginLimiter:   auth.NewLoginLimiter(),
		pendingTOTP:    make(map[string]pendingTOTPEntry),
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
	mux.HandleFunc("POST /api/auth/login/totp", srv.handleAuthLoginTOTP)
	mux.HandleFunc("POST /api/auth/logout", srv.handleAuthLogout)
	mux.HandleFunc("POST /api/auth/totp/setup", srv.authGuard(srv.handleTOTPSetup))
	mux.HandleFunc("POST /api/auth/totp/disable", srv.authGuard(srv.handleTOTPDisable))

	// Status (public for health checks).
	mux.HandleFunc("GET /api/status", srv.authGuard(srv.handleStatus))

	// Agents (authenticated).
	mux.HandleFunc("GET /api/agents", srv.authGuard(srv.handleAgentsList))
	mux.HandleFunc("POST /api/agents", srv.authGuard(srv.handleAgentsCreate))
	mux.HandleFunc("PUT /api/agents/", srv.authGuard(srv.handleAgentsUpdate))
	mux.HandleFunc("DELETE /api/agents/", srv.authGuard(srv.handleAgentsDelete))
	mux.HandleFunc("GET /api/agents/{id}/examples", srv.authGuard(srv.handleAgentExamples))

	// Providers (authenticated).
	mux.HandleFunc("GET /api/providers", srv.authGuard(srv.handleProvidersList))
	mux.HandleFunc("POST /api/providers", srv.authGuard(srv.handleProvidersCreate))
	mux.HandleFunc("DELETE /api/providers/", srv.authGuard(srv.handleProvidersDelete))
	mux.HandleFunc("PATCH /api/providers/", srv.authGuard(srv.handleProvidersUpdateConfig))
	mux.HandleFunc("GET /api/providers/models", srv.authGuard(srv.handleProvidersModels))
	mux.HandleFunc("GET /api/providers/models/live", srv.authGuard(srv.handleProvidersModelsLive))
	mux.HandleFunc("POST /api/providers/models/refresh", srv.authGuard(srv.handleProvidersModelsRefresh))

	// File upload (authenticated).
	mux.HandleFunc("POST /api/upload", srv.authGuard(srv.handleFileUpload))

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
	mux.HandleFunc("POST /api/teams/tasks/", srv.authGuard(srv.handleTeamsTaskAction))
	mux.HandleFunc("GET /api/teams/resolve-task", srv.authGuard(srv.handleTaskResolve))
	mux.HandleFunc("GET /api/teams/attention", srv.authGuard(srv.handleTeamsAttentionCount))
	mux.HandleFunc("GET /api/teams/task-comments/", srv.authGuard(srv.handleTeamsTaskComments))
	mux.HandleFunc("POST /api/teams/task-comments/", srv.authGuard(srv.handleTeamsTaskComments))

	// Skills (authenticated).
	mux.HandleFunc("GET /api/skills", srv.authGuard(srv.handleSkillsList))

	// Settings (authenticated).
	mux.HandleFunc("PUT /api/settings/password", srv.authGuard(srv.handleSettingsPassword))
	mux.HandleFunc("GET /api/settings/export", srv.authGuard(srv.handleSettingsExport))
	mux.HandleFunc("POST /api/settings/import", srv.authGuard(srv.handleSettingsImport))
	mux.HandleFunc("GET /api/settings/utility-model", srv.authGuard(srv.handleGetUtilityModel))
	mux.HandleFunc("PUT /api/settings/utility-model", srv.authGuard(srv.handleSetUtilityModel))

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
	mux.HandleFunc("GET /api/tools/profiles", srv.authGuard(srv.handleToolProfiles))
	mux.HandleFunc("GET /api/tools/groups", srv.authGuard(srv.handleToolGroups))

	// MCP servers (authenticated).
	mux.HandleFunc("GET /api/mcp/servers", srv.authGuard(srv.handleMCPServersList))
	mux.HandleFunc("POST /api/mcp/servers", srv.authGuard(srv.handleMCPServersAdd))
	mux.HandleFunc("DELETE /api/mcp/servers/", srv.authGuard(srv.handleMCPServersRemove))

	// MCP marketplace (authenticated).
	mux.HandleFunc("GET /api/mcp/marketplace/categories", srv.authGuard(srv.handleMCPMarketCategories))
	mux.HandleFunc("GET /api/mcp/marketplace/list", srv.authGuard(srv.handleMCPMarketList))
	mux.HandleFunc("GET /api/mcp/marketplace/detail/", srv.authGuard(srv.handleMCPMarketDetail))
	mux.HandleFunc("POST /api/mcp/marketplace/install", srv.authGuard(srv.handleMCPMarketInstall))
	mux.HandleFunc("POST /api/mcp/marketplace/retry", srv.authGuard(srv.handleMCPMarketRetry))
	mux.HandleFunc("POST /api/mcp/marketplace/enable", srv.authGuard(srv.handleMCPMarketEnable))
	mux.HandleFunc("POST /api/mcp/marketplace/disable", srv.authGuard(srv.handleMCPMarketDisable))
	mux.HandleFunc("POST /api/mcp/marketplace/remove", srv.authGuard(srv.handleMCPMarketRemove))
	mux.HandleFunc("POST /api/mcp/marketplace/test", srv.authGuard(srv.handleMCPMarketTest))
	mux.HandleFunc("GET /api/mcp/marketplace/search", srv.authGuard(srv.handleMCPMarketSearch))
	mux.HandleFunc("POST /api/mcp/marketplace/assign", srv.authGuard(srv.handleMCPMarketAssign))
	mux.HandleFunc("POST /api/mcp/marketplace/update", srv.authGuard(srv.handleMCPMarketUpdate))

	// Consent (authenticated).
	mux.HandleFunc("POST /api/consent", srv.authGuard(srv.handleConsentResponse))
	mux.HandleFunc("GET /api/consent/pending", srv.authGuard(srv.handleConsentPending))
	mux.HandleFunc("GET /api/consent/grants", srv.authGuard(srv.handleConsentGrants))
	mux.HandleFunc("DELETE /api/consent/grants/", srv.authGuard(srv.handleConsentRevokeGrant))

	// Credentials (authenticated).
	mux.HandleFunc("GET /api/credentials", srv.authGuard(srv.handleCredentialsList))
	mux.HandleFunc("POST /api/credentials", srv.authGuard(srv.handleCredentialsStore))
	mux.HandleFunc("DELETE /api/credentials/", srv.authGuard(srv.handleCredentialsDelete))

	// Skill management (authenticated).
	mux.HandleFunc("POST /api/skills/install", srv.authGuard(srv.handleSkillInstall))
	mux.HandleFunc("POST /api/skills/upload", srv.authGuard(srv.handleSkillUpload))
	mux.HandleFunc("POST /api/skills/upload/approve", srv.authGuard(srv.handleSkillUploadApprove))
	mux.HandleFunc("POST /api/skills/reload", srv.authGuard(srv.handleSkillReload))
	mux.HandleFunc("DELETE /api/skills/", srv.authGuard(srv.handleSkillDelete))

	// Skill marketplace (authenticated).
	mux.HandleFunc("GET /api/skills/marketplace/search", srv.authGuard(srv.handleSkillMarketplaceSearch))
	mux.HandleFunc("GET /api/skills/marketplace/preview", srv.authGuard(srv.handleSkillMarketplacePreview))
	mux.HandleFunc("POST /api/skills/marketplace/install", srv.authGuard(srv.handleSkillMarketplaceInstall))
	mux.HandleFunc("GET /api/skills/marketplace/installed", srv.authGuard(srv.handleSkillMarketplaceInstalled))
	mux.HandleFunc("POST /api/skills/marketplace/update/", srv.authGuard(srv.handleSkillMarketplaceUpdate))
	mux.HandleFunc("GET /api/skills/marketplace/updates", srv.authGuard(srv.handleSkillMarketplaceUpdates))
	mux.HandleFunc("PUT /api/skills/marketplace/assign/", srv.authGuard(srv.handleSkillMarketplaceAssign))
	mux.HandleFunc("DELETE /api/skills/marketplace/", srv.authGuard(srv.handleSkillMarketplaceUninstall))

	// Templates (authenticated).
	mux.HandleFunc("GET /api/templates", srv.authGuard(srv.handleTemplatesList))
	mux.HandleFunc("POST /api/templates/apply", srv.authGuard(srv.handleTemplatesApply))

	// Sessions management (authenticated).
	mux.HandleFunc("DELETE /api/sessions/", srv.authGuard(srv.handleSessionDelete))
	mux.HandleFunc("POST /api/sessions/", srv.authGuard(srv.handleSessionArchive))

	// Agent form schemas + presets (authenticated).
	mux.HandleFunc("GET /api/v2/agents/schemas", srv.authGuard(srv.handleSchemasList))
	mux.HandleFunc("GET /api/v2/agents/schemas/", srv.authGuard(srv.handleSchemaGet))
	mux.HandleFunc("GET /api/v2/agents/presets", srv.authGuard(srv.handlePresetsList))
	mux.HandleFunc("POST /api/v2/agents/presets/", srv.authGuard(srv.handlePresetsApply))
	mux.HandleFunc("POST /api/v2/agents/generate", srv.authGuard(srv.handleAgentGenerate))
	mux.HandleFunc("POST /api/v2/agents/quick-create", srv.authGuard(srv.handleAgentQuickCreate))
	mux.HandleFunc("POST /api/v2/agents/avatar", srv.authGuard(srv.handleAvatarGenerate))

	// Connections v2 — multi-channel (authenticated).
	mux.HandleFunc("GET /api/v2/connections", srv.authGuard(srv.handleConnectionsList))
	mux.HandleFunc("POST /api/v2/connections", srv.authGuard(srv.handleConnectionCreate))
	mux.HandleFunc("PUT /api/v2/connections/", srv.authGuard(srv.handleConnectionUpdate))
	mux.HandleFunc("DELETE /api/v2/connections/", srv.authGuard(srv.handleConnectionDelete))

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
	mux.HandleFunc("GET /api/budget/pricing", srv.authGuard(srv.handleBudgetPricingList))
	mux.HandleFunc("PUT /api/budget/pricing", srv.authGuard(srv.handleBudgetPricingOverride))
	mux.HandleFunc("DELETE /api/budget/pricing/", srv.authGuard(srv.handleBudgetPricingDelete))

	// Audio file serving (authenticated).
	mux.HandleFunc("GET /api/audio/", srv.authGuard(srv.handleAudioServe))

	// Health (public).
	mux.HandleFunc("GET /api/health", srv.handleHealth)

	// Existing RPC + SSE.
	mux.HandleFunc("POST /rpc", srv.authGuard(srv.handleRPC))
	mux.HandleFunc("GET /events", srv.authGuard(srv.handleSSE))

	// Register webhook routes for channel adapters (Zalo, WhatsApp, etc.).
	if srv.chanMgr != nil {
		srv.chanMgr.SetWebhookMux(mux)
	}

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
		Handler: recoveryHandler(mux),
	}

	return srv
}

// recoveryHandler wraps an HTTP handler with panic recovery.
// Without this, a panic in any handler kills the goroutine and leaves
// the connection hanging (browser shows "pending" indefinitely).
func recoveryHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("rpc: panic recovered in %s %s: %v", r.Method, r.URL.Path, err)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"internal server error"}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
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
		payload := map[string]any{
			"type":       e.Type,
			"session_id": e.SessionID,
			"agent_id":   e.AgentID,
			"text":       e.Text,
			"iteration":  e.Iteration,
		}
		if e.Provider != "" {
			payload["provider"] = e.Provider
		}
		if e.Model != "" {
			payload["model"] = e.Model
		}
		if e.TeamData != nil {
			payload["task_id"] = e.TeamData.TaskID
			payload["seq"] = e.TeamData.Seq
			payload["task"] = e.TeamData.Task
		}
		if e.ToolCall != nil {
			payload["tool_call"] = e.ToolCall
		}
		if e.Consent != nil {
			payload["consent"] = e.Consent
			// Queue for polling — SSE may be missed.
			s.mu.Lock()
			s.pendingConsent = append(s.pendingConsent, map[string]any{
				"tool_name":   e.Consent.ToolName,
				"group":       e.Consent.Group,
				"source":      e.Consent.Source,
				"risk_level":  e.Consent.RiskLevel,
				"explanation": e.Consent.Explanation,
				"tool_input":  e.Consent.ToolInput,
				"nonce":       e.Consent.Nonce,
				"agent_id":    e.AgentID,
				"agent_name":  s.resolveAgentName(e.AgentID),
				"session_id":  e.SessionID,
			})
			s.mu.Unlock()
		}
		// Clear pending consent on result (consent may have been handled
		// from any channel — Telegram, CLI, etc — not just the web UI).
		if e.Type == agent.EventConsentResult {
			s.mu.Lock()
			filtered := s.pendingConsent[:0]
			for _, c := range s.pendingConsent {
				// Clear entries matching this agent+session.
				aID, _ := c["agent_id"].(string)
				sID, _ := c["session_id"].(string)
				if aID == e.AgentID && sID == e.SessionID {
					continue // Remove — consent was handled.
				}
				filtered = append(filtered, c)
			}
			s.pendingConsent = filtered
			s.mu.Unlock()
		}
		data, _ := json.Marshal(payload)
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
	case "session.continue":
		return s.sessionContinue(ctx, req.Params)
	default:
		return nil, fmt.Errorf("unknown method: %s", req.Method)
	}
}

// sessionContinue creates a new web session with context from a source session.
// This is the "Continue here" handoff — user sees a Telegram session in the
// dashboard and wants to continue that conversation in the web UI.
func (s *Server) sessionContinue(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		SourceSessionID string `json:"source_session_id"`
		AgentID         string `json:"agent_id"`
	}
	json.Unmarshal(params, &p)
	if p.SourceSessionID == "" {
		return nil, fmt.Errorf("source_session_id required")
	}

	// Load source session.
	source, err := s.store.GetSession(ctx, p.SourceSessionID)
	if err != nil {
		return nil, fmt.Errorf("source session not found: %w", err)
	}
	if p.AgentID == "" {
		p.AgentID = source.AgentID
	}

	// Load source messages (last 50).
	allMsgs, err := s.store.GetMessages(ctx, p.SourceSessionID, 50)
	if err != nil {
		return nil, fmt.Errorf("loading messages: %w", err)
	}

	// Walk backwards collecting complete turns (user + assistant + tool pairs).
	// Stop at 10 turns or 20 messages.
	var contextMsgs []canonical.Message
	turnCount := 0
	for i := len(allMsgs) - 1; i >= 0 && turnCount < 10 && len(contextMsgs) < 20; i-- {
		contextMsgs = append([]canonical.Message{allMsgs[i]}, contextMsgs...)
		if allMsgs[i].Role == "user" {
			turnCount++
		}
	}

	// Ensure we don't break tool_use/tool_result pairs.
	// Walk forward from the start of contextMsgs — if first message is a
	// tool_result without a preceding tool_use, remove it.
	for len(contextMsgs) > 0 {
		hasOrphanResult := false
		for _, c := range contextMsgs[0].Content {
			if c.ToolResult != nil {
				hasOrphanResult = true
				break
			}
		}
		if hasOrphanResult {
			contextMsgs = contextMsgs[1:]
		} else {
			break
		}
	}

	// Create new web session with timestamp suffix.
	ts := fmt.Sprintf("%d", time.Now().Unix())
	chatID := "owner:" + ts

	newSession, err := s.store.CreateSessionWithKind(ctx, "web", chatID, p.AgentID, "direct")
	if err != nil {
		return nil, fmt.Errorf("creating web session: %w", err)
	}

	// Update label to indicate it's a continuation.
	s.store.DB().ExecContext(ctx,
		`UPDATE sessions SET label = ?, spawned_by = ? WHERE id = ?`,
		fmt.Sprintf("Continued from %s", source.Channel), p.SourceSessionID, newSession.ID)

	// Copy context messages into the new session.
	if len(contextMsgs) > 0 {
		// Add a context summary message first.
		summaryMsg := canonical.Message{
			Role: "assistant",
			Content: []canonical.Content{{
				Type: "text",
				Text: fmt.Sprintf("[Context from %s session — %d messages copied]\n\nContinuing the conversation here.",
					source.Channel, len(contextMsgs)),
			}},
		}
		allCopy := append([]canonical.Message{summaryMsg}, contextMsgs...)
		s.store.AppendMessages(ctx, newSession.ID, allCopy)
	}

	return map[string]any{
		"session_id": newSession.ID,
		"agent_id":   p.AgentID,
		"messages":   len(contextMsgs),
		"source":     source.Channel,
	}, nil
}

func (s *Server) sessionsList(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct {
		AgentID string `json:"agent_id"`
		Channel string `json:"channel"`
		Status  string `json:"status"`
		Limit   int    `json:"limit"`
	}
	json.Unmarshal(params, &p)
	if p.Limit <= 0 {
		p.Limit = 50
	}

	query := `SELECT id, COALESCE(key,''), channel, chat_id, agent_id, kind, COALESCE(label,''),
		status, COALESCE(model,''), COALESCE(provider,''), input_tokens, output_tokens,
		compaction_count, message_count, COALESCE(title,''), created_at, updated_at
	 FROM sessions WHERE 1=1`
	var args []any

	if p.AgentID != "" {
		query += ` AND agent_id = ?`
		args = append(args, p.AgentID)
	}
	if p.Channel != "" {
		query += ` AND channel = ?`
		args = append(args, p.Channel)
	}
	if p.Status != "" {
		query += ` AND status = ?`
		args = append(args, p.Status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, p.Limit)

	rows, err := s.store.DB().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []map[string]any
	for rows.Next() {
		var id, key, channel, chatID, agentID, kind, label, status, model, provider, title, createdAt, updatedAt string
		var inputTokens, outputTokens int64
		var compactionCount, messageCount int
		rows.Scan(&id, &key, &channel, &chatID, &agentID, &kind, &label,
			&status, &model, &provider, &inputTokens, &outputTokens,
			&compactionCount, &messageCount, &title, &createdAt, &updatedAt)
		sess := map[string]any{
			"id": id, "key": key, "channel": channel, "chat_id": chatID,
			"agent_id": agentID, "kind": kind, "label": label, "status": status,
			"title": title,
			"model": model, "provider": provider,
			"input_tokens": inputTokens, "output_tokens": outputTokens,
			"compaction_count": compactionCount, "message_count": messageCount,
			"created_at": createdAt, "updated_at": updatedAt,
		}
		if agentID != "" {
			sess["agent_name"] = s.resolveAgentName(agentID)
		}
		sessions = append(sessions, sess)
	}
	if sessions == nil {
		sessions = []map[string]any{}
	}
	return sessions, nil
}

func (s *Server) sessionsGet(ctx context.Context, params json.RawMessage) (any, error) {
	var p struct{ ID string `json:"id"` }
	json.Unmarshal(params, &p)
	sess, err := s.store.GetSession(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	// Wrap session with agent_name for display.
	result := map[string]any{
		"id": sess.ID, "key": sess.Key, "channel": sess.Channel, "chat_id": sess.ChatID,
		"agent_id": sess.AgentID, "kind": sess.Kind, "label": sess.Label, "status": sess.Status,
		"model": sess.Model, "provider": sess.Provider,
		"input_tokens": sess.InputTokens, "output_tokens": sess.OutputTokens,
		"compaction_count": sess.CompactionCount, "message_count": sess.MessageCount,
		"created_at": sess.CreatedAt, "updated_at": sess.UpdatedAt,
		"spawned_by": sess.SpawnedBy,
	}
	if sess.AgentID != "" {
		result["agent_name"] = s.resolveAgentName(sess.AgentID)
	}
	return result, nil
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
		ChatID  string `json:"chat_id"`
		Text    string `json:"text"`
		AgentID string `json:"agent_id"`
	}
	json.Unmarshal(params, &p)
	if p.Channel == "" {
		p.Channel = "web"
	}
	if p.ChatID == "" {
		p.ChatID = "web-client" // Backward compat.
	}
	// Validate chat_id: alphanumeric + dashes, max 64 chars.
	if len(p.ChatID) > 64 {
		return nil, fmt.Errorf("chat_id too long (max 64 chars)")
	}

	// Pre-check: verify the agent serves this channel before publishing.
	if p.AgentID != "" && s.agentsDir != "" && validateAgentID(p.AgentID) {
		if cfg, err := agentcfg.LoadAgent(filepath.Join(s.agentsDir, p.AgentID)); err == nil {
			serve := cfg.Channels.Serve
			if len(serve) > 0 {
				allowed := false
				for _, ch := range serve {
					if ch == p.Channel {
						allowed = true
						break
					}
				}
				if !allowed {
					return nil, fmt.Errorf("agent %q does not serve the %s channel", cfg.Identity.Name, p.Channel)
				}
			}
		}
	}

	envelope := bus.Envelope{
		Channel: p.Channel,
		ChatID:  p.ChatID,
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

// resolveAgentName looks up the display name for an agent ID.
// Checks file-based configs first, then DB agents table, falls back to raw ID.
func (s *Server) resolveAgentName(agentID string) string {
	if s.agentsDir != "" && validateAgentID(agentID) {
		if cfg, err := agentcfg.LoadAgent(filepath.Join(s.agentsDir, agentID)); err == nil && cfg.Identity.Name != "" {
			return cfg.Identity.Name
		}
	}
	var name string
	if err := s.store.DB().QueryRow(`SELECT name FROM agents WHERE id = ?`, agentID).Scan(&name); err == nil && name != "" {
		return name
	}
	return agentID
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

