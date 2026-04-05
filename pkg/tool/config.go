package tool

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/store"
	"github.com/xoai/sageclaw/pkg/store/sqlite"
)

// ConfigStore reads and writes tool configuration values.
// Handles two-level resolution: per-agent override -> global -> schema default.
type ConfigStore struct {
	store    store.Store
	registry *Registry
	encKey   []byte

	mu        sync.RWMutex
	cache     map[string]string // key: "tool:field" -> value
	cacheTime time.Time
	cacheTTL  time.Duration
}

// NewConfigStore creates a ConfigStore backed by the settings table.
func NewConfigStore(s store.Store, reg *Registry, encKey []byte) *ConfigStore {
	return &ConfigStore{
		store:    s,
		registry: reg,
		encKey:   encKey,
		cache:    make(map[string]string),
		cacheTTL: 5 * time.Second,
	}
}

// settingKey returns the settings-table key for a tool config field.
func settingKey(toolName, fieldName string) string {
	return "tool_config:" + toolName + ":" + fieldName
}

// isSensitive checks whether a field is a password type.
func (cs *ConfigStore) isSensitive(toolName, fieldName string) bool {
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if !ok {
		return false
	}
	f, ok := schema[fieldName]
	return ok && f.Type == "password"
}

// Get returns the resolved value for a tool config field.
// Resolution order: agent override (from context) -> global setting -> schema default -> "".
func (cs *ConfigStore) Get(ctx context.Context, toolName, fieldName string) string {
	// 1. Per-agent override from context.
	if agentCfg := ToolConfigFromContext(ctx); agentCfg != nil {
		if toolCfg, ok := agentCfg[toolName]; ok {
			if val, ok := toolCfg[fieldName]; ok {
				return fmt.Sprintf("%v", val)
			}
		}
	}

	// 2. Global setting (cached).
	if val, ok := cs.getGlobalCached(ctx, toolName, fieldName); ok && val != "" {
		return val
	}

	// 3. Schema default.
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if ok {
		if f, ok := schema[fieldName]; ok && f.Default != nil {
			return fmt.Sprintf("%v", f.Default)
		}
	}

	return ""
}

// GetAll returns all resolved config values for a tool.
func (cs *ConfigStore) GetAll(ctx context.Context, toolName string) map[string]string {
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(schema))
	for fieldName := range schema {
		result[fieldName] = cs.Get(ctx, toolName, fieldName)
	}
	return result
}

// SetGlobal saves a global config value. Password fields are encrypted.
func (cs *ConfigStore) SetGlobal(ctx context.Context, toolName, fieldName, value string) error {
	key := settingKey(toolName, fieldName)

	if cs.isSensitive(toolName, fieldName) && value != "" {
		encrypted, err := sqlite.Encrypt([]byte(value), cs.encKey)
		if err != nil {
			return fmt.Errorf("encrypting config value: %w", err)
		}
		value = base64.StdEncoding.EncodeToString(encrypted)
	}

	if err := cs.store.SetSetting(ctx, key, value); err != nil {
		return err
	}

	// Invalidate entire cache — prevents stale reads from other fields
	// that were cached before this write.
	cs.mu.Lock()
	cs.cache = make(map[string]string)
	cs.cacheTime = time.Time{}
	cs.mu.Unlock()
	return nil
}

// GetGlobalAll returns all global config values for a tool (for UI display).
// Password fields are masked (show last 4 chars).
func (cs *ConfigStore) GetGlobalAll(ctx context.Context, toolName string) map[string]string {
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(schema))
	for fieldName := range schema {
		val := cs.getGlobalRaw(ctx, toolName, fieldName)
		if val != "" && cs.isSensitive(toolName, fieldName) {
			// Mask sensitive values for display.
			if len(val) > 4 {
				result[fieldName] = strings.Repeat("*", len(val)-4) + val[len(val)-4:]
			} else {
				result[fieldName] = strings.Repeat("*", len(val))
			}
		} else {
			result[fieldName] = val
		}
	}
	return result
}

// MissingField is a config field that is required but has no value.
type MissingField struct {
	Name        string
	Description string
	Link        string
}

// Validate checks if all required fields have values.
func (cs *ConfigStore) Validate(ctx context.Context, toolName string) []MissingField {
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if !ok {
		return nil
	}
	var missing []MissingField
	for fieldName, f := range schema {
		if !f.Required {
			continue
		}
		if cs.Get(ctx, toolName, fieldName) == "" {
			missing = append(missing, MissingField{
				Name:        fieldName,
				Description: f.Description,
				Link:        f.Link,
			})
		}
	}
	return missing
}

// MissingRequiredConfig returns an error ToolResult if required config is missing.
// Returns nil if all required fields are set.
func (cs *ConfigStore) MissingRequiredConfig(ctx context.Context, toolName string) *canonical.ToolResult {
	missing := cs.Validate(ctx, toolName)
	if len(missing) == 0 {
		return nil
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("Tool '%s' is not fully configured.", toolName))
	for _, f := range missing {
		lines = append(lines, fmt.Sprintf("  Missing: %s — %s", f.Name, f.Description))
	}
	lines = append(lines, fmt.Sprintf("Configure at: Settings > Tools > %s", toolName))

	return &canonical.ToolResult{
		Content: strings.Join(lines, "\n"),
		IsError: true,
	}
}

// ValidateValue checks that a value is valid for the given field's type.
// Returns an error string if invalid, empty string if ok.
// Empty values are always accepted (they clear the field).
func (cs *ConfigStore) ValidateValue(toolName, fieldName, value string) string {
	if value == "" {
		return ""
	}
	schema, ok := cs.registry.GetConfigSchema(toolName)
	if !ok {
		return "tool has no config schema"
	}
	field, ok := schema[fieldName]
	if !ok {
		return "unknown field: " + fieldName
	}
	switch field.Type {
	case "select":
		for _, opt := range field.Options {
			if value == opt {
				return ""
			}
		}
		return fmt.Sprintf("invalid value %q for field %s — allowed: %s", value, fieldName, strings.Join(field.Options, ", "))
	case "number":
		for _, ch := range value {
			if (ch < '0' || ch > '9') && ch != '-' && ch != '.' {
				return fmt.Sprintf("invalid number %q for field %s", value, fieldName)
			}
		}
	case "boolean":
		if value != "true" && value != "false" {
			return fmt.Sprintf("invalid boolean %q for field %s — use true or false", value, fieldName)
		}
	}
	return ""
}

// Reader returns a ConfigReader closure backed by this ConfigStore.
func (cs *ConfigStore) Reader() ConfigReader {
	return func(ctx context.Context, toolName, fieldName string) string {
		return cs.Get(ctx, toolName, fieldName)
	}
}

// getGlobalCached returns a global config value from cache or store.
func (cs *ConfigStore) getGlobalCached(ctx context.Context, toolName, fieldName string) (string, bool) {
	cacheKey := toolName + ":" + fieldName

	cs.mu.RLock()
	if time.Since(cs.cacheTime) < cs.cacheTTL {
		val, ok := cs.cache[cacheKey]
		cs.mu.RUnlock()
		return val, ok
	}
	cs.mu.RUnlock()

	// Cache miss or expired — read from store.
	val := cs.getGlobalRaw(ctx, toolName, fieldName)
	if val == "" {
		return "", false
	}

	cs.mu.Lock()
	cs.cache[cacheKey] = val
	cs.cacheTime = time.Now()
	cs.mu.Unlock()

	return val, true
}

// getGlobalRaw reads a global config value from the store, decrypting if sensitive.
func (cs *ConfigStore) getGlobalRaw(ctx context.Context, toolName, fieldName string) string {
	key := settingKey(toolName, fieldName)
	val, err := cs.store.GetSetting(ctx, key)
	if err != nil || val == "" {
		return ""
	}

	if cs.isSensitive(toolName, fieldName) {
		encrypted, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			return "" // corrupted
		}
		plaintext, err := sqlite.Decrypt(encrypted, cs.encKey)
		if err != nil {
			return "" // corrupted
		}
		return string(plaintext)
	}
	return val
}
