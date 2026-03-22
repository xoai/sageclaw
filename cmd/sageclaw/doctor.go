package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/config"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func runDoctor() {
	fmt.Println("SageClaw Doctor")
	fmt.Println("===============")

	// Check 1: Config.
	configPath := "configs"
	_, err := config.Load(configPath)
	if err != nil {
		printWarn("Config", fmt.Sprintf("cannot load from %s: %v", configPath, err))
	} else {
		printPass("Config", configPath)
	}

	// Check 2: Provider keys.
	providers := map[string]string{
		"ANTHROPIC_API_KEY": "Anthropic",
		"OPENAI_API_KEY":    "OpenAI",
		"GEMINI_API_KEY":    "Gemini",
	}
	found := 0
	for key, name := range providers {
		if os.Getenv(key) != "" {
			printPass("Provider", name+" API key found")
			found++
		}
	}
	if found == 0 {
		printWarn("Provider", "No API keys set (ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY)")
	}

	// Check 3: Database.
	dbPath := defaultDBPath()
	store, err := sqlite.New(dbPath)
	if err != nil {
		printFail("Database", fmt.Sprintf("cannot open %s: %v", dbPath, err))
	} else {
		printPass("Database", dbPath)

		// Check 4: FTS5.
		var ftsOK int
		err := store.DB().QueryRow("SELECT 1 FROM memories_fts LIMIT 0").Scan(&ftsOK)
		if err == nil {
			printPass("FTS5", "full-text search available")
		} else {
			// Table might be empty — check if it exists.
			var name string
			err2 := store.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='memories_fts'").Scan(&name)
			if err2 == nil {
				printPass("FTS5", "full-text search available (empty)")
			} else {
				printFail("FTS5", "FTS5 table not found")
			}
		}
		store.Close()
	}

	// Check 5: Agents directory.
	agentsDir := "agents"
	if entries, err := os.ReadDir(agentsDir); err == nil && len(entries) > 0 {
		printPass("Agents", fmt.Sprintf("%d agent(s) in %s/", len(entries), agentsDir))
	} else {
		printWarn("Agents", "no agents directory — create agents in the dashboard or agents/ folder")
	}

	// Check 6: Skills.
	skillsDir := "skills"
	if entries, err := os.ReadDir(skillsDir); err == nil {
		count := 0
		for _, e := range entries {
			if e.IsDir() {
				count++
			}
		}
		printPass("Skills", fmt.Sprintf("%d skill(s) in %s/", count, skillsDir))
	} else {
		printWarn("Skills", "no skills directory")
	}

	// Check 7: Templates.
	if entries, err := os.ReadDir("templates"); err == nil && len(entries) > 0 {
		printPass("Templates", fmt.Sprintf("%d template(s) available", len(entries)))
	} else {
		printWarn("Templates", "no templates found")
	}

	fmt.Println()
	fmt.Println("Run 'sageclaw onboard' for interactive setup.")
}

func runOnboard() {
	fmt.Println("Welcome to SageClaw!")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	// Step 1: API key.
	fmt.Print("Anthropic API key (or press Enter to skip): ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)

	if apiKey != "" {
		os.Setenv("ANTHROPIC_API_KEY", apiKey)
		fmt.Println("  ✓ API key set for this session")
		fmt.Println("  Note: Add to your shell profile for persistence:")
		fmt.Printf("  export ANTHROPIC_API_KEY=%s\n", apiKey)
	}

	// Step 2: Telegram (optional).
	fmt.Println()
	fmt.Print("Telegram bot token (or press Enter to skip): ")
	tgToken, _ := reader.ReadString('\n')
	tgToken = strings.TrimSpace(tgToken)

	if tgToken != "" {
		fmt.Println("  ✓ Telegram token noted")
		fmt.Println("  Note: Add to your shell profile:")
		fmt.Printf("  export TELEGRAM_BOT_TOKEN=%s\n", tgToken)
	}

	// Step 3: Generate config.
	os.MkdirAll("configs", 0755)

	agentsCfg := map[string]any{
		"agents": map[string]any{
			"sageclaw": map[string]any{
				"name":  "SageClaw",
				"model": "strong",
			},
		},
	}
	data, _ := json.MarshalIndent(agentsCfg, "", "  ")
	os.WriteFile("configs/agents.yaml", data, 0644)

	fmt.Println()
	fmt.Println("  ✓ configs/agents.yaml created")
	fmt.Println()
	fmt.Println("Run 'sageclaw doctor' to verify, then 'sageclaw' to start.")
}

func runBackup() {
	dbPath := defaultDBPath()
	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("sageclaw-backup-%s.db", timestamp)

	src, err := os.Open(dbPath)
	if err != nil {
		fmt.Printf("✗ Cannot open database: %v\n", err)
		os.Exit(1)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		fmt.Printf("✗ Cannot create backup: %v\n", err)
		os.Exit(1)
	}
	defer dst.Close()

	n, err := io.Copy(dst, src)
	if err != nil {
		fmt.Printf("✗ Backup failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Backup saved to %s (%d bytes)\n", backupPath, n)
}

func runRestore(backupPath string) {
	dbPath := defaultDBPath()

	fmt.Printf("This will replace %s with %s. Continue? [y/N] ", dbPath, backupPath)
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	if strings.TrimSpace(strings.ToLower(answer)) != "y" {
		fmt.Println("Cancelled.")
		return
	}

	src, err := os.Open(backupPath)
	if err != nil {
		fmt.Printf("✗ Cannot open backup: %v\n", err)
		os.Exit(1)
	}
	defer src.Close()

	dst, err := os.Create(dbPath)
	if err != nil {
		fmt.Printf("✗ Cannot write database: %v\n", err)
		os.Exit(1)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		fmt.Printf("✗ Restore failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Restored from %s\n", backupPath)
}

func printPass(name, detail string) { fmt.Printf("  ✓ %-12s %s\n", name, detail) }
func printFail(name, detail string) { fmt.Printf("  ✗ %-12s %s\n", name, detail) }
func printWarn(name, detail string) { fmt.Printf("  ⚠ %-12s %s\n", name, detail) }
