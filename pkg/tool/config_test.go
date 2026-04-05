package tool

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

func newTestConfigStore(t *testing.T) (*ConfigStore, *sqlite.Store) {
	t.Helper()
	store, err := sqlite.New(":memory:")
	if err != nil {
		t.Fatalf("creating store: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	reg := NewRegistry()
	reg.Register("web_search", "Search the web", nil, nil)
	reg.SetConfigSchema("web_search", map[string]ToolConfigField{
		"brave_api_key": {Type: "password", Required: false, Description: "Brave API key", Link: "https://brave.com/search/api/"},
	})
	reg.Register("web_fetch", "Fetch a URL", nil, nil)
	reg.SetConfigSchema("web_fetch", map[string]ToolConfigField{
		"max_chars":    {Type: "number", Required: false, Description: "Max output chars", Default: 50000},
		"extract_mode": {Type: "select", Required: false, Description: "Output format", Default: "markdown", Options: []string{"markdown", "text"}},
	})
	reg.Register("exec", "Run command", nil, nil)
	reg.SetConfigSchema("exec", map[string]ToolConfigField{
		"api_key": {Type: "password", Required: true, Description: "Required key"},
	})

	encKey, err := sqlite.GenerateKey()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}

	cs := NewConfigStore(store, reg, encKey)
	return cs, store
}

func TestConfigGet_SchemaDefault(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	val := cs.Get(ctx, "web_fetch", "max_chars")
	if val != "50000" {
		t.Errorf("expected schema default 50000, got %q", val)
	}

	val = cs.Get(ctx, "web_fetch", "extract_mode")
	if val != "markdown" {
		t.Errorf("expected schema default markdown, got %q", val)
	}
}

func TestConfigGet_GlobalOverDefault(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "web_fetch", "max_chars", "25000"); err != nil {
		t.Fatal(err)
	}
	val := cs.Get(ctx, "web_fetch", "max_chars")
	if val != "25000" {
		t.Errorf("expected global value 25000, got %q", val)
	}
}

func TestConfigGet_AgentOverrideOverGlobal(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "web_fetch", "max_chars", "25000"); err != nil {
		t.Fatal(err)
	}

	agentCfg := map[string]map[string]any{
		"web_fetch": {"max_chars": 10000},
	}
	ctx = WithToolConfig(ctx, agentCfg)

	val := cs.Get(ctx, "web_fetch", "max_chars")
	if val != "10000" {
		t.Errorf("expected agent override 10000, got %q", val)
	}
}

func TestConfigSetGlobal_CacheInvalidation(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	// Warm cache.
	cs.Get(ctx, "web_fetch", "max_chars")

	// Set new value (should invalidate cache).
	if err := cs.SetGlobal(ctx, "web_fetch", "max_chars", "30000"); err != nil {
		t.Fatal(err)
	}

	val := cs.Get(ctx, "web_fetch", "max_chars")
	if val != "30000" {
		t.Errorf("expected 30000 after cache invalidation, got %q", val)
	}
}

func TestConfigSetGlobal_EncryptsPasswordFields(t *testing.T) {
	cs, store := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "web_search", "brave_api_key", "sk-secret-key"); err != nil {
		t.Fatal(err)
	}

	// Read raw value from settings — should be base64-encoded ciphertext.
	raw, err := store.GetSetting(ctx, "tool_config:web_search:brave_api_key")
	if err != nil {
		t.Fatal(err)
	}
	if raw == "sk-secret-key" {
		t.Error("password stored in plaintext — should be encrypted")
	}
	// Verify it's valid base64.
	if _, err := base64.StdEncoding.DecodeString(raw); err != nil {
		t.Errorf("encrypted value is not valid base64: %v", err)
	}

	// ConfigStore.Get should decrypt it.
	val := cs.Get(ctx, "web_search", "brave_api_key")
	if val != "sk-secret-key" {
		t.Errorf("expected decrypted value sk-secret-key, got %q", val)
	}
}

func TestConfigGetGlobalAll_MasksPasswords(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "web_search", "brave_api_key", "sk-secret-key-12345"); err != nil {
		t.Fatal(err)
	}

	all := cs.GetGlobalAll(ctx, "web_search")
	masked := all["brave_api_key"]
	if masked == "sk-secret-key-12345" {
		t.Error("password not masked in GetGlobalAll")
	}
	// Should end with last 4 chars.
	if len(masked) < 4 {
		t.Fatalf("masked value too short: %q", masked)
	}
	if masked[len(masked)-4:] != "2345" {
		t.Errorf("expected masked value ending with 2345, got %q", masked)
	}
}

func TestConfigValidate_MissingRequired(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	missing := cs.Validate(ctx, "exec")
	if len(missing) != 1 {
		t.Fatalf("expected 1 missing field, got %d", len(missing))
	}
	if missing[0].Name != "api_key" {
		t.Errorf("expected missing field api_key, got %s", missing[0].Name)
	}
}

func TestConfigValidate_PassesWhenSet(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "exec", "api_key", "my-key"); err != nil {
		t.Fatal(err)
	}
	missing := cs.Validate(ctx, "exec")
	if len(missing) != 0 {
		t.Errorf("expected no missing fields, got %d", len(missing))
	}
}

func TestConfigReader(t *testing.T) {
	cs, _ := newTestConfigStore(t)
	ctx := context.Background()

	if err := cs.SetGlobal(ctx, "web_fetch", "max_chars", "42000"); err != nil {
		t.Fatal(err)
	}

	reader := cs.Reader()
	val := reader(ctx, "web_fetch", "max_chars")
	if val != "42000" {
		t.Errorf("expected 42000, got %q", val)
	}
}

func TestConfigSchemaRegistration(t *testing.T) {
	reg := NewRegistry()
	reg.Register("test_tool", "Test", nil, nil)

	// No config initially.
	if _, ok := reg.GetConfigSchema("test_tool"); ok {
		t.Error("expected no schema before SetConfigSchema")
	}

	reg.SetConfigSchema("test_tool", map[string]ToolConfigField{
		"field1": {Type: "string"},
	})

	schema, ok := reg.GetConfigSchema("test_tool")
	if !ok {
		t.Fatal("expected schema after SetConfigSchema")
	}
	if _, exists := schema["field1"]; !exists {
		t.Error("expected field1 in schema")
	}
}

func TestListConfigurable(t *testing.T) {
	reg := NewRegistry()
	reg.Register("tool_a", "A", nil, nil)
	reg.Register("tool_b", "B", nil, nil)
	reg.Register("tool_c", "C", nil, nil)

	reg.SetConfigSchema("tool_a", map[string]ToolConfigField{
		"key": {Type: "string"},
	})
	reg.SetConfigSchema("tool_c", map[string]ToolConfigField{
		"key": {Type: "string"},
	})

	configurable := reg.ListConfigurable()
	if len(configurable) != 2 {
		t.Errorf("expected 2 configurable tools, got %d", len(configurable))
	}
}

func TestContextHelpers(t *testing.T) {
	ctx := context.Background()

	// No config on bare context.
	if cfg := ToolConfigFromContext(ctx); cfg != nil {
		t.Error("expected nil config on bare context")
	}

	// With config.
	agentCfg := map[string]map[string]any{
		"web_search": {"brave_api_key": "test"},
	}
	ctx = WithToolConfig(ctx, agentCfg)

	cfg := ToolConfigFromContext(ctx)
	if cfg == nil {
		t.Fatal("expected config from context")
	}
	if cfg["web_search"]["brave_api_key"] != "test" {
		t.Error("expected brave_api_key=test")
	}

	// Nil config is a no-op.
	ctx2 := WithToolConfig(context.Background(), nil)
	if ToolConfigFromContext(ctx2) != nil {
		t.Error("WithToolConfig(nil) should not set context value")
	}
}

func TestValidateValue_SelectField(t *testing.T) {
	cs, _ := newTestConfigStore(t)

	// Valid option.
	if msg := cs.ValidateValue("web_fetch", "extract_mode", "markdown"); msg != "" {
		t.Errorf("expected valid, got %q", msg)
	}
	if msg := cs.ValidateValue("web_fetch", "extract_mode", "text"); msg != "" {
		t.Errorf("expected valid, got %q", msg)
	}

	// Invalid option.
	if msg := cs.ValidateValue("web_fetch", "extract_mode", "xml"); msg == "" {
		t.Error("expected error for invalid select value 'xml'")
	}

	// Empty is always accepted.
	if msg := cs.ValidateValue("web_fetch", "extract_mode", ""); msg != "" {
		t.Errorf("expected empty to be valid, got %q", msg)
	}
}

func TestValidateValue_NumberField(t *testing.T) {
	cs, _ := newTestConfigStore(t)

	if msg := cs.ValidateValue("web_fetch", "max_chars", "50000"); msg != "" {
		t.Errorf("expected valid, got %q", msg)
	}
	if msg := cs.ValidateValue("web_fetch", "max_chars", "abc"); msg == "" {
		t.Error("expected error for non-numeric value")
	}
}

func TestValidateValue_UnknownField(t *testing.T) {
	cs, _ := newTestConfigStore(t)

	if msg := cs.ValidateValue("web_fetch", "nonexistent", "val"); msg == "" {
		t.Error("expected error for unknown field")
	}
}

func TestValidateValue_PasswordAcceptsAnything(t *testing.T) {
	cs, _ := newTestConfigStore(t)

	// Password fields should accept any non-empty string.
	if msg := cs.ValidateValue("web_search", "brave_api_key", "sk-any-value-here"); msg != "" {
		t.Errorf("expected valid password, got %q", msg)
	}
}
