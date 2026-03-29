package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/xoai/sageclaw/pkg/mcp"
	"github.com/xoai/sageclaw/pkg/store"
)

// Registry coordinates the curated index, SQLite store, credential manager,
// and MCP Manager for marketplace operations.
type Registry struct {
	curated    atomic.Pointer[CuratedIndex]
	store      store.MCPRegistryStore
	creds      store.CredentialStore
	encKey     []byte
	manager    *mcp.Manager
	wg         sync.WaitGroup
	mu         sync.Mutex
	installing map[string]bool // in-progress install guard
	updateMu   sync.Mutex      // serializes index updates
	dataDir    string           // for local index override
	indexURL   string           // remote index URL
}

// NewRegistry creates a new MCP marketplace registry.
func NewRegistry(
	st store.MCPRegistryStore,
	creds store.CredentialStore,
	encKey []byte,
	manager *mcp.Manager,
) (*Registry, error) {
	idx, err := LoadCuratedIndex()
	if err != nil {
		return nil, fmt.Errorf("loading curated index: %w", err)
	}
	r := &Registry{
		store:      st,
		creds:      creds,
		encKey:     encKey,
		manager:    manager,
		installing: make(map[string]bool),
	}
	r.curated.Store(idx)
	return r, nil
}

// SetDataDir sets the directory for local index override.
// Call before SeedFromCurated.
func (r *Registry) SetDataDir(dir string) {
	r.dataDir = dir
}

// SetIndexURL sets the remote index download URL.
func (r *Registry) SetIndexURL(url string) {
	r.indexURL = url
}

// IndexURL returns the configured or default index URL.
func (r *Registry) IndexURL() string {
	if r.indexURL != "" {
		return r.indexURL
	}
	return DefaultIndexURL
}

// LoadLocalOverride checks for a local index file and uses it if
// it has a newer version than the embedded index.
func (r *Registry) LoadLocalOverride() {
	if r.dataDir == "" {
		return
	}
	path := filepath.Join(r.dataDir, IndexFilename)
	local, err := LoadLocalIndex(path)
	if err != nil {
		log.Printf("mcp-registry: local index corrupt, using embedded: %v", err)
		return
	}
	if local == nil {
		return // no local index
	}
	current := r.curated.Load()
	if local.Version > current.Version {
		log.Printf("mcp-registry: using local index v%d (%d servers) over embedded v%d",
			local.Version, len(local.Servers), current.Version)
		r.curated.Store(local)
	}
}

// CuratedIndex returns the active curated index.
func (r *Registry) CuratedIndex() *CuratedIndex {
	return r.curated.Load()
}

// UpdateIndex downloads a fresh index from the remote URL, saves it locally,
// and live-reseeds the registry. Thread-safe via updateMu.
func (r *Registry) UpdateIndex(ctx context.Context) (*IndexVersion, error) {
	r.updateMu.Lock()
	defer r.updateMu.Unlock()

	if r.dataDir == "" {
		return nil, fmt.Errorf("data directory not configured")
	}

	destPath := filepath.Join(r.dataDir, IndexFilename)
	idx, err := DownloadIndex(r.IndexURL(), destPath)
	if err != nil {
		return nil, err
	}

	r.curated.Store(idx)

	if err := r.SeedFromCurated(ctx); err != nil {
		return nil, fmt.Errorf("re-seeding after update: %w", err)
	}

	v := GetIndexVersion(idx)
	log.Printf("mcp-registry: updated to v%d (%d servers)", v.Version, v.Servers)
	return &v, nil
}

// SeedFromCurated populates the registry with curated index entries.
// Preserves user state (installed, enabled, agent_ids) for existing entries.
// Removes orphan curated entries that no longer exist in the index.
func (r *Registry) SeedFromCurated(ctx context.Context) error {
	idx := r.curated.Load()
	storedVersion, _ := r.store.GetMCPSeedVersion(ctx)
	if storedVersion == idx.Version {
		return nil
	}

	// Build set of current curated IDs for orphan detection.
	curatedIDs := make(map[string]struct{}, len(idx.Servers))
	for _, server := range idx.Servers {
		curatedIDs[server.ID] = struct{}{}

		existing, _ := r.store.GetMCPEntry(ctx, server.ID)
		if existing != nil && existing.Status != "" && existing.Status != "available" {
			// Update metadata but preserve user state.
			fresh := curatedToEntry(server)
			fresh.Installed = existing.Installed
			fresh.Enabled = existing.Enabled
			fresh.Status = existing.Status
			fresh.StatusError = existing.StatusError
			fresh.AgentIDs = existing.AgentIDs
			fresh.InstalledAt = existing.InstalledAt
			r.store.UpsertMCPEntry(ctx, fresh)
			continue
		}

		entry := curatedToEntry(server)
		r.store.UpsertMCPEntry(ctx, entry)
	}

	// Remove orphan curated entries no longer in the index.
	all, _ := r.store.ListMCPEntries(ctx, store.MCPFilter{Source: "curated"})
	for _, e := range all {
		if _, ok := curatedIDs[e.ID]; !ok {
			r.store.DeleteMCPEntry(ctx, e.ID)
		}
	}

	return r.store.SetMCPSeedVersion(ctx, idx.Version)
}

// acquireInstall atomically checks and marks an ID as in-progress.
// Returns false if already in progress.
func (r *Registry) acquireInstall(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.installing[id] {
		return false
	}
	r.installing[id] = true
	return true
}

func (r *Registry) releaseInstall(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.installing, id)
}

// Install validates config, stores credentials, sets status to "installing",
// and returns immediately. Connection happens in a background goroutine.
func (r *Registry) Install(ctx context.Context, id string, userConfig map[string]string) error {
	entry, err := r.store.GetMCPEntry(ctx, id)
	if err != nil {
		return fmt.Errorf("MCP %q not found", id)
	}
	if !r.acquireInstall(id) {
		return fmt.Errorf("MCP %q installation already in progress", id)
	}
	if entry.Status == "installing" {
		r.releaseInstall(id)
		return fmt.Errorf("MCP %q installation already in progress", id)
	}
	if entry.Status == "connected" || entry.Status == "disabled" {
		r.releaseInstall(id)
		return fmt.Errorf("MCP %q is already installed", id)
	}

	// Validate required config fields.
	schema := parseConfigSchema(entry.ConfigSchema)
	for field, def := range schema {
		if def.Required {
			if _, ok := userConfig[field]; !ok {
				r.releaseInstall(id)
				return fmt.Errorf("required config field %q missing", field)
			}
		}
	}

	// Validate config values.
	for field, value := range userConfig {
		if err := validateConfigValue(field, value); err != nil {
			r.releaseInstall(id)
			return err
		}
	}

	// Store credentials immediately.
	for field, value := range userConfig {
		if err := r.creds.StoreCredential(ctx, "mcp:"+id+":"+field, []byte(value), r.encKey); err != nil {
			r.releaseInstall(id)
			return fmt.Errorf("storing credential %s: %w", field, err)
		}
	}

	// Set status to installing and return immediately.
	if err := r.store.SetMCPStatus(ctx, id, "installing", ""); err != nil {
		r.releaseInstall(id)
		return fmt.Errorf("updating status: %w", err)
	}

	// Background: connect with 600s timeout.
	r.connectBackground(id, entry)
	return nil
}

// Retry re-attempts installation for a failed MCP using stored credentials.
func (r *Registry) Retry(ctx context.Context, id string) error {
	entry, err := r.store.GetMCPEntry(ctx, id)
	if err != nil {
		return fmt.Errorf("MCP %q not found", id)
	}
	if !r.acquireInstall(id) {
		return fmt.Errorf("MCP %q installation already in progress", id)
	}
	if entry.Status != "failed" {
		r.releaseInstall(id)
		return fmt.Errorf("MCP %q is not in failed state", id)
	}

	if err := r.store.SetMCPStatus(ctx, id, "installing", ""); err != nil {
		r.releaseInstall(id)
		return fmt.Errorf("updating status: %w", err)
	}

	r.connectBackground(id, entry)
	return nil
}

// Enable reconnects a disabled MCP server (async).
func (r *Registry) Enable(ctx context.Context, id string) error {
	entry, err := r.store.GetMCPEntry(ctx, id)
	if err != nil {
		return fmt.Errorf("MCP %q not found", id)
	}
	if !r.acquireInstall(id) {
		return fmt.Errorf("MCP %q is already connecting", id)
	}
	if entry.Status != "disabled" {
		r.releaseInstall(id)
		return fmt.Errorf("MCP %q is not disabled (status: %s)", id, entry.Status)
	}

	if err := r.store.SetMCPStatus(ctx, id, "installing", ""); err != nil {
		r.releaseInstall(id)
		return fmt.Errorf("updating status: %w", err)
	}

	r.connectBackground(id, entry)
	return nil
}

// Disable stops an MCP server but keeps credentials and registry entry.
func (r *Registry) Disable(ctx context.Context, id string) error {
	r.store.SetMCPStatus(ctx, id, "disabled", "")
	if r.manager != nil {
		r.manager.RemoveServer(id)
	}
	return nil
}

// Remove disconnects, deletes credentials, and resets registry entry.
func (r *Registry) Remove(ctx context.Context, id string) error {
	if r.manager != nil {
		r.manager.RemoveServer(id)
	}

	entry, _ := r.store.GetMCPEntry(ctx, id)
	if entry != nil {
		schema := parseConfigSchema(entry.ConfigSchema)
		r.deleteCredentials(ctx, id, schema)
	}

	r.store.SetMCPStatus(ctx, id, "available", "")
	r.store.SetMCPAgents(ctx, id, nil)
	return nil
}

// Test validates an MCP connection without installing.
func (r *Registry) Test(ctx context.Context, id string, userConfig map[string]string) (*TestResult, error) {
	entry, err := r.store.GetMCPEntry(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("MCP %q not found", id)
	}
	cfg := r.buildServerConfig(id, entry, userConfig)
	return r.testConnection(ctx, id, cfg)
}

// AssignAgents sets which agents can use this MCP's tools.
func (r *Registry) AssignAgents(ctx context.Context, id string, agentIDs []string) error {
	return r.store.SetMCPAgents(ctx, id, agentIDs)
}

// Close waits for all background goroutines to finish (10s deadline).
func (r *Registry) Close() {
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		log.Println("mcp-registry: shutdown timeout, some installs may be incomplete")
	}
}

// InstallSync is like Install but blocks until connection completes.
// Used by CLI commands where the process exits after the call.
func (r *Registry) InstallSync(ctx context.Context, id string, userConfig map[string]string) error {
	entry, err := r.store.GetMCPEntry(ctx, id)
	if err != nil {
		return fmt.Errorf("MCP %q not found", id)
	}
	if entry.Status == "connected" || entry.Status == "disabled" {
		return fmt.Errorf("MCP %q is already installed", id)
	}

	schema := parseConfigSchema(entry.ConfigSchema)
	for field, def := range schema {
		if def.Required {
			if _, ok := userConfig[field]; !ok {
				return fmt.Errorf("required config field %q missing", field)
			}
		}
	}
	for field, value := range userConfig {
		if err := validateConfigValue(field, value); err != nil {
			return err
		}
	}

	for field, value := range userConfig {
		if err := r.creds.StoreCredential(ctx, "mcp:"+id+":"+field, []byte(value), r.encKey); err != nil {
			return fmt.Errorf("storing credential %s: %w", field, err)
		}
	}

	r.store.SetMCPStatus(ctx, id, "installing", "")

	config, _ := r.LoadCredentials(ctx, id, entry.ConfigSchema)
	cfg := r.buildServerConfig(id, entry, config)
	err = r.tryConnect(ctx, id, cfg)

	if err != nil && entry.FallbackConn != "" {
		fallbackCfg := r.buildServerConfigFromConn(id, entry.FallbackConn, config)
		if fbErr := r.tryConnect(ctx, id, fallbackCfg); fbErr == nil {
			err = nil
		}
	}

	if err != nil {
		r.store.SetMCPStatus(ctx, id, "failed", err.Error())
		return fmt.Errorf("connection failed: %w", err)
	}

	r.store.SetMCPStatus(ctx, id, "connected", "")
	return nil
}

// connectBackground spawns a goroutine that connects to an MCP server.
// Tries primary connection, then fallback if available.
// Updates status to "connected" on success or "failed" on error.
func (r *Registry) connectBackground(id string, entry *store.MCPRegistryEntry) {
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer r.releaseInstall(id)

		ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
		defer cancel()

		config, _ := r.LoadCredentials(ctx, id, entry.ConfigSchema)

		// Try primary connection.
		cfg := r.buildServerConfig(id, entry, config)
		err := r.tryConnect(ctx, id, cfg)

		// Try fallback if primary fails.
		if err != nil && entry.FallbackConn != "" {
			log.Printf("mcp-registry: %s primary failed, trying fallback: %v", id, err)
			fallbackCfg := r.buildServerConfigFromConn(id, entry.FallbackConn, config)
			if fbErr := r.tryConnect(ctx, id, fallbackCfg); fbErr == nil {
				err = nil
			}
		}

		if err != nil {
			log.Printf("mcp-registry: %s install failed: %v", id, err)
			r.store.SetMCPStatus(ctx, id, "failed", err.Error())
			return
		}

		r.store.SetMCPStatus(ctx, id, "connected", "")
		log.Printf("mcp-registry: %s connected", id)
	}()
}

// tryConnect starts an MCP client and registers it with the manager.
func (r *Registry) tryConnect(ctx context.Context, id string, cfg mcp.MCPServerConfig) error {
	r.manager.RemoveServer(id) // safe no-op
	return r.manager.AddServer(id, cfg)
}

// StartInstalled connects all connected MCPs at startup.
// Also retries entries stuck in "installing" state (interrupted).
func (r *Registry) StartInstalled(ctx context.Context) {
	entries, err := r.store.ListMCPEntries(ctx, store.MCPFilter{
		Status: []string{"connected", "installing"},
	})
	if err != nil {
		log.Printf("mcp-registry: failed to list MCPs: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.Status == "installing" {
			// Retry interrupted installs.
			log.Printf("mcp-registry: %s was interrupted, retrying", entry.ID)
			r.connectBackground(entry.ID, &entry)
			continue
		}

		config, err := r.LoadCredentials(ctx, entry.ID, entry.ConfigSchema)
		if err != nil {
			log.Printf("mcp-registry: %s credentials missing, skipping: %v", entry.ID, err)
			continue
		}

		cfg := r.buildServerConfig(entry.ID, &entry, config)
		r.manager.RemoveServer(entry.ID) // safe no-op
		if err := r.manager.AddServer(entry.ID, cfg); err != nil {
			log.Printf("mcp-registry: %s failed to start: %v", entry.ID, err)
		}
	}
}

// GetInstalledForAgent returns MCP server IDs connected for a specific agent.
// Empty agent_ids means the MCP is available to all agents.
func (r *Registry) GetInstalledForAgent(ctx context.Context, agentID string) ([]string, error) {
	entries, err := r.store.ListMCPEntries(ctx, store.MCPFilter{
		Status: []string{"connected"},
	})
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if len(e.AgentIDs) == 0 || containsStr(e.AgentIDs, agentID) {
			ids = append(ids, e.ID)
		}
	}
	return ids, nil
}

// TestResult holds the result of an MCP connection test.
type TestResult struct {
	Success   bool        `json:"success"`
	Error     string      `json:"error,omitempty"`
	ToolCount int         `json:"tool_count"`
	Tools     []ToolSummary `json:"tools,omitempty"`
}

// ToolSummary is a lightweight tool representation for API responses.
type ToolSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// --- Internal helpers ---

func (r *Registry) buildServerConfigFromConn(id, connJSON string, config map[string]string) mcp.MCPServerConfig {
	var conn ConnectionConfig
	json.Unmarshal([]byte(connJSON), &conn)
	return r.buildConfigFromConn(id, conn, config)
}

func (r *Registry) buildServerConfig(id string, entry *store.MCPRegistryEntry, config map[string]string) mcp.MCPServerConfig {
	var conn ConnectionConfig
	json.Unmarshal([]byte(entry.Connection), &conn)
	return r.buildConfigFromConn(id, conn, config)
}

func (r *Registry) buildConfigFromConn(id string, conn ConnectionConfig, config map[string]string) mcp.MCPServerConfig {

	cfg := mcp.MCPServerConfig{
		Transport:  conn.Type,
		Trust:      "untrusted",
		ToolPrefix: id + "_",
		TimeoutSec: conn.TimeoutSec,
	}

	switch conn.Type {
	case "stdio", "":
		cfg.Transport = "stdio"
		cfg.Command = conn.Command
		cfg.Args = conn.Args
		cfg.Env = config
	case "http", "sse", "streamable-http":
		cfg.URL = conn.URL
		headers := make(map[string]string)
		for k, v := range conn.Headers {
			headers[k] = v
		}
		// Set Authorization from the first credential field that looks like a token.
		// Priority: AUTH_TOKEN > API_KEY > TOKEN. Only one is used.
		if _, ok := headers["Authorization"]; !ok {
			authValue := pickAuthValue(config)
			if authValue != "" {
				headers["Authorization"] = "Bearer " + authValue
			}
		}
		if len(headers) > 0 {
			cfg.Headers = headers
		}
	}

	enabled := true
	cfg.Enabled = &enabled
	return cfg
}

// LoadCredentials retrieves stored credentials for an MCP from the credential store.
func (r *Registry) LoadCredentials(ctx context.Context, id string, schemaJSON string) (map[string]string, error) {
	schema := parseConfigSchema(schemaJSON)
	config := make(map[string]string)
	var missing []string

	for field, def := range schema {
		key := "mcp:" + id + ":" + field
		val, err := r.creds.GetCredential(ctx, key, r.encKey)
		if err != nil || len(val) == 0 {
			if def.Required {
				missing = append(missing, field)
			}
			continue
		}
		config[field] = string(val)
	}

	if len(missing) > 0 {
		return config, fmt.Errorf("missing credentials: %s", strings.Join(missing, ", "))
	}
	return config, nil
}

func (r *Registry) deleteCredentials(ctx context.Context, id string, schema map[string]ConfigField) {
	for field := range schema {
		r.store.DeleteMCPCredential(ctx, "mcp:"+id+":"+field)
	}
}

func (r *Registry) testConnection(ctx context.Context, id string, cfg mcp.MCPServerConfig) (*TestResult, error) {
	client, err := mcp.NewClientFromConfig(id+"-test", cfg)
	if err != nil {
		return &TestResult{Success: false, Error: err.Error()}, nil
	}

	testCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	if err := client.Start(testCtx); err != nil {
		client.Stop()
		return &TestResult{Success: false, Error: err.Error()}, nil
	}

	tools := client.Tools()
	client.Stop()

	summaries := make([]ToolSummary, len(tools))
	for i, t := range tools {
		summaries[i] = ToolSummary{Name: t.Name, Description: t.Description}
	}

	return &TestResult{
		Success:   true,
		ToolCount: len(tools),
		Tools:     summaries,
	}, nil
}

func parseConfigSchema(schemaJSON string) map[string]ConfigField {
	if schemaJSON == "" || schemaJSON == "{}" {
		return nil
	}
	var schema map[string]ConfigField
	json.Unmarshal([]byte(schemaJSON), &schema)
	return schema
}

func curatedToEntry(s CuratedServer) store.MCPRegistryEntry {
	conn, _ := json.Marshal(s.Connection)
	cs := "{}"
	if len(s.ConfigSchema) > 0 {
		b, _ := json.Marshal(s.ConfigSchema)
		cs = string(b)
	}

	entry := store.MCPRegistryEntry{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Connection:  string(conn),
		ConfigSchema: cs,
		GitHubURL:   s.GitHub,
		Stars:       s.Stars,
		Tags:        s.Tags,
		Source:      "curated",
	}

	if s.FallbackConnection != nil {
		fc, _ := json.Marshal(s.FallbackConnection)
		entry.FallbackConn = string(fc)
	}

	return entry
}

// validateConfigValue rejects values with shell metacharacters for env vars.
func validateConfigValue(field, value string) error {
	if len(value) > 4096 {
		return fmt.Errorf("config value for %q exceeds 4096 chars", field)
	}
	for _, ch := range value {
		if ch == ';' || ch == '|' || ch == '&' || ch == '$' || ch == '`' || ch == '\n' || ch == '\r' {
			return fmt.Errorf("config value for %q contains invalid character %q", field, ch)
		}
		if !unicode.IsPrint(ch) && ch != '\t' {
			return fmt.Errorf("config value for %q contains non-printable character", field)
		}
	}
	return nil
}

// pickAuthValue selects the best credential for Authorization header.
// Scans config fields for auth-like names. If multiple match, the first
// found wins (map iteration order). Returns "" if no auth field found.
func pickAuthValue(config map[string]string) string {
	for field, value := range config {
		upper := strings.ToUpper(field)
		if strings.Contains(upper, "AUTH_TOKEN") || strings.Contains(upper, "API_KEY") ||
			strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") {
			return value
		}
	}
	return ""
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
