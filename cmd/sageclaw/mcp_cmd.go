package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"

	"golang.org/x/term"

	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/mcp/registry"
	"github.com/xoai/sageclaw/pkg/store"
	sqliteStore "github.com/xoai/sageclaw/pkg/store/sqlite"
	"github.com/xoai/sageclaw/pkg/tool"
)

func runMCPCommand(args []string) error {
	if len(args) == 0 {
		printMCPUsage()
		return nil
	}

	switch args[0] {
	case "list":
		return mcpList(args[1:])
	case "categories":
		return mcpCategories()
	case "search":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp search <query>")
		}
		return mcpSearch(strings.Join(args[1:], " "))
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp info <id>")
		}
		return mcpInfo(args[1])
	case "install":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp install <id> [--config KEY=VALUE ...]")
		}
		return mcpInstall(args[1], args[2:])
	case "enable":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp enable <id>")
		}
		return mcpEnable(args[1])
	case "disable":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp disable <id>")
		}
		return mcpDisable(args[1])
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp remove <id>")
		}
		return mcpRemove(args[1])
	case "test":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp test <id>")
		}
		return mcpTest(args[1])
	case "assign":
		if len(args) < 2 {
			return fmt.Errorf("usage: sageclaw mcp assign <id> --agent <name>")
		}
		return mcpAssign(args[1], args[2:])
	case "update":
		return mcpUpdate(args[1:])
	default:
		return fmt.Errorf("unknown mcp command: %s\nRun 'sageclaw mcp' for usage.", args[0])
	}
}

func printMCPUsage() {
	fmt.Println(`Usage: sageclaw mcp <command>

Commands:
  list                       List all MCP servers (curated + installed)
  list --category <name>     Filter by category
  list --installed           Show only installed
  categories                 List categories with counts
  search <query>             Search by name, description, tags
  info <id>                  Show MCP server details
  install <id>               Install an MCP server (prompts for config)
  install <id> --config K=V  Install with inline config
  enable <id>                Enable a disabled MCP
  disable <id>               Disable (keep config)
  remove <id>                Uninstall + remove credentials
  test <id>                  Test connection
  assign <id> --agent <name> Assign to specific agent
  update                     Download fresh MCP index
  update --check             Show index version info`)
}

// --- Command implementations ---

func mcpList(args []string) error {
	idx, err := registry.LoadCuratedIndex()
	if err != nil {
		return err
	}

	// Parse flags.
	var category string
	var installedOnly bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--category":
			if i+1 < len(args) {
				category = args[i+1]
				i++
			}
		case "--installed":
			installedOnly = true
		}
	}

	// Get installed state from DB if available.
	installedMap := make(map[string]bool)
	enabledMap := make(map[string]bool)
	statusMap := make(map[string]string)
	if s, encKey, cleanup, err := openRegistryStore(); err == nil {
		defer cleanup()
		reg, _ := registry.NewRegistry(s, s, encKey, nil)
		if reg != nil {
			reg.SeedFromCurated(context.Background())
		}
		entries, _ := s.ListMCPEntries(context.Background(), store.MCPFilter{
			Status: []string{"installing", "connected", "disabled", "failed"},
		})
		for _, e := range entries {
			installedMap[e.ID] = true
			enabledMap[e.ID] = e.Enabled
			statusMap[e.ID] = e.Status
		}
	}

	// Group by category.
	type catGroup struct {
		cat     registry.Category
		servers []registry.CuratedServer
	}
	groups := make(map[string]*catGroup)
	for _, c := range idx.Categories {
		groups[c.ID] = &catGroup{cat: c}
	}
	for _, s := range idx.Servers {
		if category != "" && s.Category != category {
			continue
		}
		if installedOnly && !installedMap[s.ID] {
			continue
		}
		if g, ok := groups[s.Category]; ok {
			g.servers = append(g.servers, s)
		}
	}

	// Print.
	for _, c := range idx.Categories {
		g := groups[c.ID]
		if len(g.servers) == 0 {
			continue
		}
		fmt.Printf("\n%s %s (%d)\n", c.Icon, c.Name, len(g.servers))
		w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
		for _, s := range g.servers {
			status := ""
			switch statusMap[s.ID] {
			case "installing":
				status = "\033[33m[installing...]\033[0m"
			case "connected":
				status = "\033[32m[connected]\033[0m"
			case "disabled":
				status = "\033[33m[disabled]\033[0m"
			case "failed":
				status = "\033[31m[failed]\033[0m"
			}
			needsKey := ""
			if len(s.ConfigSchema) > 0 {
				needsKey = "🔑"
			}
			fmt.Fprintf(w, "  %-22s\t%-40s\t★ %-6d\t%s %s\n",
				s.ID, truncate(s.Description, 40), s.Stars, needsKey, status)
		}
		w.Flush()
	}
	fmt.Println()
	return nil
}

func mcpCategories() error {
	idx, err := registry.LoadCuratedIndex()
	if err != nil {
		return err
	}

	// Count installed per category.
	installedCounts := make(map[string]int)
	if s, _, cleanup, err := openRegistryStore(); err == nil {
		defer cleanup()
		counts, _ := s.CountMCPByCategory(context.Background())
		installedCounts = counts
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", "Icon", "Category", "Total", "Installed")
	fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", "----", "--------", "-----", "---------")
	for _, c := range idx.Categories {
		total := len(idx.ByCategory(c.ID))
		installed := installedCounts[c.ID]
		fmt.Fprintf(w, "  %s\t%-25s\t%d\t%d\n", c.Icon, c.Name, total, installed)
	}
	w.Flush()
	return nil
}

func mcpSearch(query string) error {
	idx, err := registry.LoadCuratedIndex()
	if err != nil {
		return err
	}

	results := idx.Search(query)
	if len(results) == 0 {
		fmt.Printf("No results for %q\n", query)
		return nil
	}

	fmt.Printf("Found %d result(s) for %q:\n\n", len(results), query)
	w := tabwriter.NewWriter(os.Stdout, 2, 0, 2, ' ', 0)
	for _, s := range results {
		needsKey := ""
		if len(s.ConfigSchema) > 0 {
			needsKey = "🔑"
		}
		fmt.Fprintf(w, "  %-22s\t%-45s\t%-12s\t★ %d\t%s\n",
			s.ID, truncate(s.Description, 45), s.Category, s.Stars, needsKey)
	}
	w.Flush()
	return nil
}

func mcpInfo(id string) error {
	idx, err := registry.LoadCuratedIndex()
	if err != nil {
		return err
	}

	s, ok := idx.Get(id)
	if !ok {
		return fmt.Errorf("MCP %q not found in curated index", id)
	}

	fmt.Printf("%s — %s\n\n", s.Name, s.Description)
	fmt.Printf("  Category:    %s\n", s.Category)
	fmt.Printf("  Connection:  %s", s.Connection.Type)
	if s.Connection.URL != "" {
		fmt.Printf(" → %s", s.Connection.URL)
	} else if s.Connection.Command != "" {
		fmt.Printf(" → %s %s", s.Connection.Command, strings.Join(s.Connection.Args, " "))
	}
	fmt.Println()
	if s.FallbackConnection != nil {
		fmt.Printf("  Fallback:    %s", s.FallbackConnection.Type)
		if s.FallbackConnection.Command != "" {
			fmt.Printf(" → %s %s", s.FallbackConnection.Command, strings.Join(s.FallbackConnection.Args, " "))
		}
		fmt.Println()
	}
	if s.GitHub != "" {
		fmt.Printf("  GitHub:      %s  ★ %d\n", s.GitHub, s.Stars)
	}
	if len(s.Tags) > 0 {
		fmt.Printf("  Tags:        %s\n", strings.Join(s.Tags, ", "))
	}

	if len(s.ConfigSchema) > 0 {
		fmt.Printf("\n  Configuration required:\n")
		for name, field := range s.ConfigSchema {
			req := ""
			if field.Required {
				req = " (required)"
			}
			desc := ""
			if field.Description != "" {
				desc = " — " + field.Description
			}
			fmt.Printf("    %s%s%s\n", name, req, desc)
		}
	} else {
		fmt.Printf("\n  No configuration required (one-click install).\n")
	}

	// Check installed state.
	if st, _, cleanup, err := openRegistryStore(); err == nil {
		defer cleanup()
		if entry, err := st.GetMCPEntry(context.Background(), id); err == nil {
			fmt.Println()
			fmt.Printf("  Status:      %s\n", entry.Status)
			if entry.StatusError != "" {
				fmt.Printf("  Error:       %s\n", entry.StatusError)
			}
			if len(entry.AgentIDs) > 0 {
				fmt.Printf("  Agents:      %s\n", strings.Join(entry.AgentIDs, ", "))
			}
		}
	}

	return nil
}

func mcpInstall(id string, extraArgs []string) error {
	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	toolReg := tool.NewRegistry()
	mgr := mcp.NewManager(toolReg)

	reg, err := registry.NewRegistry(st, st, encKey, mgr)
	if err != nil {
		return err
	}
	reg.SeedFromCurated(context.Background())

	// Check if already installed.
	entry, err := st.GetMCPEntry(context.Background(), id)
	if err != nil {
		return fmt.Errorf("MCP %q not found. Run 'sageclaw mcp search' to find available servers.", id)
	}
	if entry.Installed {
		return fmt.Errorf("MCP %q is already installed. Use 'sageclaw mcp enable/disable' to manage it.", id)
	}

	// Parse --config flags or prompt interactively.
	config := parseConfigFlags(extraArgs)
	schema := parseConfigSchemaFromJSON(entry.ConfigSchema)

	if len(schema) > 0 && len(config) == 0 {
		// Interactive mode.
		fmt.Printf("Installing %s...\n\n", entry.Name)
		fmt.Println("Required configuration:")
		for name, field := range schema {
			if !field.Required {
				continue
			}
			desc := ""
			if field.Description != "" {
				desc = " (" + field.Description + ")"
			}
			fmt.Printf("  %s%s\n", name, desc)

			value := promptInput(name, isSensitiveField(name))
			if value == "" {
				return fmt.Errorf("required field %s cannot be empty", name)
			}
			config[name] = value
		}
		fmt.Println()
	}

	fmt.Print("Connecting... ")
	if err := reg.InstallSync(context.Background(), id, config); err != nil {
		fmt.Println("FAILED")
		return err
	}

	fmt.Println("OK")
	fmt.Printf("%s installed and connected.\n", entry.Name)
	return nil
}

func mcpEnable(id string) error {
	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	toolReg := tool.NewRegistry()
	mgr := mcp.NewManager(toolReg)
	reg, _ := registry.NewRegistry(st, st, encKey, mgr)

	if err := reg.Enable(context.Background(), id); err != nil {
		return err
	}
	fmt.Printf("MCP %s enabled.\n", id)
	return nil
}

func mcpDisable(id string) error {
	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	reg, _ := registry.NewRegistry(st, st, encKey, nil)
	if err := reg.Disable(context.Background(), id); err != nil {
		return err
	}
	fmt.Printf("MCP %s disabled.\n", id)
	return nil
}

func mcpRemove(id string) error {
	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	// Confirm.
	fmt.Printf("Remove MCP %q? This will delete stored credentials. [y/N] ", id)
	var confirm string
	fmt.Scanln(&confirm)
	if strings.ToLower(confirm) != "y" {
		fmt.Println("Cancelled.")
		return nil
	}

	reg, _ := registry.NewRegistry(st, st, encKey, nil)
	if err := reg.Remove(context.Background(), id); err != nil {
		return err
	}
	fmt.Printf("MCP %s removed.\n", id)
	return nil
}

func mcpTest(id string) error {
	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	reg, _ := registry.NewRegistry(st, st, encKey, nil)
	reg.SeedFromCurated(context.Background())

	// Load stored credentials if installed.
	config := make(map[string]string)
	entry, _ := st.GetMCPEntry(context.Background(), id)
	if entry != nil && entry.Installed {
		loaded, err := reg.LoadCredentials(context.Background(), id, entry.ConfigSchema)
		if err == nil {
			config = loaded
		}
	}

	fmt.Printf("Testing %s... ", id)
	result, err := reg.Test(context.Background(), id, config)
	if err != nil {
		fmt.Println("FAILED")
		return err
	}
	if !result.Success {
		fmt.Printf("FAILED: %s\n", result.Error)
		return nil
	}

	fmt.Printf("✓ %d tools available\n", result.ToolCount)
	for _, t := range result.Tools {
		fmt.Printf("  %-30s %s\n", t.Name, truncate(t.Description, 50))
	}
	return nil
}

func mcpAssign(id string, args []string) error {
	var agentName string
	for i := 0; i < len(args); i++ {
		if args[i] == "--agent" && i+1 < len(args) {
			agentName = args[i+1]
			i++
		}
	}
	if agentName == "" {
		return fmt.Errorf("usage: sageclaw mcp assign <id> --agent <name>")
	}

	st, encKey, cleanup, err := openRegistryStore()
	if err != nil {
		return fmt.Errorf("cannot open database: %w", err)
	}
	defer cleanup()

	reg, _ := registry.NewRegistry(st, st, encKey, nil)

	// Get current agents and add the new one.
	entry, err := st.GetMCPEntry(context.Background(), id)
	if err != nil {
		return fmt.Errorf("MCP %q not found", id)
	}

	agents := entry.AgentIDs
	for _, a := range agents {
		if a == agentName {
			fmt.Printf("MCP %s is already assigned to agent %s.\n", id, agentName)
			return nil
		}
	}
	agents = append(agents, agentName)

	if err := reg.AssignAgents(context.Background(), id, agents); err != nil {
		return err
	}
	fmt.Printf("MCP %s assigned to agent %s.\n", id, agentName)
	return nil
}

func mcpUpdate(args []string) error {
	checkOnly := false
	for _, a := range args {
		if a == "--check" {
			checkOnly = true
		}
	}

	// Show embedded version.
	embedded, err := registry.LoadCuratedIndex()
	if err != nil {
		return fmt.Errorf("loading embedded index: %w", err)
	}
	embV := registry.GetIndexVersion(embedded)
	fmt.Printf("Embedded: v%d, %d servers (%s)\n", embV.Version, embV.Servers, embV.UpdatedAt)

	// Show local version if exists.
	dbPath := envOrDefault("SAGECLAW_DB", defaultDBPath())
	dataDir := filepath.Dir(dbPath)
	localPath := filepath.Join(dataDir, registry.IndexFilename)
	local, _ := registry.LoadLocalIndex(localPath)
	if local != nil {
		lv := registry.GetIndexVersion(local)
		fmt.Printf("Local:    v%d, %d servers (%s)\n", lv.Version, lv.Servers, lv.UpdatedAt)
	} else {
		fmt.Printf("Local:    (none)\n")
	}

	indexURL := os.Getenv("SAGECLAW_MCP_INDEX_URL")
	if indexURL == "" {
		indexURL = registry.DefaultIndexURL
	}

	if checkOnly {
		// Fetch remote to show version comparison.
		fmt.Print("Checking remote... ")
		tmpPath := filepath.Join(os.TempDir(), "sageclaw-mcp-check.json.gz")
		remote, err := registry.DownloadIndex(indexURL, tmpPath)
		os.Remove(tmpPath)
		if err != nil {
			fmt.Printf("failed (%v)\n", err)
		} else {
			rv := registry.GetIndexVersion(remote)
			label := ""
			currentV := embV.Version
			if local != nil {
				currentV = registry.GetIndexVersion(local).Version
			}
			if rv.Version > currentV {
				label = " — update available"
			} else {
				label = " — up to date"
			}
			fmt.Printf("v%d, %d servers (%s)%s\n", rv.Version, rv.Servers, rv.UpdatedAt, label)
		}
		return nil
	}

	fmt.Print("Downloading MCP index... ")
	idx, err := registry.DownloadIndex(indexURL, localPath)
	if err != nil {
		fmt.Println("FAILED")
		return err
	}

	rv := registry.GetIndexVersion(idx)
	fmt.Printf("done (%d servers, v%d, %s).\n", rv.Servers, rv.Version, rv.UpdatedAt)
	fmt.Println("Index saved. New servers will appear on next start.")

	// If we can open the store, re-seed live.
	if st, encKey, cleanup, err := openRegistryStore(); err == nil {
		defer cleanup()
		toolReg := tool.NewRegistry()
		mgr := mcp.NewManager(toolReg)
		reg, err := registry.NewRegistry(st, st, encKey, mgr)
		if err == nil {
			reg.SetDataDir(dataDir)
			reg.LoadLocalOverride()
			if err := reg.SeedFromCurated(context.Background()); err != nil {
				fmt.Printf("Warning: re-seed failed: %v\n", err)
			} else {
				fmt.Println("Registry re-seeded with new index.")
			}
		}
	}

	return nil
}

// --- Helpers ---

func openRegistryStore() (*sqliteStore.Store, []byte, func(), error) {
	dbPath := envOrDefault("SAGECLAW_DB", defaultDBPath())
	st, err := sqliteStore.New(dbPath)
	if err != nil {
		return nil, nil, nil, err
	}
	encKey, err := loadOrGenerateEncKey(st.DB())
	if err != nil {
		st.Close()
		return nil, nil, nil, err
	}
	return st, encKey, func() { st.Close() }, nil
}

func parseConfigFlags(args []string) map[string]string {
	config := make(map[string]string)
	for i := 0; i < len(args); i++ {
		if args[i] == "--config" && i+1 < len(args) {
			i++
			parts := strings.SplitN(args[i], "=", 2)
			if len(parts) == 2 {
				config[parts[0]] = parts[1]
			}
		}
	}
	return config
}

func parseConfigSchemaFromJSON(schemaJSON string) map[string]registry.ConfigField {
	if schemaJSON == "" || schemaJSON == "{}" {
		return nil
	}
	var schema map[string]registry.ConfigField
	if err := parseJSON(schemaJSON, &schema); err != nil {
		return nil
	}
	return schema
}

func parseJSON(s string, v any) error {
	return json.Unmarshal([]byte(s), v)
}


func isSensitiveField(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "KEY") || strings.Contains(upper, "SECRET") ||
		strings.Contains(upper, "TOKEN") || strings.Contains(upper, "PASSWORD")
}

func promptInput(label string, masked bool) string {
	if masked {
		fmt.Printf("  Enter %s (input hidden): ", label)
		pw, err := term.ReadPassword(int(syscall.Stdin))
		fmt.Println() // newline after hidden input
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(pw))
	}
	fmt.Printf("  Enter %s: ", label)
	var input string
	fmt.Scanln(&input)
	return input
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
