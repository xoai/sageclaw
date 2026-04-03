package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/xoai/sageclaw/pkg/agent"
	"github.com/xoai/sageclaw/pkg/agentcfg"
	"github.com/xoai/sageclaw/pkg/audio"
	localbus "github.com/xoai/sageclaw/pkg/bus/local"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/channel"
	"github.com/xoai/sageclaw/pkg/channel/cli"
	"github.com/xoai/sageclaw/pkg/channel/discord"
	"github.com/xoai/sageclaw/pkg/channel/telegram"
	"github.com/xoai/sageclaw/pkg/channel/toolstatus"
	"github.com/xoai/sageclaw/pkg/channel/whatsapp"
	"github.com/xoai/sageclaw/pkg/channel/zalo"
	"github.com/xoai/sageclaw/pkg/channel/zalobot"
	"github.com/xoai/sageclaw/pkg/config"
	"github.com/xoai/sageclaw/pkg/mcp"
	mcpregistry "github.com/xoai/sageclaw/pkg/mcp/registry"
	"github.com/xoai/sageclaw/pkg/memory"
	"github.com/xoai/sageclaw/pkg/memory/fts5"
	"github.com/xoai/sageclaw/pkg/middleware"
	"github.com/xoai/sageclaw/pkg/orchestration"
	"github.com/xoai/sageclaw/pkg/pipeline"
	"github.com/xoai/sageclaw/pkg/provider"
	"github.com/xoai/sageclaw/pkg/provider/anthropic"
	"github.com/xoai/sageclaw/pkg/provider/gemini"
	"github.com/xoai/sageclaw/pkg/provider/github"
	"github.com/xoai/sageclaw/pkg/provider/livesession"
	"github.com/xoai/sageclaw/pkg/provider/ollama"
	"github.com/xoai/sageclaw/pkg/provider/openai"
	"github.com/xoai/sageclaw/pkg/provider/openrouter"
	"github.com/xoai/sageclaw/pkg/rpc"
	"github.com/xoai/sageclaw/pkg/security"
	"github.com/xoai/sageclaw/pkg/skill"
	"github.com/xoai/sageclaw/pkg/skillstore"
	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/team"
	"github.com/xoai/sageclaw/pkg/tool"
	"github.com/xoai/sageclaw/pkg/tui"
	"github.com/xoai/sageclaw/pkg/tunnel"
)

const version = "0.4.0-dev"

// pricingStoreAdapter bridges store.Store to provider.PricingStore.
// Avoids provider → store import by adapting at the wiring layer.
type pricingStoreAdapter struct {
	store store.Store
}

func (a *pricingStoreAdapter) GetModelPricing(ctx context.Context, modelID string) (*provider.PricingStoreResult, error) {
	m, err := a.store.GetModelPricing(ctx, modelID)
	if err != nil || m == nil {
		return nil, err
	}
	return &provider.PricingStoreResult{
		InputCost:         m.InputCost,
		OutputCost:        m.OutputCost,
		CacheCost:         m.CacheCost,
		ThinkingCost:      m.ThinkingCost,
		CacheCreationCost: m.CacheCreationCost,
		ContextWindow:     m.ContextWindow,
		MaxOutputTokens:   m.MaxOutputTokens,
		PricingSource:     m.PricingSource,
	}, nil
}

func (a *pricingStoreAdapter) BulkUpdatePricing(ctx context.Context, updates []provider.PricingUpdate) error {
	bulk := make([]store.ModelPricingBulk, len(updates))
	for i, u := range updates {
		bulk[i] = store.ModelPricingBulk{
			ModelID:           u.ModelID,
			Provider:          u.Provider,
			InputCost:         u.InputCost,
			OutputCost:        u.OutputCost,
			CacheCost:         u.CacheCost,
			ThinkingCost:      u.ThinkingCost,
			CacheCreationCost: u.CacheCreationCost,
			ContextWindow:     u.ContextWindow,
			MaxOutputTokens:   u.MaxOutputTokens,
			Source:            u.Source,
		}
	}
	return a.store.BulkUpdateModelPricing(ctx, bulk)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("sageclaw", version)
			return
		case "--help", "-h":
			printHelp()
			return
		case "init":
			tmpl := ""
			initDir := "."
			for _, arg := range os.Args[2:] {
				if strings.HasPrefix(arg, "--template=") {
					tmpl = strings.TrimPrefix(arg, "--template=")
				} else if strings.HasPrefix(arg, "--dir=") {
					initDir = strings.TrimPrefix(arg, "--dir=")
				}
			}
			if err := runInit(tmpl, initDir); err != nil {
				log.Fatalf("init: %v", err)
			}
			return
		case "skill":
			if err := runSkillCommand(os.Args[2:]); err != nil {
				log.Fatalf("skill: %v", err)
			}
			return
		case "mcp":
			if err := runMCPCommand(os.Args[2:]); err != nil {
				log.Fatalf("mcp: %v", err)
			}
			return
		case "tunnel":
			if err := runTunnelCommand(os.Args[2:]); err != nil {
				log.Fatalf("tunnel: %v", err)
			}
			return
		case "auth":
			if err := runAuthCommand(os.Args[2:]); err != nil {
				log.Fatalf("auth: %v", err)
			}
			return
		case "doctor":
			runDoctor()
			return
		case "onboard":
			runOnboard()
			return
		case "upgrade":
			runUpgrade()
			return
		case "backup":
			runBackup()
			return
		case "restore":
			if len(os.Args) < 3 {
				log.Fatal("usage: sageclaw restore <backup-file>")
			}
			runRestore(os.Args[2])
			return
		}
	}

	if err := run(); err != nil {
		log.Fatalf("fatal: %v", err)
	}
}

func printHelp() {
	fmt.Printf(`sageclaw %s — Multi-agent AI framework

Usage: sageclaw [flags]

Flags:
  --version          Show version
  --help, -h         Show this help
  --cli              Force CLI interactive mode (default if no Telegram token)
  --tui              Launch TUI dashboard
  --rpc              Start RPC server (default: off, port 9090)
  --full-access      Enable full access mode (default: standard)
  --workspace PATH   Workspace root (default: current directory)
  --db PATH          Database file path (default: ~/.sageclaw/sageclaw.db)

Environment:
  ANTHROPIC_API_KEY     Anthropic API key
  OPENAI_API_KEY        OpenAI API key
  TELEGRAM_BOT_TOKEN    Telegram bot token
  DISCORD_BOT_TOKEN     Discord bot token
  ZALO_OA_ID            Zalo Official Account ID
  ZALO_SECRET_KEY       Zalo webhook secret key
  ZALO_ACCESS_TOKEN     Zalo API access token
  OLLAMA_BASE_URL       Ollama base URL (default: http://localhost:11434/v1)
  SAGECLAW_DB_PATH      Database file path
  SAGECLAW_WORKSPACE    Workspace root
  SAGECLAW_LANE_MAIN    Max concurrent agent runs (default: 10)
  SAGECLAW_LANE_SUBAGENT Max concurrent subagent runs (default: 10)
  SAGECLAW_SKILLS_DIR   Skills directory (default: ./skills)
  SAGECLAW_RPC_ADDR     RPC server address (default: :9090)

Commands:
  sageclaw init        Initialize a new project from template
  sageclaw doctor      Verify configuration and dependencies
  sageclaw onboard     Interactive setup wizard
  sageclaw backup      Backup the database
  sageclaw restore     Restore from a backup file
  sageclaw skill       Manage skills (install, uninstall)
  sageclaw tunnel      Manage native tunnel for webhook channels
`, version)
}

type flags struct {
	forceCLI   bool
	tui        bool
	rpc        bool
	mcpMode    bool
	fullAccess bool
	workspace  string
	dbPath     string
	configDir  string
}

func parseFlags() flags {
	f := flags{
		workspace: envOrDefault("SAGECLAW_WORKSPACE", "."),
		dbPath:    envOrDefault("SAGECLAW_DB_PATH", defaultDBPath()),
		configDir: envOrDefault("SAGECLAW_CONFIG_DIR", "configs"),
	}
	for _, arg := range os.Args[1:] {
		switch {
		case arg == "--cli":
			f.forceCLI = true
		case arg == "--tui":
			f.tui = true
		case arg == "--rpc":
			f.rpc = true
		case arg == "--mcp":
			f.mcpMode = true
		case arg == "--full-access":
			f.fullAccess = true
		case strings.HasPrefix(arg, "--workspace="):
			f.workspace = strings.TrimPrefix(arg, "--workspace=")
		case strings.HasPrefix(arg, "--db="):
			f.dbPath = strings.TrimPrefix(arg, "--db=")
		case strings.HasPrefix(arg, "--config="):
			f.configDir = strings.TrimPrefix(arg, "--config=")
		}
	}
	return f
}

func run() error {
	f := parseFlags()

	// --- Environment ---
	anthropicKey := os.Getenv("ANTHROPIC_API_KEY")
	openaiKey := os.Getenv("OPENAI_API_KEY")
	geminiKey := os.Getenv("GEMINI_API_KEY")
	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	githubToken := os.Getenv("GITHUB_COPILOT_TOKEN")
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	discordToken := os.Getenv("DISCORD_BOT_TOKEN")
	zaloOAID := os.Getenv("ZALO_OA_ID")
	zaloSecret := os.Getenv("ZALO_SECRET_KEY")
	zaloToken := os.Getenv("ZALO_ACCESS_TOKEN")
	zaloBotToken := os.Getenv("ZALO_BOT_TOKEN")
	waPhoneID := os.Getenv("WHATSAPP_PHONE_NUMBER_ID")
	waAccessToken := os.Getenv("WHATSAPP_ACCESS_TOKEN")
	waVerifyToken := os.Getenv("WHATSAPP_VERIFY_TOKEN")
	skillsDir := envOrDefault("SAGECLAW_SKILLS_DIR", "skills")

	// --- Load config files ---
	cfg, cfgErr := config.Load(f.configDir)
	if cfgErr != nil {
		log.Printf("warning: config load: %v", cfgErr)
		cfg = &config.AppConfig{Agents: make(map[string]config.AgentConfig)}
	} else {
		log.Printf("config: loaded from %s (%d agents, %d delegation links, %d teams)",
			f.configDir, len(cfg.Agents), len(cfg.Delegation), len(cfg.Teams))
	}

	if anthropicKey == "" && openaiKey == "" {
		log.Println("warning: no ANTHROPIC_API_KEY or OPENAI_API_KEY set — only Ollama available (if running)")
	}

	log.Printf("sageclaw %s starting...", version)

	// --- Database ---
	storeType := envOrDefault("SAGECLAW_STORE", "sqlite")
	var appStore store.Store
	initCtx := context.Background()

	switch storeType {
	case "postgres":
		return fmt.Errorf("PostgreSQL support removed in v0.4 (ADR Rule E). Use SQLite")
	default:
		if err := os.MkdirAll(filepath.Dir(f.dbPath), 0755); err != nil {
			return fmt.Errorf("creating database directory: %w", err)
		}
		sqliteStore, err := sqlite.New(f.dbPath)
		if err != nil {
			return fmt.Errorf("opening database: %w", err)
		}
		defer sqliteStore.Close()
		appStore = sqliteStore
		log.Printf("database: sqlite (%s)", f.dbPath)
	}

	// --- Memory engine ---
	var memEngine memory.MemoryEngine
	var graphOps memory.GraphEngine

	if ss, ok := appStore.(*sqlite.Store); ok {
		memEngine = fts5.New(ss)
		graphOps = fts5.NewGraphOps(ss)
	} else {
		// PG: use a generic wrapper over the store interface.
		memEngine = newStoreMemoryEngine(appStore)
		graphOps = newStoreGraphEngine(appStore)
	}
	log.Println("memory: active")

	// --- Security ---
	sandbox, err := security.NewSandbox(f.workspace)
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}
	log.Printf("workspace: %s", sandbox.Root())

	// --- Encryption key: env var or auto-generated and persisted in DB ---
	encKey, err := loadOrGenerateEncKey(appStore.DB())
	if err != nil {
		return fmt.Errorf("encryption key: %w", err)
	}
	// Load provider keys from DB — scan the providers table to support custom names.
	{
		rows, err := appStore.DB().QueryContext(initCtx, `SELECT id, type FROM providers WHERE status = 'active'`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id, pType string
				if rows.Scan(&id, &pType) != nil {
					continue
				}
				credKey := "provider_" + id
				key, err := appStore.GetCredential(initCtx, credKey, encKey)
				if err != nil || len(key) == 0 {
					continue
				}
				switch pType {
				case "anthropic":
					if anthropicKey == "" {
						anthropicKey = string(key)
						log.Printf("provider: anthropic key loaded from database (%s)", id)
					}
				case "openai":
					if openaiKey == "" {
						openaiKey = string(key)
						log.Printf("provider: openai key loaded from database (%s)", id)
					}
				case "gemini":
					if geminiKey == "" {
						geminiKey = string(key)
						log.Printf("provider: gemini key loaded from database (%s)", id)
					}
				case "openrouter":
					if openrouterKey == "" {
						openrouterKey = string(key)
						log.Printf("provider: openrouter key loaded from database (%s)", id)
					}
				case "github":
					if githubToken == "" {
						githubToken = string(key)
						log.Printf("provider: github key loaded from database (%s)", id)
					}
				}
			}
		}
	}
	if telegramToken == "" {
		if key, err := appStore.GetCredential(initCtx, "TELEGRAM_BOT_TOKEN", encKey); err == nil && len(key) > 0 {
			telegramToken = string(key)
			log.Println("channel: telegram token loaded from database")
		}
	}
	if discordToken == "" {
		if key, err := appStore.GetCredential(initCtx, "DISCORD_BOT_TOKEN", encKey); err == nil && len(key) > 0 {
			discordToken = string(key)
			log.Println("channel: discord token loaded from database")
		}
	}

	// --- Providers + Router ---
	routes := make(map[provider.Tier]provider.Route)
	var defaultProvider provider.Provider

	// Provider registration order:
	// Strong tier: Anthropic > OpenAI > Gemini > OpenRouter > GitHub Copilot
	// Fast tier:   Gemini > OpenAI > Anthropic > OpenRouter (best cost/speed ratio)

	// Track providers for fast tier — registered after all providers are loaded.
	var (
		apClient  provider.Provider
		opClient  provider.Provider
		gpClient  provider.Provider
		orpClient provider.Provider
	)

	if anthropicKey != "" {
		ap := anthropic.NewClient(anthropicKey)
		apClient = ap
		routes[provider.TierStrong] = provider.Route{Provider: ap, Model: "claude-sonnet-4-20250514"}
		defaultProvider = ap
		log.Println("provider: anthropic")
	}

	if openaiKey != "" {
		op := openai.NewClient(openaiKey)
		opClient = op
		if _, exists := routes[provider.TierStrong]; !exists {
			routes[provider.TierStrong] = provider.Route{Provider: op, Model: "gpt-4o"}
		}
		if defaultProvider == nil {
			defaultProvider = op
		}
		log.Println("provider: openai")
	}

	// Gemini.
	var geminiClient *gemini.Client
	if geminiKey != "" {
		geminiClient = gemini.NewClient(geminiKey)
		gpClient = geminiClient
		if _, exists := routes[provider.TierStrong]; !exists {
			routes[provider.TierStrong] = provider.Route{Provider: geminiClient, Model: "gemini-2.0-flash"}
		}
		if defaultProvider == nil {
			defaultProvider = geminiClient
		}
		log.Println("provider: gemini")
	}

	// OpenRouter.
	if openrouterKey != "" {
		orp := openrouter.NewClient(openrouterKey)
		orpClient = orp
		if _, exists := routes[provider.TierStrong]; !exists {
			routes[provider.TierStrong] = provider.Route{Provider: orp, Model: "anthropic/claude-sonnet-4-20250514"}
		}
		if defaultProvider == nil {
			defaultProvider = orp
		}
		log.Println("provider: openrouter")

		_ = orpClient // used below for fast tier
	}

	// GitHub Copilot.
	if githubToken != "" {
		ghp := github.NewClient(githubToken)
		if _, exists := routes[provider.TierStrong]; !exists {
			routes[provider.TierStrong] = provider.Route{Provider: ghp, Model: "gpt-4o"}
		}
		if defaultProvider == nil {
			defaultProvider = ghp
		}
		log.Println("provider: github copilot")
	}

	// Ollama (local).
	ctx := context.Background()
	ollamaClient := ollama.New()
	ollamaHealthy := ollamaClient.Healthy(ctx)
	if ollamaHealthy {
		routes[provider.TierLocal] = provider.Route{Provider: ollamaClient, Model: "llama3.2:3b"}
		if defaultProvider == nil {
			defaultProvider = ollamaClient
		}
		models, _ := ollamaClient.Models(ctx)
		log.Printf("provider: ollama (models: %v)", models)
	} else {
		log.Println("provider: ollama not available")
	}

	// Fast tier: Gemini > OpenAI > Anthropic > OpenRouter.
	// These models offer the best speed/cost ratio for the fast tier.
	if _, exists := routes[provider.TierFast]; !exists && gpClient != nil {
		routes[provider.TierFast] = provider.Route{Provider: gpClient, Model: "gemini-3-flash-preview"}
	}
	if _, exists := routes[provider.TierFast]; !exists && opClient != nil {
		routes[provider.TierFast] = provider.Route{Provider: opClient, Model: "gpt-5.4-mini"}
	}
	if _, exists := routes[provider.TierFast]; !exists && apClient != nil {
		routes[provider.TierFast] = provider.Route{Provider: apClient, Model: "claude-haiku-4-5-20251001"}
	}
	if _, exists := routes[provider.TierFast]; !exists && orpClient != nil {
		routes[provider.TierFast] = provider.Route{Provider: orpClient, Model: "anthropic/claude-haiku-4-5-20251001"}
	}

	noProviders := defaultProvider == nil
	if noProviders {
		log.Println("warning: no providers available — dashboard will work but agent chat requires ANTHROPIC_API_KEY, OPENAI_API_KEY, or Ollama")
	}

	var router *provider.Router
	if !noProviders {
		fallback := provider.TierStrong
		if _, ok := routes[fallback]; !ok {
			for _, t := range []provider.Tier{provider.TierFast, provider.TierLocal} {
				if _, ok := routes[t]; ok {
					fallback = t
					break
				}
			}
		}
		var err error
		router, err = provider.NewRouter(routes, fallback)
		if err != nil {
			return fmt.Errorf("creating router: %w", err)
		}
		log.Printf("router: tiers=%v fallback=%s", router.Tiers(), router.Fallback())
	} else {
		// Create empty router so hot-reload from dashboard works (providers added via UI).
		router = provider.NewEmptyRouter()
		log.Println("router: empty (providers can be added via dashboard)")
	}

	// Register providers for combo resolution and model discovery.
	// Must run for ALL routers — not just the empty router path.
	if apClient != nil {
		router.RegisterProvider("anthropic", apClient)
	}
	if opClient != nil {
		router.RegisterProvider("openai", opClient)
	}
	if gpClient != nil {
		router.RegisterProvider("gemini", gpClient)
	}
	if orpClient != nil {
		router.RegisterProvider("openrouter", orpClient)
	}
	if ollamaHealthy {
		router.RegisterProvider("ollama", ollamaClient)
	}

	// Seed preset combos once: if no presets exist in DB, generate from KnownModels
	// with all providers (unconnected models are skipped at resolution time).
	// Once seeded, users are free to edit them — they're never overwritten.
	{
		var presetCount int
		appStore.DB().QueryRow(`SELECT COUNT(*) FROM combos WHERE is_preset = 1`).Scan(&presetCount)
		if presetCount == 0 {
			known := make([]provider.DiscoveredModelInfo, 0, len(provider.KnownModels))
			for _, m := range provider.KnownModels {
				known = append(known, provider.DiscoveredModelInfo{
					ModelID:       m.ModelID,
					Provider:      m.Provider,
					OutputCost:    m.OutputCost,
					ContextWindow: m.ContextWindow,
				})
			}
			presets := provider.GeneratePresetCombos(known, nil) // nil = all providers
			for _, combo := range presets {
				modelsJSON, err := json.Marshal(combo.Models)
				if err != nil {
					continue
				}
				appStore.DB().Exec(
					`INSERT OR IGNORE INTO combos (id, name, description, strategy, models, is_preset) VALUES (?, ?, ?, ?, ?, 1)`,
					combo.Name, combo.Name, "Auto-generated preset", combo.Strategy, string(modelsJSON))
			}
			log.Printf("router: seeded %d preset combos from known models", len(presets))
		}
	}

	// Load ALL combos from DB into the router (both user-created and presets).
	if comboRows, err := appStore.DB().Query(
		`SELECT id, name, strategy, models, is_preset FROM combos ORDER BY is_preset DESC, name`); err == nil {
		for comboRows.Next() {
			var id, name, strategy, modelsJSON string
			var isPreset int
			comboRows.Scan(&id, &name, &strategy, &modelsJSON, &isPreset)
			var models []provider.ComboModel
			if json.Unmarshal([]byte(modelsJSON), &models) != nil {
				// Handle double-encoded JSON strings from earlier frontend bug.
				var unwrapped string
				if json.Unmarshal([]byte(modelsJSON), &unwrapped) == nil {
					json.Unmarshal([]byte(unwrapped), &models)
				}
			}
			if len(models) > 0 {
				router.SetCombo(id, provider.Combo{
					Name:     name,
					Strategy: strategy,
					Models:   models,
					IsUser:   isPreset == 0,
				})
			}
		}
		comboRows.Close()
	}

	// Load per-provider TPM from DB config.
	if tpmRows, err := appStore.DB().QueryContext(initCtx,
		`SELECT type, config FROM providers WHERE status = 'active'`); err == nil {
		for tpmRows.Next() {
			var pType, cfgJSON string
			if tpmRows.Scan(&pType, &cfgJSON) != nil {
				continue
			}
			var cfg struct {
				TokensPerMinute int `json:"tokens_per_minute"`
			}
			if json.Unmarshal([]byte(cfgJSON), &cfg) == nil && cfg.TokensPerMinute > 0 {
				router.SetProviderTPM(pType, cfg.TokensPerMinute)
			} else {
				router.SetProviderTPM(pType, provider.DefaultTPM(pType))
			}
		}
		tpmRows.Close()
	}

	// --- Tool registry ---
	toolReg := tool.NewRegistry()
	tool.RegisterFS(toolReg, sandbox)
	tool.RegisterEdit(toolReg, sandbox)
	tool.RegisterExec(toolReg, sandbox.Root())
	browserMgr := tool.NewBrowserManager(filepath.Join(f.workspace, "screenshots"))
	fetchCache := tool.NewToolCache(15*time.Minute, 200)
	searchCache := tool.NewToolCache(60*time.Minute, 200)
	extractorChain := tool.NewDefaultChain("") // No Defuddle endpoint by default.
	tool.RegisterWeb(toolReg, &tool.WebConfig{
		FetchCache:      fetchCache,
		SearchCache:     searchCache,
		ExtractorChain:  extractorChain,
		BrowserFallback: browserMgr, // Headless browser fallback for JS-heavy pages.
	})
	tool.RegisterMemory(toolReg, memEngine)
	tool.RegisterGraph(toolReg, graphOps, memEngine)
	tool.RegisterCron(toolReg, appStore)
	// Spawn tools registered later after SubagentManager is created.
	tool.RegisterPlan(toolReg)
	tool.RegisterSkillLoader(toolReg, skillsDir)
	tool.RegisterDatetime(toolReg)
	tool.RegisterToolSearch(toolReg)
	tool.RegisterSessions(toolReg, appStore)
	tool.RegisterBrowser(toolReg, browserMgr)
	// Build provider chain for media tools (vision, document, image gen).
	var mediaProviders []provider.Provider
	router.ForEachProvider(func(_ string, p provider.Provider) {
		mediaProviders = append(mediaProviders, p)
	})
	tool.RegisterMedia(toolReg, sandbox, mediaProviders)

	// --- Agent configs (file-first, with DB/YAML fallback) ---
	agentsDir := filepath.Join(f.workspace, "agents")
	agentConfigs := map[string]agent.Config{}
	var agentMu sync.Mutex // protects fileAgents and agentConfigs

	// Migrate deprecated tools.yaml fields once at startup.
	agentcfg.MigrateToolsConfig(agentsDir)

	// Try file-based agent configs first.
	fileAgents, err := agentcfg.LoadAll(agentsDir)
	if err != nil {
		log.Printf("agentcfg: %v (falling back to YAML/DB)", err)
	}

	// Agent config provider for runtime consumers (pipeline, handlers).
	agentProvider := agentcfg.NewMapProvider(fileAgents)

	// Forward-declare loopPool and workflowEngine so the file watcher closure can reference them.
	var loopPool *agent.LoopPool
	var workflowEngine *team.WorkflowEngine
	var workflowCollector *team.WorkflowEventCollector
	leadAgentIDs := &sync.Map{} // agentID → true for team leads (cached for fast filtering)

	if len(fileAgents) > 0 {
		for id, ac := range fileAgents {
			ac.SkillsDir = skillsDir
			agentConfigs[id] = agentcfg.ToRuntimeConfig(ac)
		}
		log.Printf("agents: %d loaded from files (%s)", len(fileAgents), agentsDir)

		// Start file watcher for live reload.
		agentWatcher, watchErr := agentcfg.NewWatcher(agentsDir, func(agentID string) {
			dir := filepath.Join(agentsDir, agentID)
			reloaded, err := agentcfg.LoadAgent(dir)
			if err != nil {
				log.Printf("agentcfg: reload failed for %s: %v", agentID, err)
				return
			}
			reloaded.SkillsDir = skillsDir

			agentMu.Lock()
			defer agentMu.Unlock()

			// Preserve TeamInfo across hot-reload — it was set during startup
			// from team config, not from the agent's files on disk.
			if prev, ok := fileAgents[agentID]; ok && prev.TeamInfo != nil {
				reloaded.TeamInfo = prev.TeamInfo
			}
			fileAgents[agentID] = reloaded

			rc := agentcfg.ToRuntimeConfig(reloaded)
			// Re-inject task context funcs for team agents.
			if reloaded.TeamInfo != nil && reloaded.TeamInfo.Role == "lead" {
				teamIDCopy := reloaded.TeamInfo.TeamID
				rc.TaskSummaryFunc = func(ctx context.Context) string {
					return buildLeadTaskSummary(ctx, appStore, teamIDCopy)
				}
				// Re-wire workflow engine on hot-reload.
				if reloaded.TeamInfo.WorkflowEnabled {
					rc.WorkflowEnabled = true
					rc.WorkflowToolDefs = team.WorkflowToolDefs()
					var memberIDs []string
					for _, mi := range reloaded.TeamInfo.Members {
						if mi.Role != "lead" {
							memberIDs = append(memberIDs, mi.AgentID)
						}
					}
					memberIDsCopy := memberIDs
					rc.WorkflowToolHandler = func(ctx context.Context, sid, toolName, toolInput, userMessage string) (*canonical.ToolResult, error) {
						if workflowEngine == nil {
							return &canonical.ToolResult{Content: "Workflow engine not available.", IsError: true}, nil
						}
						switch toolName {
						case team.ToolWorkflowAnalyze:
							text, _, err := workflowEngine.HandleAnalyze(ctx, teamIDCopy, sid, userMessage, toolInput)
							if err != nil {
								return nil, err
							}
							return &canonical.ToolResult{Content: text}, nil
						case team.ToolWorkflowPlan:
							text, err := workflowEngine.HandlePlan(ctx, teamIDCopy, sid, toolInput, memberIDsCopy)
							if err != nil {
								return nil, err
							}
							return &canonical.ToolResult{Content: text}, nil
						default:
							return &canonical.ToolResult{Content: "Unknown workflow tool: " + toolName, IsError: true}, nil
						}
					}
				}
				// Re-build delegation routing on hot-reload.
				var profiles []team.MemberProfile
				for _, mi := range reloaded.TeamInfo.Members {
					if mi.Role == "lead" {
						continue
					}
					soulContent := ""
					if mfa, ok := fileAgents[mi.AgentID]; ok {
						soulContent = mfa.Soul
					}
					profiles = append(profiles, team.MemberProfile{
						AgentID:     mi.AgentID,
						DisplayName: mi.DisplayName,
						Keywords:    team.ExtractKeywords(mi.Description, soulContent),
					})
				}
				if len(profiles) > 0 {
					rc.DelegationAnalyzeFunc = func(message string) string {
						hint := team.AnalyzeDelegation(message, profiles, nil)
						return team.FormatDelegationHint(hint)
					}
				}
			} else if reloaded.TeamInfo != nil && reloaded.TeamInfo.Role == "member" {
				memberAgentID := agentID
				rc.MemberTaskContextFunc = func(ctx context.Context) string {
					return buildMemberTaskContext(ctx, appStore, memberAgentID)
				}
			}
			agentConfigs[agentID] = rc
			agentProvider.Update(agentID, reloaded)
			if loopPool != nil {
				loopPool.UpdateConfig(agentID, agentConfigs[agentID])
			}
			log.Printf("agentcfg: reloaded %s", agentID)
		})
		if watchErr == nil {
			agentWatcher.Start()
			defer agentWatcher.Stop()
			log.Printf("agentcfg: file watcher active")
		}
	} else {
		// Fallback: load from YAML config.
		if len(cfg.Agents) > 0 {
			for id, ac := range cfg.Agents {
				model := ac.Tier
				if model == "" {
					model = ac.Model
				}
				if model == "" {
					model = "strong"
				}
				maxTokens := ac.MaxTokens
				if maxTokens == 0 {
					maxTokens = 8192
				}
				agentConfigs[id] = agent.Config{
					AgentID:      id,
					SystemPrompt: ac.SystemPrompt,
					Model:        model,
					MaxTokens:    maxTokens,
				}
			}
			log.Printf("agents: %d loaded from YAML config", len(agentConfigs))
		}

		// Migrate DB agents to files (first boot).
		if migrated, err := agentcfg.MigrateFromDB(appStore.DB(), agentsDir); err == nil && migrated > 0 {
			log.Printf("agents: migrated %d agents from DB to %s/", migrated, agentsDir)
			// Reload from files after migration.
			if freshAgents, err := agentcfg.LoadAll(agentsDir); err == nil {
				for id, ac := range freshAgents {
					ac.SkillsDir = skillsDir
					agentConfigs[id] = agentcfg.ToRuntimeConfig(ac)
				}
			}
		}
	}

	// Ensure a default agent always exists.
	if _, ok := agentConfigs["default"]; !ok {
		defaultCfg := agentcfg.Defaults("default")
		defaultCfg.Identity.Name = "SageClaw"
		defaultCfg.Identity.Role = "personal AI agent"
		defaultCfg.Soul = `You have tools for file operations, shell execution, web access, persistent memory, and team collaboration.

Key behaviors:
- Search memory before starting tasks to recall relevant context
- Store important findings and decisions in memory after significant work
- Use self-learning: when you make a mistake, store a prevention rule
- Delegate complex subtasks to specialized agents when available
- Be concise and direct in responses`
		agentConfigs["default"] = agentcfg.ToRuntimeConfig(&defaultCfg)

		// Save the default agent to disk.
		agentcfg.SaveAgent(&defaultCfg, filepath.Join(agentsDir, "default"))
	}

	// --- Orchestration ---

	// Seed delegation links from YAML on first boot (DB is authoritative).
	seedDelegationLinks(appStore, cfg)

	delegator := orchestration.NewDelegator(appStore, agentConfigs, defaultProvider, router, toolReg)

	// Register delegation tools.
	tool.RegisterDelegate(toolReg,
		func(ctx context.Context, sourceID, targetID, prompt, mode string) (string, string, error) {
			return delegator.Delegate(ctx, sourceID, targetID, prompt, mode)
		},
		func(ctx context.Context, delegationID string) (status, sourceID, targetID, prompt, result string, err error) {
			record, err := delegator.Status(ctx, delegationID)
			if err != nil {
				return "", "", "", "", "", err
			}
			return record.Status, record.SourceID, record.TargetID, record.Prompt, record.Result, nil
		},
	)

	// Teams: DB is authoritative. Seed from YAML on first boot only.
	seedTeamsFromConfig(appStore, cfg)

	// Handoff.
	agentNames := map[string]string{"default": "SageClaw"}
	handoffMgr := orchestration.NewHandoff(appStore, agentNames)
	tool.RegisterHandoff(toolReg, func(ctx context.Context, sessionID, sourceAgentID, targetAgentID, reason string) error {
		return handoffMgr.Transfer(ctx, sessionID, sourceAgentID, targetAgentID, reason)
	})

	// Evaluate loop — uses the calling agent's config so model combos are respected.
	tool.RegisterEvaluate(toolReg, func(ctx context.Context, prompt string, maxRounds int, threshold float64) (string, float64, int, error) {
		callerID := tool.AgentIDFromContext(ctx)
		agentMu.Lock()
		genConfig, ok := agentConfigs[callerID]
		if !ok {
			genConfig = agentConfigs["default"]
		}
		agentMu.Unlock()
		evalConfig := genConfig
		evalConfig.SystemPrompt = "You are a quality evaluator. Score responses from 0.0 to 1.0. First line is the score. Then provide feedback."
		el := orchestration.NewEvalLoop(genConfig, evalConfig, defaultProvider, router, toolReg, maxRounds, threshold)
		result, err := el.Run(ctx, prompt)
		if err != nil {
			return "", 0, 0, err
		}
		return result.FinalOutput, result.Score, result.Rounds, nil
	})

	tool.RegisterAudit(toolReg, appStore.DB())

	log.Printf("tools: %d registered", len(toolReg.Names()))

	// --- Skills ---
	skills, err := skill.LoadSkills(skillsDir)
	if err != nil {
		log.Printf("warning: loading skills: %v", err)
	} else {
		for _, s := range skills {
			for _, bt := range s.BundledTools {
				toolName := s.Name + "_" + bt.Name
				toolReg.RegisterWithGroup(toolName, bt.Description, bt.Schema, tool.GroupOther, tool.RiskModerate, "skill:"+s.Name, skill.MakeShellToolFunc(bt.ScriptPath))
			}
			log.Printf("skill: %s (%d bundled tools)", s.Name, len(s.BundledTools))
		}
	}

	// --- Skill Store (marketplace) ---
	skillLockPath := filepath.Join(skillsDir, ".skills-lock.json")
	skillStoreOpts := []skillstore.Option{}
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		skillStoreOpts = append(skillStoreOpts, skillstore.WithGitHubToken(token))
	}
	if apiURL := os.Getenv("SKILLS_API_URL"); apiURL != "" {
		skillStoreOpts = append(skillStoreOpts, skillstore.WithSearchURL(apiURL))
	}
	skillStore, err := skillstore.NewStore(skillsDir, skillLockPath, skillStoreOpts...)
	if err != nil {
		log.Printf("warning: skill store init failed: %v", err)
	}

	// --- Middleware ---
	preCtx := middleware.Chain(
		middleware.PreContextMemory(memEngine),
		middleware.PreContextSelfLearning(memEngine),
	)
	postTool := middleware.Chain(
		middleware.PostToolScrub(),
		middleware.PostToolAudit(appStore),
	)

	// --- Consent ---
	consentStore := tool.NewPersistentConsentStore(appStore.DB())
	nonceManager := agent.NewNonceManager(ctx)

	// --- Agent ---
	appStore.DB().Exec(`INSERT OR IGNORE INTO agents (id, name, model) VALUES ('default', 'SageClaw', 'strong')`)

	// Forward-reference for SSE broadcast (set after RPC server is created).
	var sseBroadcast func(agent.Event)
	var telegramEventForwarder func(agent.Event)

	// Tool activity tracker — surfaces tool call activity to channel adapters.
	displayMap := toolstatus.DefaultDisplayMap()
	toolTracker := toolstatus.NewTracker(displayMap, nil, nil) // callbacks set after channel manager is ready

	// Forward-reference for lead wakeup (set after pipeline is created).
	var wakeLeadFn team.WakeLeadFunc

	// --- Voice messaging infrastructure ---
	audioStoragePath := cfg.Audio.StoragePath
	if audioStoragePath == "" {
		audioStoragePath = "data/audio"
	}
	audioStore := audio.NewStore(audioStoragePath)

	// Run audio cleanup on startup.
	if cfg.Audio.MaxAgeDays > 0 {
		deleted, err := audioStore.Cleanup(cfg.Audio.MaxAgeDays)
		if err != nil {
			log.Printf("audio: cleanup error: %v", err)
		} else if deleted > 0 {
			log.Printf("audio: cleaned up %d old files (max age: %d days)", deleted, cfg.Audio.MaxAgeDays)
		}
	}

	// Audio codec (ffmpeg or opus-tools).
	audioCodec, codecErr := audio.DefaultCodec()
	if codecErr != nil {
		log.Printf("audio: no codec available (%v) — voice encoding/decoding will fail. Install ffmpeg or opus-tools.", codecErr)
	} else {
		log.Printf("audio: codec ready (%s)", audioCodec.Name())
	}

	// Gemini Live client for voice sessions (requires Gemini API key).
	var liveClient *gemini.LiveClient
	var livePool *livesession.Pool
	if geminiKey != "" {
		liveClient = gemini.NewLiveClient(geminiKey)
		livePool = livesession.NewPool(liveClient, 0) // Default 5min idle timeout.
		log.Println("voice: Gemini Live client ready")
	} else {
		log.Println("voice: disabled (no Gemini API key)")
	}

	// Build loop options — include voice components when available.
	loopOpts := []agent.LoopOption{
		agent.WithRouter(router),
		agent.WithConsentStore(consentStore),
		agent.WithNonceManager(nonceManager),
		agent.WithOwnerResolver(func(sessionID string) (string, string) {
			sess, err := appStore.GetSession(ctx, sessionID)
			if err != nil || sess == nil || sess.Channel == "" {
				return "", ""
			}
			conn, err := appStore.GetConnection(ctx, sess.Channel)
			if err != nil || conn == nil {
				return "", ""
			}
			return conn.OwnerUserID, conn.Platform
		}),
	}
	if liveClient != nil {
		loopOpts = append(loopOpts, agent.WithLiveProvider(liveClient))
	}
	if audioCodec != nil {
		loopOpts = append(loopOpts, agent.WithAudioCodec(audioCodec))
	}
	if geminiClient != nil {
		loopOpts = append(loopOpts, agent.WithAudioTranscriber(geminiClient))
	}
	loopOpts = append(loopOpts, agent.WithAudioStore(audioStore))

	// Create SubagentManager before LoopPool so WithSubagentManager is in loopOpts at creation.
	subagentMgr := agent.NewSubagentManager(agent.SubagentConfig{
		MaxChildrenPerAgent: 5,
		MaxConcurrent:       8,
	}, nil, func(e agent.Event) {
		if sseBroadcast != nil {
			sseBroadcast(e)
		}
	})
	loopOpts = append(loopOpts, agent.WithSubagentManager(subagentMgr))

	// Streaming & parallel tool execution — always enabled.
	// Early consent check is nil: consent-requiring tools skip early execution
	// and are handled in ExecuteRemaining's batch path instead.
	resourceSem := agent.NewResourceSemaphores()
	streamingExec := agent.NewStreamingExecutor(toolReg, resourceSem, func(e agent.Event) {
		if sseBroadcast != nil {
			sseBroadcast(e)
		}
	}, nil)
	loopOpts = append(loopOpts, agent.WithStreamingExecutor(streamingExec))
	log.Println("tools: streaming & parallel execution enabled")

	// Load utility model setting and inject into all agent configs.
	if utilModel, _ := appStore.GetSetting(context.Background(), "utility_model"); utilModel != "" && utilModel != "auto" {
		for id, ac := range agentConfigs {
			ac.UtilityModel = utilModel
			agentConfigs[id] = ac
		}
		log.Printf("utility model override: %s", utilModel)
	}

	// Always create LoopPool so hot-reload from dashboard works.
	// When no providers exist yet, defaultProvider is nil — the router handles resolution.
	loopPool = agent.NewLoopPool(agentConfigs, defaultProvider, toolReg, preCtx, postTool,
		func(e agent.Event) {
			switch e.Type {
			case agent.EventRunStarted:
				log.Printf("[%s] run started", e.SessionID)
				toolTracker.OnRunStarted(e.SessionID)
			case agent.EventRunCompleted:
				log.Printf("[%s] run completed", e.SessionID)
				toolTracker.OnRunCompleted(e.SessionID)
				// Check if a team lead just completed a synthesis run (fast-path: cached lead IDs).
				if workflowEngine != nil && e.AgentID != "" && e.SessionID != "" {
					if _, isLead := leadAgentIDs.Load(e.AgentID); !isLead {
						break // Not a lead — skip DB lookup entirely.
					}
					agentID := e.AgentID
					sessID := e.SessionID
					go func() {
						ctx := context.Background()
						teamInfo, role, err := appStore.GetTeamByAgent(ctx, agentID)
						if err != nil || teamInfo == nil || role != "lead" {
							return
						}
						// Fetch the last assistant message as the synthesis response.
						var responseText string
						msgs, err := appStore.GetMessages(ctx, sessID, 5)
						if err == nil {
							for i := len(msgs) - 1; i >= 0; i-- {
								if msgs[i].Role == "assistant" {
									for _, c := range msgs[i].Content {
										if c.Type == "text" {
											responseText = c.Text
											break
										}
									}
									break
								}
							}
						}
						workflowEngine.HandleLeadRunComplete(ctx, teamInfo.ID, sessID, responseText)
					}()
				}
			case agent.EventToolCall:
				if e.ToolCall != nil {
					log.Printf("[%s] tool call: %s", e.SessionID, e.ToolCall.Name)
					toolTracker.OnToolCall(e.SessionID, e.ToolCall)
				}
			case agent.EventToolResult:
				if e.ToolResult != nil {
					toolTracker.OnToolResult(e.SessionID, e.ToolResult)
				}
			case agent.EventChunk:
				toolTracker.OnChunk(e.SessionID)
			case agent.EventConsentNeeded:
				if e.Consent != nil {
					log.Printf("[%s] consent needed: %s (%s/%s)", e.SessionID, e.Consent.ToolName, e.Consent.Group, e.Consent.RiskLevel)
				}
			case agent.EventRunFailed:
				log.Printf("[%s] run failed: %v", e.SessionID, e.Error)
				toolTracker.OnRunFailed(e.SessionID, e.Error)
			}
			// Broadcast to SSE clients (web dashboard).
			if sseBroadcast != nil {
				sseBroadcast(e)
			}
			// Forward streaming events to Telegram adapters.
			if telegramEventForwarder != nil {
				telegramEventForwarder(e)
			}
			// Persist-first event collection (handles tool calls from member loops).
			if workflowCollector != nil {
				workflowCollector.HandleEvent(e)
			}
		}, loopOpts...)

	// --- TeamExecutor + WorkflowEngine (needs LoopPool) ---
	var teamExec *team.TeamExecutor
	var teamExecMu sync.Mutex

	// ensureTeamExecutor lazily creates the TeamExecutor on first team.
	ensureTeamExecutor := func() *team.TeamExecutor {
		teamExecMu.Lock()
		defer teamExecMu.Unlock()
		if teamExec != nil {
			return teamExec
		}
		if loopPool == nil {
			return nil
		}
		teamExec = team.NewTeamExecutor(appStore, loopPool, func(e agent.Event) {
			if sseBroadcast != nil {
				sseBroadcast(e)
			}
			// Persist task lifecycle events.
			if workflowCollector != nil {
				workflowCollector.HandleEvent(e)
			}
			// Chain: forward task events to the workflow monitor.
			if workflowEngine != nil {
				workflowEngine.Monitor().HandleEvent(e)
			}
		})
		notifier := team.NewTeamProgressNotifier(appStore, teamExec, func(ctx context.Context, leadAgentID, teamID, systemMessage, sessionID string) {
			if wakeLeadFn != nil {
				wakeLeadFn(ctx, leadAgentID, teamID, systemMessage, sessionID)
			}
		})
		teamExec.SetNotifier(notifier)
		tool.RegisterTeamTasks(toolReg, appStore, teamExec)
		teamExec.StartRecoveryTicker(60 * time.Second)
		workflowEngine = team.NewWorkflowEngine(appStore, teamExec, func(e agent.Event) {
			if sseBroadcast != nil {
				sseBroadcast(e)
			}
		})
		// Persist-first event collector.
		workflowCollector = team.NewWorkflowEventCollector(appStore, func(e agent.Event) {
			if sseBroadcast != nil {
				sseBroadcast(e)
			}
		}, toolTracker)
		workflowEngine.SetCollector(workflowCollector)
		// Wire lead wakeup and start timeout ticker + recovery.
		workflowEngine.Monitor().SetWakeLead(func(ctx context.Context, leadAgentID, teamID, systemMessage, sessionID string) {
			if wakeLeadFn != nil {
				wakeLeadFn(ctx, leadAgentID, teamID, systemMessage, sessionID)
			}
		})
		workflowEngine.Monitor().StartTimeoutTicker(60*time.Second, team.DefaultTaskTimeout)
		workflowEngine.Monitor().RecoverActiveWorkflows(context.Background())
		log.Println("team: executor + workflow engine created (hot-reload)")
		return teamExec
	}

	// Team executor is initialized lazily by reloadTeams() or ensureTeamExecutor().

	// reloadTeams re-reads teams from DB and updates agent configs.
	reloadTeams := func() {
		rows, err := appStore.DB().QueryContext(context.Background(),
			`SELECT id, name, lead_id, config, COALESCE(settings,'{}') FROM teams WHERE status = 'active'`)
		if err != nil {
			log.Printf("team reload: query failed: %v", err)
			return
		}
		defer rows.Close()

		type teamRow struct {
			ID, Name, LeadID string
			Members          []string
			WorkflowEnabled  bool
		}
		var dbTeams []teamRow
		for rows.Next() {
			var id, name, leadID, configJSON, settingsJSON string
			rows.Scan(&id, &name, &leadID, &configJSON, &settingsJSON)
			var cfg struct {
				Members []string `json:"members"`
			}
			json.Unmarshal([]byte(configJSON), &cfg)
			// Parse team settings for kill switch.
			var settings struct {
				Workflow *bool `json:"workflow"`
			}
			json.Unmarshal([]byte(settingsJSON), &settings)
			workflowOn := settings.Workflow == nil || *settings.Workflow // Default: true
			dbTeams = append(dbTeams, teamRow{ID: id, Name: name, LeadID: leadID, Members: cfg.Members, WorkflowEnabled: workflowOn})
		}

		agentMu.Lock()
		defer agentMu.Unlock()

		if len(dbTeams) == 0 {
			// Clear TeamInfo from all agents.
			for aid, fa := range fileAgents {
				if fa.TeamInfo != nil {
					fa.TeamInfo = nil
					rc := agentcfg.ToRuntimeConfig(fa)
					agentConfigs[aid] = rc
					if loopPool != nil {
						loopPool.UpdateConfig(aid, rc)
					}
				}
			}
			log.Println("team reload: no active teams")
			return
		}

		// Ensure executor exists.
		ensureTeamExecutor()

		for _, tc := range dbTeams {
			allAgents := append([]string{tc.LeadID}, tc.Members...)
			var memberInfos []agentcfg.TeamMemberInfo
			for _, aid := range allAgents {
				role := "member"
				if aid == tc.LeadID {
					role = "lead"
				}
				displayName := aid
				desc := ""
				if fa, ok := fileAgents[aid]; ok {
					if fa.Identity.Name != "" {
						displayName = fa.Identity.Name
					}
					desc = fa.Identity.Role
				}
				memberInfos = append(memberInfos, agentcfg.TeamMemberInfo{
					AgentID: aid, DisplayName: displayName, Role: role, Description: desc,
				})
			}

			leadName := tc.LeadID
			if fa, ok := fileAgents[tc.LeadID]; ok && fa.Identity.Name != "" {
				leadName = fa.Identity.Name
			}

			for _, aid := range allAgents {
				fa, ok := fileAgents[aid]
				if !ok {
					continue
				}
				role := "member"
				if aid == tc.LeadID {
					role = "lead"
				}
				fa.TeamInfo = &agentcfg.TeamInfo{
					TeamID: tc.ID, TeamName: tc.Name, Role: role,
					LeadName: leadName, Members: memberInfos,
					WorkflowEnabled: tc.WorkflowEnabled,
				}
				rc := agentcfg.ToRuntimeConfig(fa)
				if role == "lead" {
					teamIDCopy := tc.ID
					rc.TaskSummaryFunc = func(ctx context.Context) string {
						return buildLeadTaskSummary(ctx, appStore, teamIDCopy)
					}

					// Wire workflow engine for this lead (if not disabled via kill switch).
					rc.WorkflowEnabled = tc.WorkflowEnabled
					if tc.WorkflowEnabled {
						rc.WorkflowToolDefs = team.WorkflowToolDefs()
						// Build member roster for plan validation.
						var memberIDs []string
						for _, mi := range memberInfos {
							if mi.Role != "lead" {
								memberIDs = append(memberIDs, mi.AgentID)
							}
						}
						memberIDsCopy := memberIDs
						rc.WorkflowToolHandler = func(ctx context.Context, sessionID, toolName, toolInput, userMessage string) (*canonical.ToolResult, error) {
							if workflowEngine == nil {
								return &canonical.ToolResult{Content: "Workflow engine not available.", IsError: true}, nil
							}
							switch toolName {
							case team.ToolWorkflowAnalyze:
								text, _, err := workflowEngine.HandleAnalyze(ctx, teamIDCopy, sessionID, userMessage, toolInput)
								if err != nil {
									return nil, err
								}
								return &canonical.ToolResult{Content: text}, nil
							case team.ToolWorkflowPlan:
								text, err := workflowEngine.HandlePlan(ctx, teamIDCopy, sessionID, toolInput, memberIDsCopy)
								if err != nil {
									return nil, err
								}
								return &canonical.ToolResult{Content: text}, nil
							default:
								return &canonical.ToolResult{Content: "Unknown workflow tool: " + toolName, IsError: true}, nil
							}
						}
					}

					// Build member profiles for delegation routing.
					var profiles []team.MemberProfile
					for _, mi := range memberInfos {
						if mi.Role == "lead" {
							continue
						}
						// Use description (agent role) + soul content for keyword extraction.
						soulContent := ""
						if mfa, ok := fileAgents[mi.AgentID]; ok {
							soulContent = mfa.Soul
						}
						profiles = append(profiles, team.MemberProfile{
							AgentID:     mi.AgentID,
							DisplayName: mi.DisplayName,
							Keywords:    team.ExtractKeywords(mi.Description, soulContent),
						})
					}
					if len(profiles) > 0 {
						rc.DelegationAnalyzeFunc = func(message string) string {
							hint := team.AnalyzeDelegation(message, profiles, nil)
							return team.FormatDelegationHint(hint)
						}
					}
				} else {
					memberAgentID := aid
					rc.MemberTaskContextFunc = func(ctx context.Context) string {
						return buildMemberTaskContext(ctx, appStore, memberAgentID)
					}
				}
				agentConfigs[aid] = rc
				if loopPool != nil {
					loopPool.UpdateConfig(aid, rc)
				}
			}
		}
		// Update cached lead agent IDs for fast filtering.
		// Clear old entries and add new ones.
		leadAgentIDs.Range(func(key, _ any) bool { leadAgentIDs.Delete(key); return true })
		for _, tc := range dbTeams {
			leadAgentIDs.Store(tc.LeadID, true)
		}

		log.Printf("team reload: updated %d teams", len(dbTeams))
	}

	// Load teams from DB at startup (covers teams created via dashboard, not in YAML).
	if loopPool != nil {
		reloadTeams()
	}

	// Wire SubagentManager's loopPool now that it's created.
	subagentMgr.SetLoopPool(loopPool)

	// Periodic cleanup of expired subagent results (prevent memory leak).
	subagentCleanupDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				subagentMgr.Cleanup(30 * time.Minute)
			case <-subagentCleanupDone:
				return
			}
		}
	}()

	// Register spawn tools with a bridge adapter.
	tool.RegisterSpawnTools(toolReg, &subagentBridge{mgr: subagentMgr})

	// --- Channel Pairing ---
	pairingEnabled := os.Getenv("SAGECLAW_PAIRING") != "off"
	pairingMgr := security.NewPairingManager(appStore.DB(), pairingEnabled)
	if pairingEnabled {
		log.Println("security: channel pairing enabled (set SAGECLAW_PAIRING=off to disable)")
	} else {
		log.Println("security: channel pairing disabled")
	}

	// Budget engine (must be before pipeline for cost recording).
	budgetEngine := provider.NewBudgetEngine(appStore.DB())

	// Unified Model Registry — single source of truth for pricing.
	// Owns the OpenRouter pricing refresh goroutine (fetches every 24h, persists to DB).
	modelRegistry := provider.NewModelRegistry(&pricingStoreAdapter{store: appStore})
	modelRegistry.SeedFromKnownModels(context.Background()) // Seed baseline data for offline users.
	budgetEngine.SetResolver(modelRegistry)
	provider.GlobalCacheStats.SetResolver(modelRegistry)
	// StartPricingRefresh is called after startCtx is created (below)
	// so the goroutine stops on graceful shutdown.

	// First-boot seeding: populate discovered_models from KnownModels if empty.
	if models, _ := appStore.ListAllDiscoveredModels(ctx); len(models) == 0 {
		var seeds []store.DiscoveredModel
		for _, km := range provider.KnownModels {
			seeds = append(seeds, store.DiscoveredModel{
				ID:                km.ID,
				Provider:          km.Provider,
				ModelID:           km.ModelID,
				DisplayName:       km.Name,
				ContextWindow:     km.ContextWindow,
				InputCost:         km.InputCost,
				OutputCost:        km.OutputCost,
				CacheCost:         km.CacheCost,
				ThinkingCost:      km.ThinkingCost,
				PricingSource:     "known",
			})
		}
		if err := appStore.UpsertDiscoveredModels(ctx, seeds); err != nil {
			log.Printf("pricing: seed failed: %v", err)
		} else {
			log.Printf("pricing: seeded %d models from KnownModels", len(seeds))
		}
	}

	// --- Bus + Pipeline ---
	msgBus := localbus.New()
	tool.RegisterMessage(toolReg, appStore, msgBus)

	// Forward-declare pipeline for consent callbacks in channel factories.
	var p *pipeline.Pipeline

	// Channel manager for hot-reload.
	chanMgr := channel.NewManager(ctx, msgBus)
	chanMgr.RegisterFactory("telegram", func(cfg map[string]string) (channel.Channel, error) {
		token := cfg["TELEGRAM_BOT_TOKEN"]
		if token == "" {
			return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN required")
		}
		connID := cfg["__conn_id"]
		if connID == "" {
			connID = "telegram"
		}
		return telegram.New(connID, token,
			telegram.WithAudioStore(audioStore),
			telegram.WithConsentCallback(func(nonce string, granted bool, tier string) {
				if p != nil {
					if err := p.InjectConsent(nonce, granted, tier); err != nil {
						log.Printf("telegram: consent inject failed: %v", err)
					}
				}
			}),
		), nil
	})
	chanMgr.RegisterFactory("discord", func(cfg map[string]string) (channel.Channel, error) {
		token := cfg["DISCORD_BOT_TOKEN"]
		if token == "" {
			return nil, fmt.Errorf("DISCORD_BOT_TOKEN required")
		}
		connID := cfg["__conn_id"]
		if connID == "" {
			connID = "discord"
		}
		d := discord.New(connID, token)
		d.SetConsentCallback(func(nonce string, granted bool, tier string) {
			if p != nil {
				if err := p.InjectConsent(nonce, granted, tier); err != nil {
					log.Printf("discord: consent inject failed: %v", err)
				}
			}
		})
		return d, nil
	})
	chanMgr.RegisterFactory("zalo", func(cfg map[string]string) (channel.Channel, error) {
		connID := cfg["__conn_id"]
		if connID == "" {
			connID = "zalo"
		}
		// Support both credential map keys and legacy env-var keys.
		creds := map[string]string{
			"oa_id":        firstNonEmpty(cfg["oa_id"], cfg["ZALO_OA_ID"]),
			"secret_key":   firstNonEmpty(cfg["secret_key"], cfg["ZALO_SECRET_KEY"]),
			"access_token": firstNonEmpty(cfg["access_token"], cfg["ZALO_ACCESS_TOKEN"]),
		}
		return zalo.NewFromCredentials(connID, creds,
			zalo.WithConsentCallback(func(nonce string, granted bool, tier string) {
				if p != nil {
					if err := p.InjectConsent(nonce, granted, tier); err != nil {
						log.Printf("zalo: consent inject failed: %v", err)
					}
				}
			}),
		), nil
	})
	chanMgr.RegisterFactory("zalo_bot", func(cfg map[string]string) (channel.Channel, error) {
		token := firstNonEmpty(cfg["token"], cfg["ZALO_BOT_TOKEN"])
		if token == "" {
			return nil, fmt.Errorf("ZALO_BOT_TOKEN required")
		}
		connID := cfg["__conn_id"]
		if connID == "" {
			connID = "zalo_bot"
		}
		return zalobot.New(connID, token,
			zalobot.WithConsentCallback(func(nonce string, granted bool, tier string) {
				if p != nil {
					if err := p.InjectConsent(nonce, granted, tier); err != nil {
						log.Printf("zalo_bot: consent inject failed: %v", err)
					}
				}
			}),
		), nil
	})
	chanMgr.RegisterFactory("whatsapp", func(cfg map[string]string) (channel.Channel, error) {
		connID := cfg["__conn_id"]
		if connID == "" {
			connID = "whatsapp"
		}
		// Support both credential map keys and legacy env-var keys.
		creds := map[string]string{
			"phone_number_id": firstNonEmpty(cfg["phone_number_id"], cfg["WHATSAPP_PHONE_NUMBER_ID"]),
			"access_token":    firstNonEmpty(cfg["access_token"], cfg["WHATSAPP_ACCESS_TOKEN"]),
			"verify_token":    firstNonEmpty(cfg["verify_token"], cfg["WHATSAPP_VERIFY_TOKEN"]),
			"app_secret":      firstNonEmpty(cfg["app_secret"], cfg["WHATSAPP_APP_SECRET"]),
		}
		return whatsapp.NewFromCredentials(connID, creds,
			whatsapp.WithConsentCallback(func(nonce string, granted bool, tier string) {
				if p != nil {
					if err := p.InjectConsent(nonce, granted, tier); err != nil {
						log.Printf("whatsapp: consent inject failed: %v", err)
					}
				}
			}),
		), nil
	})

	scheduler := pipeline.NewLaneScheduler(pipeline.DefaultLaneLimits(), func(ctx context.Context, req pipeline.RunRequest) {
		if p != nil {
			p.RunAgent(ctx, req)
		}
	})
	p = pipeline.New(msgBus, scheduler, appStore, pipeline.PipelineConfig{
		AgentID:         "default",
		LoopPool:        loopPool,
		PreResponse:     middleware.PreResponseLog(),
		Pairing:         pairingMgr,
		AgentProvider:   agentProvider,
		LiveSessionPool: livePool,
		NonceManager:    nonceManager,
		PostRun: func(ctx context.Context, agentID string) {
			if teamExec == nil {
				return
			}
			queue := tool.PendingDispatchFromCtx(ctx)
			if queue == nil {
				return
			}
			tasks := queue.Drain()
			if len(tasks) > 0 {
				// Detect cycles once before launching any tasks.
				teamExec.FailCycleTasks(ctx, tasks[0].TeamID)
			}
			for _, task := range tasks {
				teamExec.LaunchIfReady(ctx, task)
			}
		},
		CostRecorder: func(ctx context.Context, sessionID, agentID, provName, model string, usage canonical.Usage) {
			budgetEngine.RecordCost(ctx, provider.CostEntry{
				SessionID:      sessionID,
				AgentID:        agentID,
				Provider:       provName,
				Model:          model,
				InputTokens:    usage.InputTokens,
				OutputTokens:   usage.OutputTokens,
				CacheCreation:  usage.CacheCreation,
				CacheRead:      usage.CacheRead,
				ThinkingTokens: usage.ThinkingTokens,
			})
		},
	})

	// Wire lead wakeup: dispatches a run in the given session (or creates an internal one).
	wakeLeadFn = func(ctx context.Context, leadAgentID, teamID, systemMessage, sessionID string) {
		var sessID string
		if sessionID != "" {
			// Use the provided session (e.g., user's original chat session for synthesis delivery).
			sessID = sessionID
		} else {
			// Create an internal session for non-workflow wakeups (inbox results).
			sess, err := appStore.CreateSessionWithKind(ctx, "internal", teamID, leadAgentID, "team_wakeup")
			if err != nil {
				log.Printf("[team] wakeup: failed to create session for %s: %v", leadAgentID, err)
				return
			}
			sessID = sess.ID
		}
		msg := canonical.Message{
			Role:    "user",
			Content: []canonical.Content{{Type: "text", Text: "##wf:7a3f9e2b-4c1d-48a6-b5e0-3d2f1a8c9b7e##\n" + systemMessage}},
		}
		req := pipeline.RunRequest{
			SessionID: sessID,
			AgentID:   leadAgentID,
			Messages:  []canonical.Message{msg},
			Lane:      pipeline.LaneDelegate,
		}
		if err := scheduler.Schedule(ctx, pipeline.LaneDelegate, req); err != nil {
			log.Printf("[team] wakeup: failed to schedule lead run for %s: %v", leadAgentID, err)
		}
	}

	cronRunner := pipeline.NewCronRunner(appStore, scheduler)

	// --- MCP mode (early exit) ---
	if f.mcpMode {
		log.Println("Starting MCP server (stdio)...")
		mcpServer := mcp.NewServer(toolReg)
		return mcpServer.Run(context.Background())
	}

	// --- TUI mode ---
	if f.tui {
		tuiModel := tui.New(appStore.(*sqlite.Store), memEngine)
		p := tea.NewProgram(tuiModel, tea.WithAltScreen())
		_, err := p.Run()
		return err
	}

	// --- Start ---
	startCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := p.Start(startCtx); err != nil {
		return fmt.Errorf("starting pipeline: %w", err)
	}
	cronRunner.Start(startCtx)
	modelRegistry.StartPricingRefresh(startCtx)
	if noProviders {
		log.Println("agent: no providers configured yet — add one via dashboard to start chatting")
	}

	// --- RPC + Web Dashboard (auto-start) ---
	rpcAddr := envOrDefault("SAGECLAW_RPC_ADDR", ":9090")
	// Build provider health map.
	providerHealth := map[string]string{}
	if anthropicKey != "" {
		providerHealth["anthropic"] = "connected"
	} else {
		providerHealth["anthropic"] = "not configured"
	}
	if openaiKey != "" {
		providerHealth["openai"] = "connected"
	} else {
		providerHealth["openai"] = "not configured"
	}
	if geminiKey != "" {
		providerHealth["gemini"] = "connected"
	} else {
		providerHealth["gemini"] = "not configured"
	}
	if openrouterKey != "" {
		providerHealth["openrouter"] = "connected"
	} else {
		providerHealth["openrouter"] = "not configured"
	}
	if githubToken != "" {
		providerHealth["github"] = "connected"
	} else {
		providerHealth["github"] = "not configured"
	}
	if ollamaHealthy {
		providerHealth["ollama"] = "connected"
	} else {
		providerHealth["ollama"] = "not available"
	}

	// Create tunnel manager for webhook channels.
	rpcPort := 9090
	if len(rpcAddr) > 1 {
		if p, err := strconv.Atoi(rpcAddr[1:]); err == nil {
			rpcPort = p
		}
	}
	tunnelCfg := tunnel.Config{
		Mode:        cfg.Tunnel.Mode,
		RelayURL:    cfg.Tunnel.RelayURL,
		Token:       cfg.Tunnel.Token,
		AutoStart:   cfg.Tunnel.AutoStart,
		AutoWebhook: cfg.Tunnel.AutoWebhook,
		LocalPort:   rpcPort,
	}.Defaults()
	var tunnelMgr *tunnel.Client
	if tunnelCfg.Enabled() {
		var err error
		tunnelMgr, err = tunnel.NewClient(appStore.DB(), tunnelCfg, func(status tunnel.Status) {
			log.Printf("tunnel: running=%v url=%s", status.Running, status.URL)
		})
		if err != nil {
			log.Printf("warning: tunnel init failed: %v", err)
		}
	}

	// --- MCP Manager ---
	mcpMgr := mcp.NewManager(toolReg)
	// Collect MCP server configs from file-based agent configs.
	mcpServers := make(map[string]mcp.MCPServerConfig)
	if len(fileAgents) > 0 {
		for _, ac := range fileAgents {
			for name, cfg := range ac.Tools.MCPServers {
				if _, exists := mcpServers[name]; !exists {
					mcpServers[name] = cfg
				}
			}
		}
	}
	if len(mcpServers) > 0 {
		mcpMgr.StartAll(startCtx, mcpServers)
		defer mcpMgr.Stop()
	}

	// --- MCP Marketplace Registry ---
	mcpRegistry, err := mcpregistry.NewRegistry(appStore, appStore, encKey, mcpMgr)
	if err != nil {
		log.Printf("mcp-registry: failed to initialize: %v", err)
	} else {
		mcpRegistry.SetDataDir(filepath.Dir(f.dbPath))
		if indexURL := os.Getenv("SAGECLAW_MCP_INDEX_URL"); indexURL != "" {
			mcpRegistry.SetIndexURL(indexURL)
		}
		mcpRegistry.LoadLocalOverride()
		if err := mcpRegistry.SeedFromCurated(startCtx); err != nil {
			log.Printf("mcp-registry: seed failed: %v", err)
		}
		mcpRegistry.StartInstalled(startCtx)

		// Populate AllowedMCPServers for each agent based on registry assignments.
		for id, cfg := range agentConfigs {
			if allowed, err := mcpRegistry.GetInstalledForAgent(startCtx, id); err == nil && len(allowed) > 0 {
				cfg.AllowedMCPServers = allowed
				agentConfigs[id] = cfg
			}
		}
	}

	rpcServer := rpc.NewServer(appStore, memEngine, msgBus, rpc.Config{ListenAddr: rpcAddr},
		rpc.WithGraphEngine(graphOps),
		rpc.WithToolRegistry(toolReg),
		rpc.WithProviderHealth(providerHealth),
		rpc.WithTunnel(tunnelMgr),
		rpc.WithAgentsDir(agentsDir),
		rpc.WithPairing(pairingMgr),
		rpc.WithBudgetEngine(budgetEngine),
		rpc.WithModelRegistry(modelRegistry),
		rpc.WithEncryptionKey(encKey),
		rpc.WithRouter(router),
		rpc.WithChannelManager(chanMgr),
		rpc.WithMCPManager(mcpMgr),
		rpc.WithMCPRegistry(mcpRegistry),
		rpc.WithSkillStore(skillStore),
		rpc.WithConsentHandler(p.InjectConsent),
		rpc.WithConsentStore(consentStore),
		rpc.WithAudioBasePath(audioStoragePath),
		rpc.WithLoopPool(loopPool),
		rpc.WithTeamReload(reloadTeams),
		rpc.WithWorkflowCancel(func(ctx context.Context, sessionID string) (bool, error) {
			if workflowEngine == nil {
				return false, nil
			}
			return workflowEngine.CancelActiveWorkflow(ctx, sessionID)
		}),
		rpc.WithWorkspace(f.workspace),
	)
	// Wire SSE broadcast now that rpcServer exists.
	sseBroadcast = rpcServer.EventHandler()

	if err := rpcServer.Start(startCtx); err != nil {
		log.Printf("warning: dashboard server failed: %v", err)
	} else {
		log.Printf("dashboard: http://localhost%s", rpcAddr)
		defer rpcServer.Stop(startCtx)
	}

	// Discover models for all connected providers (non-blocking).
	rpcServer.DiscoverAllModels()

	// Wire session invalidation: rotate JWT secret when tunnel starts.
	// This forces all existing sessions to re-login (with TOTP if enabled).
	rpcServer.WireTunnelAuth()

	// Wire auto-webhook: register webhook URLs when tunnel is ready.
	rpcServer.WireTunnelWebhooks()

	// Auto-start tunnel if configured.
	if tunnelMgr != nil && tunnelCfg.AutoStart {
		if err := tunnelMgr.Start(startCtx); err != nil {
			log.Printf("warning: tunnel auto-start failed: %v", err)
		} else {
			log.Printf("tunnel: auto-started in %s mode", tunnelCfg.Mode)
		}
	}

	// --- Channels ---
	useCLI := f.forceCLI || (telegramToken == "" && discordToken == "")

	// Check which platforms already have DB-configured connections.
	// Skip legacy env var startup for those to avoid duplicate pollers.
	dbPlatforms := map[string]bool{}
	if dbConnsCheck, err := appStore.ListConnections(startCtx, store.ConnectionFilter{Status: "active"}); err == nil {
		for _, conn := range dbConnsCheck {
			dbPlatforms[conn.Platform] = true
		}
	}

	if telegramToken != "" && !f.forceCLI && !dbPlatforms["telegram"] {
		tg := telegram.New("telegram", telegramToken,
			telegram.WithAudioStore(audioStore),
			telegram.WithConsentCallback(func(nonce string, granted bool, tier string) {
				if p != nil {
					if err := p.InjectConsent(nonce, granted, tier); err != nil {
						log.Printf("telegram: consent inject failed: %v", err)
					}
				}
			}),
		)
		if err := tg.Start(startCtx, msgBus); err != nil {
			return fmt.Errorf("starting telegram: %w", err)
		}
		log.Println("channel: telegram (legacy)")
		chanMgr.Register(tg)
		defer tg.Stop(startCtx)
	} else if telegramToken != "" && dbPlatforms["telegram"] {
		log.Println("channel: telegram (skipping legacy — DB connection exists)")
	}

	if discordToken != "" && !dbPlatforms["discord"] {
		dc := discord.New("discord", discordToken)
		if err := dc.Start(startCtx, msgBus); err != nil {
			log.Printf("warning: discord start failed: %v", err)
		} else {
			log.Println("channel: discord (legacy)")
			chanMgr.Register(dc)
			defer dc.Stop(startCtx)
		}
	}

	if zaloOAID != "" {
		zc := zalo.New("zalo", zaloOAID, zaloSecret, zaloToken)
		if err := zc.Start(startCtx, msgBus); err != nil {
			return fmt.Errorf("starting zalo: %w", err)
		}
		chanMgr.Register(zc)
		log.Println("channel: zalo OA (legacy)")
		defer zc.Stop(startCtx)
	}

	if zaloBotToken != "" {
		zb := zalobot.New("zalo_bot", zaloBotToken)
		if err := zb.Start(startCtx, msgBus); err != nil {
			return fmt.Errorf("starting zalo_bot: %w", err)
		}
		chanMgr.Register(zb)
		log.Println("channel: zalo bot (legacy)")
		defer zb.Stop(startCtx)
	}

	if waPhoneID != "" {
		wa := whatsapp.New("whatsapp", waPhoneID, waAccessToken, waVerifyToken)
		if err := wa.Start(startCtx, msgBus); err != nil {
			return fmt.Errorf("starting whatsapp: %w", err)
		}
		chanMgr.Register(wa)
		log.Println("channel: whatsapp (legacy)")
		defer wa.Stop(startCtx)
	}

	// --- Backfill inline credentials (migration from credential_key → credentials blob) ---
	allConns, _ := appStore.ListConnections(startCtx, store.ConnectionFilter{})
	for _, conn := range allConns {
		if len(conn.Credentials) == 0 && conn.CredentialKey != "" {
			// Migrate: read from credentials table → encrypt as JSON → save inline.
			oldVal, err := appStore.GetCredential(startCtx, conn.CredentialKey, encKey)
			if err == nil && len(oldVal) > 0 {
				creds := map[string]string{"token": string(oldVal)}
				blob, err := sqlite.EncryptCredentials(creds, encKey)
				if err == nil {
					appStore.UpdateConnection(startCtx, conn.ID, map[string]any{"credentials": blob})
					log.Printf("connection %s: backfilled inline credentials", conn.ID)
				}
			}
		}
	}

	// --- Start connections from DB ---
	dbConns, err := appStore.ListConnections(startCtx, store.ConnectionFilter{Status: "active"})
	if err == nil && len(dbConns) > 0 {
		for _, conn := range dbConns {
			// Skip if already started via legacy env vars.
			if chanMgr.IsRunning(conn.ID) {
				continue
			}

			// Load credentials: prefer inline blob, fall back to legacy credential_key.
			var creds map[string]string
			if len(conn.Credentials) > 0 {
				creds, err = sqlite.DecryptCredentials(conn.Credentials, encKey)
				if err != nil {
					log.Printf("connection %s: failed to decrypt credentials, skipping", conn.ID)
					continue
				}
			} else if conn.CredentialKey != "" {
				token, err := appStore.GetCredential(startCtx, conn.CredentialKey, encKey)
				if err != nil || len(token) == 0 {
					log.Printf("connection %s: credential not found, skipping", conn.ID)
					continue
				}
				creds = map[string]string{"token": string(token)}
			} else {
				log.Printf("connection %s: no credentials, skipping", conn.ID)
				continue
			}

			cfg := map[string]string{"__conn_id": conn.ID}
			// Copy all credential fields into config for the factory.
			for k, v := range creds {
				cfg[k] = v
			}
			// Also set legacy config keys for backward compat.
			switch conn.Platform {
			case "telegram":
				if t, ok := creds["token"]; ok {
					cfg["TELEGRAM_BOT_TOKEN"] = t
				}
			case "discord":
				if t, ok := creds["token"]; ok {
					cfg["DISCORD_BOT_TOKEN"] = t
				}
			case "zalo":
				if v, ok := creds["oa_id"]; ok {
					cfg["ZALO_OA_ID"] = v
				}
				if v, ok := creds["secret_key"]; ok {
					cfg["ZALO_SECRET_KEY"] = v
				}
				if v, ok := creds["access_token"]; ok {
					cfg["ZALO_ACCESS_TOKEN"] = v
				}
			case "whatsapp":
				if v, ok := creds["phone_number_id"]; ok {
					cfg["WHATSAPP_PHONE_NUMBER_ID"] = v
				}
				if v, ok := creds["access_token"]; ok {
					cfg["WHATSAPP_ACCESS_TOKEN"] = v
				}
				if v, ok := creds["verify_token"]; ok {
					cfg["WHATSAPP_VERIFY_TOKEN"] = v
				}
				if v, ok := creds["app_secret"]; ok {
					cfg["WHATSAPP_APP_SECRET"] = v
				}
			}

			if err := chanMgr.StartConnection(conn.ID, conn.Platform, cfg); err != nil {
				log.Printf("connection %s (%s): start failed: %v", conn.ID, conn.Platform, err)
				appStore.UpdateConnection(startCtx, conn.ID, map[string]any{"status": "error"})
			} else {
				log.Printf("connection %s (%s): started from DB", conn.ID, conn.Platform)
			}
		}
	}

	if useCLI {
		cliAdapter := cli.New()
		if err := cliAdapter.Start(startCtx, msgBus); err != nil {
			return fmt.Errorf("starting cli: %w", err)
		}
		log.Println("channel: cli (interactive)")
	} else {
		log.Println("SageClaw is running. Listening for messages...")
	}

	// --- Tool activity tracker callbacks ---
	// Set callbacks now that chanMgr and appStore are available.
	// Dispatch tool status and reactions to channel-specific adapters.
	toolTracker.SetCallbacks(
		func(sessionID string, update toolstatus.StatusUpdate) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			sess, err := appStore.GetSession(ctx, sessionID)
			if err != nil || sess == nil {
				return
			}
			// Dispatch to channel-specific adapter.
			chanMgr.ForEachChannel(func(ch channel.Channel) {
				switch adapter := ch.(type) {
				case *telegram.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnToolStatus(sessionID, sess.ChatID, update)
					}
				case *discord.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnToolStatus(sessionID, sess.ChatID, update)
					}
				case *whatsapp.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnToolStatus(sessionID, sess.ChatID, update)
					}
				case *zalo.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnToolStatus(sessionID, sess.ChatID, update)
					}
				case *zalobot.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnToolStatus(sessionID, sess.ChatID, update)
					}
				}
			})
		},
		func(sessionID string, update toolstatus.ReactionUpdate) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			sess, err := appStore.GetSession(ctx, sessionID)
			if err != nil || sess == nil {
				return
			}
			// Reactions need the user's message ID from session metadata.
			userMsgIDStr := sess.Metadata["user_message_id"]
			if userMsgIDStr == "" {
				return
			}
			userMsgID, _ := strconv.Atoi(userMsgIDStr) // Telegram uses numeric IDs.
			// Dispatch to channel-specific adapter.
			chanMgr.ForEachChannel(func(ch channel.Channel) {
				switch adapter := ch.(type) {
				case *telegram.Adapter:
					if adapter.ConnID() == sess.Channel {
						adapter.OnReaction(sess.ChatID, userMsgID, update)
					}
				case *discord.Adapter:
					if adapter.ConnID() == sess.Channel {
						// Discord uses string message IDs.
						adapter.OnReaction(sess.ChatID, userMsgIDStr, update)
					}
				}
				// WhatsApp/Zalo: no reaction API — skip.
			})
		},
	)

	// --- Channel event forwarder ---
	// Forward streaming events to Telegram adapters and consent events
	// to all channel adapters that implement ConsentPrompt.
	telegramEventForwarder = func(e agent.Event) {
		if e.SessionID == "" {
			return
		}

		// Consent events → route to the appropriate channel adapter's RenderConsent.
		if e.Type == agent.EventConsentNeeded && e.Consent != nil {
			sess, err := appStore.GetSession(startCtx, e.SessionID)
			if err != nil || sess == nil {
				log.Printf("consent: session %s not found for consent event", e.SessionID)
				return
			}
			ch := chanMgr.GetChannel(sess.Channel)
			if ch == nil {
				return
			}
			if cp, ok := ch.(channel.ConsentPrompt); ok {
				req := channel.ConsentPromptRequest{
					ChatID:      sess.ChatID,
					Nonce:       e.Consent.Nonce,
					ToolName:    e.Consent.ToolName,
					Group:       e.Consent.Group,
					RiskLevel:   e.Consent.RiskLevel,
					Explanation: e.Consent.Explanation,
					Options:     channel.DefaultConsentOptions(e.Consent.Nonce),
				}
				if err := cp.RenderConsent(startCtx, req); err != nil {
					log.Printf("consent: render failed on %s: %v", sess.Channel, err)
				}
			}
			return
		}

		// Streaming + lifecycle events → forward to channel adapters.
		if e.Type != agent.EventChunk && e.Type != agent.EventRunStarted && e.Type != agent.EventRunCompleted && e.Type != agent.EventRunFailed {
			return
		}
		sess, err := appStore.GetSession(startCtx, e.SessionID)
		if err != nil || sess == nil {
			return
		}
		chanMgr.ForEachChannel(func(ch channel.Channel) {
			switch adapter := ch.(type) {
			case *telegram.Adapter:
				if adapter.ConnID() == sess.Channel {
					adapter.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
				}
			case *discord.Adapter:
				if adapter.ConnID() == sess.Channel {
					adapter.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
				}
			case *whatsapp.Adapter:
				if adapter.ConnID() == sess.Channel {
					adapter.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
				}
			case *zalo.Adapter:
				if adapter.ConnID() == sess.Channel {
					adapter.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
				}
			case *zalobot.Adapter:
				if adapter.ConnID() == sess.Channel {
					adapter.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
				}
			}
		})
	}

	// --- SIGHUP for skill hot-reload ---
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			log.Println("Reloading skills...")
			newSkills, err := skill.LoadSkills(skillsDir)
			if err != nil {
				log.Printf("skill reload error: %v", err)
				continue
			}
			skill.Reconcile(toolReg, skills, newSkills)
			skills = newSkills
			log.Printf("Skills reloaded: %d skills", len(newSkills))
		}
	}()

	// --- Graceful shutdown ---
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Shutting down...")
	close(subagentCleanupDone)
	subagentMgr.Shutdown()
	if teamExec != nil {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		teamExec.Shutdown(shutdownCtx)
		shutdownCancel()
	}
	cronRunner.Stop()
	if mcpRegistry != nil {
		mcpRegistry.Close()
	}
	cancel()
	log.Println("Goodbye.")
	return nil
}

// loadOrGenerateEncKey returns a 32-byte encryption key.
// Priority: SAGECLAW_ENCRYPTION_KEY env var > persisted in DB > generate new.
func loadOrGenerateEncKey(db *sql.DB) ([]byte, error) {
	if envKey := os.Getenv("SAGECLAW_ENCRYPTION_KEY"); envKey != "" {
		k, err := hex.DecodeString(envKey)
		if err != nil || len(k) != 32 {
			return nil, fmt.Errorf("SAGECLAW_ENCRYPTION_KEY must be 64 hex characters (32 bytes)")
		}
		return k, nil
	}

	// Try loading from DB.
	var stored string
	err := db.QueryRow("SELECT value FROM settings WHERE key = 'encryption_key'").Scan(&stored)
	if err == nil && stored != "" {
		k, err := hex.DecodeString(stored)
		if err == nil && len(k) == 32 {
			return k, nil
		}
	}

	// Generate and persist.
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generating encryption key: %w", err)
	}
	keyHex := hex.EncodeToString(key)
	_, err = db.Exec(
		`INSERT INTO settings (key, value) VALUES ('encryption_key', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		keyHex)
	if err != nil {
		return nil, fmt.Errorf("persisting encryption key: %w", err)
	}
	log.Println("security: generated new encryption key (persisted in DB)")
	return key, nil
}

func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sageclaw.db"
	}
	return filepath.Join(home, ".sageclaw", "sageclaw.db")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- Generic store wrappers for non-SQLite backends ---

// storeMemoryEngine wraps a store.Store as a memory.MemoryEngine.
type storeMemoryEngine struct{ s store.Store }

func newStoreMemoryEngine(s store.Store) memory.MemoryEngine { return &storeMemoryEngine{s: s} }

func (e *storeMemoryEngine) Search(ctx context.Context, query string, opts memory.SearchOptions) ([]memory.Entry, error) {
	mems, scores, err := e.s.SearchMemories(ctx, query, opts.Limit)
	if err != nil {
		return nil, err
	}
	entries := make([]memory.Entry, len(mems))
	for i, m := range mems {
		entries[i] = memory.Entry{ID: m.ID, Title: m.Title, Content: m.Content, Tags: m.Tags, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
		if i < len(scores) {
			entries[i].Score = scores[i]
		}
	}
	return entries, nil
}

func (e *storeMemoryEngine) Write(ctx context.Context, content, title string, tags []string) (string, error) {
	id, _, err := e.s.WriteMemory(ctx, content, title, tags)
	return id, err
}

func (e *storeMemoryEngine) Delete(ctx context.Context, id string) error {
	return e.s.DeleteMemory(ctx, id)
}

func (e *storeMemoryEngine) List(ctx context.Context, tags []string, limit, offset int) ([]memory.Entry, error) {
	mems, err := e.s.ListMemories(ctx, tags, limit, offset)
	if err != nil {
		return nil, err
	}
	entries := make([]memory.Entry, len(mems))
	for i, m := range mems {
		entries[i] = memory.Entry{ID: m.ID, Title: m.Title, Content: m.Content, Tags: m.Tags, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	}
	return entries, nil
}

// storeGraphEngine is a no-op graph engine for non-SQLite backends.
// Full PG graph support would query the edges table via store interface.
type storeGraphEngine struct{}

func newStoreGraphEngine(s store.Store) memory.GraphEngine { return &storeGraphEngine{} }

func (g *storeGraphEngine) Link(ctx context.Context, sourceID, targetID, relation string, props map[string]any) (string, error) {
	return "", fmt.Errorf("graph operations require SQLite backend (PG graph support coming soon)")
}
func (g *storeGraphEngine) Unlink(ctx context.Context, sourceID, targetID, relation string) error {
	return fmt.Errorf("graph operations require SQLite backend")
}
func (g *storeGraphEngine) Graph(ctx context.Context, startID, direction string, depth int) ([]memory.Entry, []memory.Edge, error) {
	return nil, nil, fmt.Errorf("graph operations require SQLite backend")
}

// subagentBridge adapts agent.SubagentManager to tool.SubagentSpawner interface.
type subagentBridge struct {
	mgr *agent.SubagentManager
}

func (b *subagentBridge) Spawn(ctx context.Context, parentAgentID, sessionID, task, label, mode string) (string, string, error) {
	return b.mgr.Spawn(ctx, parentAgentID, sessionID, task, label, mode)
}

func (b *subagentBridge) List(parentAgentID, sessionID string) []tool.SubagentInfo {
	tasks := b.mgr.List(parentAgentID, sessionID)
	infos := make([]tool.SubagentInfo, len(tasks))
	for i, t := range tasks {
		infos[i] = tool.SubagentInfo{
			ID:     t.ID,
			Label:  t.Label,
			Status: t.Status,
			Result: t.Result,
			Error:  t.Error,
		}
	}
	return infos
}

func (b *subagentBridge) Cancel(taskID string) error {
	return b.mgr.Cancel(taskID)
}

func (b *subagentBridge) CancelAll(parentAgentID, sessionID string) {
	b.mgr.CancelAll(parentAgentID, sessionID)
}

// buildLeadTaskSummary builds a [Active team tasks] injection for lead agents.
func buildLeadTaskSummary(ctx context.Context, s store.Store, teamID string) string {
	tasks, err := s.ListTasks(ctx, teamID, "")
	if err != nil || len(tasks) == 0 {
		return ""
	}

	var pending, inProgress, blocked, inReview int
	var lines []string
	for _, t := range tasks {
		switch t.Status {
		case "pending":
			pending++
			lines = append(lines, fmt.Sprintf("- %s: \"%s\" → %s (pending)", t.Identifier, t.Title, t.AssignedTo))
		case "in_progress":
			inProgress++
			progress := ""
			if t.ProgressPercent > 0 {
				progress = fmt.Sprintf(" (%d%%)", t.ProgressPercent)
			}
			lines = append(lines, fmt.Sprintf("- %s: \"%s\" → %s (in_progress%s)", t.Identifier, t.Title, t.AssignedTo, progress))
		case "blocked":
			blocked++
			lines = append(lines, fmt.Sprintf("- %s: \"%s\" (blocked by %s)", t.Identifier, t.Title, t.BlockedBy))
		case "in_review":
			inReview++
			lines = append(lines, fmt.Sprintf("- %s: \"%s\" → needs your approval", t.Identifier, t.Title))
		}
	}

	if len(lines) == 0 {
		return "" // All tasks completed/cancelled — no reminder needed.
	}

	var sb strings.Builder
	sb.WriteString("[Active team tasks]\n")
	sb.WriteString(fmt.Sprintf("You have %d pending, %d in-progress, %d blocked, %d awaiting review.\n",
		pending, inProgress, blocked, inReview))
	for _, line := range lines {
		sb.WriteString(line + "\n")
	}
	sb.WriteString("Results arrive automatically. Do NOT re-create, cancel, or re-spawn these tasks.\n")
	sb.WriteString("[/Active team tasks]")
	return sb.String()
}

// buildMemberTaskContext returns a one-line context reminder for a member agent's active task.
func buildMemberTaskContext(ctx context.Context, s store.Store, agentID string) string {
	team, _, err := s.GetTeamByAgent(ctx, agentID)
	if err != nil || team == nil {
		return ""
	}

	tasks, err := s.ListTasks(ctx, team.ID, "in_progress")
	if err != nil {
		return ""
	}

	for _, t := range tasks {
		if t.AssignedTo == agentID {
			progress := ""
			if t.ProgressPercent > 0 {
				progress = fmt.Sprintf(" — progress: %d%%", t.ProgressPercent)
			}
			return fmt.Sprintf("[Your task: %s \"%s\"%s]", t.Identifier, t.Title, progress)
		}
	}
	return ""
}

// seedTeamsFromConfig seeds teams from YAML config into DB on first boot.
// If DB already has teams, YAML is ignored with a deprecation warning.
func seedTeamsFromConfig(s store.Store, cfg *config.AppConfig) {
	if len(cfg.Teams) == 0 {
		return
	}
	ctx := context.Background()

	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM teams WHERE COALESCE(status,'active') = 'active'`).Scan(&count); err != nil {
		log.Printf("teams: failed to count existing teams: %v", err)
		return
	}
	if count > 0 {
		log.Printf("teams: ignoring teams.yaml (DB has %d active teams — DB is authoritative)", count)
		return
	}

	seeded := 0
	for _, tc := range cfg.Teams {
		membersJSON := `{"members":[]}`
		if len(tc.Members) > 0 {
			parts := make([]string, len(tc.Members))
			for i, m := range tc.Members {
				parts[i] = `"` + m + `"`
			}
			membersJSON = fmt.Sprintf(`{"members":[%s]}`, strings.Join(parts, ","))
		}
		_, err := s.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO teams (id, name, lead_id, config, description, status, settings, created_at, updated_at)
			 VALUES (?, ?, ?, ?, '', 'active', '{}', datetime('now'), datetime('now'))`,
			tc.ID, tc.Name, tc.Lead, membersJSON)
		if err != nil {
			log.Printf("teams: failed to seed team %s: %v", tc.ID, err)
			continue
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("teams: seeded %d teams from config", seeded)
	}
}

// seedDelegationLinks seeds delegation links from YAML config into DB on first boot.
// If DB already has links, YAML is ignored with a deprecation warning.
func seedDelegationLinks(s store.Store, cfg *config.AppConfig) {
	if len(cfg.Delegation) == 0 {
		return
	}
	ctx := context.Background()

	// Check if DB already has delegation links.
	var count int
	if err := s.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM delegation_links`).Scan(&count); err != nil {
		log.Printf("delegation: failed to count existing links: %v", err)
		return
	}
	if count > 0 {
		log.Printf("delegation: ignoring delegation.yaml (DB has %d links — DB is authoritative)", count)
		return
	}

	// Seed from YAML.
	seeded := 0
	for _, dl := range cfg.Delegation {
		maxC := dl.MaxConcurrent
		if maxC == 0 {
			maxC = 1
		}
		id := fmt.Sprintf("link_%s_%s", dl.Source, dl.Target)
		_, err := s.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO delegation_links (id, source_id, target_id, direction, max_concurrent) VALUES (?, ?, ?, ?, ?)`,
			id, dl.Source, dl.Target, dl.Direction, maxC)
		if err != nil {
			log.Printf("delegation: failed to seed link %s→%s: %v", dl.Source, dl.Target, err)
			continue
		}
		if _, err := s.DB().ExecContext(ctx,
			`INSERT OR IGNORE INTO delegation_state (link_id, active_count) VALUES (?, 0)`, id); err != nil {
			log.Printf("delegation: failed to init state for %s: %v", id, err)
		}
		seeded++
	}
	if seeded > 0 {
		log.Printf("delegation: seeded %d links from config", seeded)
	}
}
