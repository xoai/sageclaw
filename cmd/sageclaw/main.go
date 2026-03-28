package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

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
	"github.com/xoai/sageclaw/pkg/channel/whatsapp"
	"github.com/xoai/sageclaw/pkg/channel/zalo"
	"github.com/xoai/sageclaw/pkg/channel/zalobot"
	"github.com/xoai/sageclaw/pkg/config"
	"github.com/xoai/sageclaw/pkg/mcp"
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
	"github.com/xoai/sageclaw/pkg/tool"
	"github.com/xoai/sageclaw/pkg/tui"
	"github.com/xoai/sageclaw/pkg/tunnel"
)

const version = "0.4.0-dev"

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
	if anthropicKey == "" {
		if key, err := appStore.GetCredential(initCtx, "provider_anthropic_anthropic", encKey); err == nil && len(key) > 0 {
			anthropicKey = string(key)
			log.Println("provider: anthropic key loaded from database")
		}
	}
	if openaiKey == "" {
		if key, err := appStore.GetCredential(initCtx, "provider_openai_openai", encKey); err == nil && len(key) > 0 {
			openaiKey = string(key)
			log.Println("provider: openai key loaded from database")
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
	geminiKey := os.Getenv("GEMINI_API_KEY")
	if geminiKey == "" {
		if key, err := appStore.GetCredential(initCtx, "provider_gemini_gemini", encKey); err == nil && len(key) > 0 {
			geminiKey = string(key)
			log.Println("provider: gemini key loaded from database")
		}
	}
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
	openrouterKey := os.Getenv("OPENROUTER_API_KEY")
	if openrouterKey == "" {
		if key, err := appStore.GetCredential(initCtx, "provider_openrouter_openrouter", encKey); err == nil && len(key) > 0 {
			openrouterKey = string(key)
			log.Println("provider: openrouter key loaded from database")
		}
	}
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
	githubToken := os.Getenv("GITHUB_COPILOT_TOKEN")
	if githubToken == "" {
		if key, err := appStore.GetCredential(initCtx, "provider_github_github", encKey); err == nil && len(key) > 0 {
			githubToken = string(key)
			log.Println("provider: github copilot token loaded from database")
		}
	}
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
	if ollamaClient.Healthy(ctx) {
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

		// Register providers for model discovery and combo resolution.
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

		// Load combos from DB into the router.
		comboRows, err := appStore.DB().Query(
			`SELECT id, name, strategy, models FROM combos ORDER BY name`)
		if err == nil {
			defer comboRows.Close()
			for comboRows.Next() {
				var id, name, strategy, modelsJSON string
				comboRows.Scan(&id, &name, &strategy, &modelsJSON)
				var models []provider.ComboModel
				if json.Unmarshal([]byte(modelsJSON), &models) == nil && len(models) > 0 {
					router.SetCombo(id, provider.Combo{
						Name:     name,
						Strategy: strategy,
						Models:   models,
					})
				}
			}
		}
	}

	// --- Tool registry ---
	toolReg := tool.NewRegistry()
	tool.RegisterFS(toolReg, sandbox)
	tool.RegisterExec(toolReg, sandbox.Root())
	tool.RegisterWeb(toolReg)
	tool.RegisterMemory(toolReg, memEngine)
	tool.RegisterGraph(toolReg, graphOps)
	tool.RegisterCron(toolReg, appStore)
	tool.RegisterSpawn(toolReg)
	tool.RegisterPlan(toolReg)
	tool.RegisterSkillLoader(toolReg, skillsDir)

	// --- Agent configs (file-first, with DB/YAML fallback) ---
	agentsDir := filepath.Join(f.workspace, "agents")
	agentConfigs := map[string]agent.Config{}

	// Try file-based agent configs first.
	fileAgents, err := agentcfg.LoadAll(agentsDir)
	if err != nil {
		log.Printf("agentcfg: %v (falling back to YAML/DB)", err)
	}

	// Agent config provider for runtime consumers (pipeline, handlers).
	agentProvider := agentcfg.NewMapProvider(fileAgents)

	// Forward-declare loopPool so the file watcher closure can reference it.
	var loopPool *agent.LoopPool

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
			agentConfigs[agentID] = agentcfg.ToRuntimeConfig(reloaded)
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
					Tools:        ac.Tools,
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

	// Delegation links from config.
	var delegationLinks []store.DelegationLink
	for _, dl := range cfg.Delegation {
		maxC := dl.MaxConcurrent
		if maxC == 0 {
			maxC = 1
		}
		delegationLinks = append(delegationLinks, store.DelegationLink{
			ID: fmt.Sprintf("link_%s_%s", dl.Source, dl.Target),
			SourceID: dl.Source, TargetID: dl.Target,
			Direction: dl.Direction, MaxConcurrent: maxC,
			TimeoutSec: dl.TimeoutSec,
		})
	}

	delegator := orchestration.NewDelegator(appStore, agentConfigs, delegationLinks, defaultProvider, router, toolReg)

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

	// Teams from config.
	var teams []orchestration.Team
	for _, tc := range cfg.Teams {
		teams = append(teams, orchestration.Team{
			ID: tc.ID, Name: tc.Name, LeadID: tc.Lead, Members: tc.Members,
		})
	}
	teamMgr := orchestration.NewTeamManager(appStore, teams)
	tool.RegisterTeam(toolReg, teamMgr)

	// Handoff.
	agentNames := map[string]string{"default": "SageClaw"}
	handoffMgr := orchestration.NewHandoff(appStore, agentNames)
	tool.RegisterHandoff(toolReg, func(ctx context.Context, sessionID, sourceAgentID, targetAgentID, reason string) error {
		return handoffMgr.Transfer(ctx, sessionID, sourceAgentID, targetAgentID, reason)
	})

	// Evaluate loop.
	tool.RegisterEvaluate(toolReg, func(ctx context.Context, prompt string, maxRounds int, threshold float64) (string, float64, int, error) {
		genConfig := agentConfigs["default"]
		evalConfig := agentConfigs["default"]
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

	if !noProviders {
		loopPool = agent.NewLoopPool(agentConfigs, defaultProvider, toolReg, preCtx, postTool,
			func(e agent.Event) {
				switch e.Type {
				case agent.EventRunStarted:
					log.Printf("[%s] run started", e.SessionID)
				case agent.EventRunCompleted:
					log.Printf("[%s] run completed", e.SessionID)
				case agent.EventToolCall:
					if e.ToolCall != nil {
						log.Printf("[%s] tool call: %s", e.SessionID, e.ToolCall.Name)
					}
				case agent.EventConsentNeeded:
					if e.Consent != nil {
						log.Printf("[%s] consent needed: %s (%s/%s)", e.SessionID, e.Consent.ToolName, e.Consent.Group, e.Consent.RiskLevel)
					}
				case agent.EventRunFailed:
					log.Printf("[%s] run failed: %v", e.SessionID, e.Error)
				}
				// Broadcast to SSE clients (web dashboard).
				if sseBroadcast != nil {
					sseBroadcast(e)
				}
				// Forward streaming events to Telegram adapters.
				if telegramEventForwarder != nil {
					telegramEventForwarder(e)
				}
			}, loopOpts...)
	}

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

	// --- Bus + Pipeline ---
	msgBus := localbus.New()

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
		return telegram.New(connID, token, telegram.WithAudioStore(audioStore)), nil
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
		return discord.New(connID, token), nil
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
		return zalo.NewFromCredentials(connID, creds), nil
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
		return zalobot.New(connID, token), nil
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
		return whatsapp.NewFromCredentials(connID, creds), nil
	})

	var p *pipeline.Pipeline
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
		CostRecorder: func(ctx context.Context, sessionID, agentID, provName, model string, usage canonical.Usage) {
			budgetEngine.RecordCost(ctx, provider.CostEntry{
				SessionID:     sessionID,
				AgentID:       agentID,
				Provider:      provName,
				Model:         model,
				InputTokens:   usage.InputTokens,
				OutputTokens:  usage.OutputTokens,
				CacheCreation: usage.CacheCreation,
				CacheRead:     usage.CacheRead,
			})
		},
	})

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

	if loopPool != nil {
		if err := p.Start(startCtx); err != nil {
			return fmt.Errorf("starting pipeline: %w", err)
		}
		cronRunner.Start(startCtx)
	} else {
		log.Println("agent: disabled (no providers configured)")
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
	if ollamaClient.Healthy(ctx) {
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

	rpcServer := rpc.NewServer(appStore, memEngine, msgBus, rpc.Config{ListenAddr: rpcAddr},
		rpc.WithGraphEngine(graphOps),
		rpc.WithToolRegistry(toolReg),
		rpc.WithProviderHealth(providerHealth),
		rpc.WithTunnel(tunnelMgr),
		rpc.WithAgentsDir(agentsDir),
		rpc.WithPairing(pairingMgr),
		rpc.WithBudgetEngine(budgetEngine),
		rpc.WithEncryptionKey(encKey),
		rpc.WithRouter(router),
		rpc.WithChannelManager(chanMgr),
		rpc.WithMCPManager(mcpMgr),
		rpc.WithSkillStore(skillStore),
		rpc.WithConsentHandler(p.InjectConsent),
		rpc.WithConsentHandlerLegacy(p.InjectConsentLegacy),
		rpc.WithConsentStore(consentStore),
		rpc.WithAudioBasePath(audioStoragePath),
	)
	// Wire SSE broadcast now that rpcServer exists.
	sseBroadcast = rpcServer.EventHandler()

	if err := rpcServer.Start(startCtx); err != nil {
		log.Printf("warning: dashboard server failed: %v", err)
	} else {
		log.Printf("dashboard: http://localhost%s", rpcAddr)
		defer rpcServer.Stop(startCtx)
	}

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
		tg := telegram.New("telegram", telegramToken, telegram.WithAudioStore(audioStore))
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

	// --- Telegram streaming forwarder ---
	// Forward EventChunk/EventRunCompleted events to Telegram adapters
	// for progressive message editing.
	telegramEventForwarder = func(e agent.Event) {
		if e.Type != agent.EventChunk && e.Type != agent.EventRunCompleted && e.Type != agent.EventRunFailed {
			return
		}
		if e.SessionID == "" {
			return
		}
		// Look up the session to get channel and chatID.
		sess, err := appStore.GetSession(startCtx, e.SessionID)
		if err != nil || sess == nil {
			return
		}
		// Only forward to Telegram channels.
		if !strings.HasPrefix(sess.Channel, "tg_") && sess.Channel != "telegram" {
			return
		}
		// Find the Telegram adapter via channel manager.
		chanMgr.ForEachChannel(func(ch channel.Channel) {
			if tg, ok := ch.(*telegram.Adapter); ok && (tg.ConnID() == sess.Channel) {
				tg.OnAgentEvent(e.SessionID, sess.ChatID, string(e.Type), e.Text)
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
	cronRunner.Stop()
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
