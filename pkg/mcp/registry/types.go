package registry

// CuratedIndex is the top-level structure of the embedded MCP index.
type CuratedIndex struct {
	Version    int              `json:"version"`
	UpdatedAt  string           `json:"updated_at"`
	Categories []Category       `json:"categories"`
	Servers    []CuratedServer  `json:"servers"`
}

// Category represents an MCP server category.
type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Icon        string `json:"icon"`
	Description string `json:"description"`
}

// CuratedServer is a single MCP server entry in the curated index.
type CuratedServer struct {
	ID                 string                 `json:"id"`
	Name               string                 `json:"name"`
	Category           string                 `json:"category"`
	Description        string                 `json:"description"`
	Connection         ConnectionConfig       `json:"connection"`
	FallbackConnection *ConnectionConfig      `json:"fallback_connection,omitempty"`
	ConfigSchema       map[string]ConfigField `json:"config_schema,omitempty"`
	GitHub             string                 `json:"github,omitempty"`
	Stars              int                    `json:"stars,omitempty"`
	Tags               []string               `json:"tags,omitempty"`
	Tools              []ToolEntry            `json:"tools,omitempty"`
	InstallCount       int                    `json:"install_count,omitempty"`
	IsFeatured         bool                   `json:"is_featured,omitempty"`
	Author             string                 `json:"author,omitempty"`
}

// ToolEntry is a lightweight tool name+description from the curated index.
type ToolEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ConnectionConfig describes how to connect to an MCP server.
type ConnectionConfig struct {
	Type       string            `json:"type"`                          // "stdio" | "http" | "sse"
	Command    string            `json:"command,omitempty"`             // stdio
	Args       []string          `json:"args,omitempty"`                // stdio
	URL        string            `json:"url,omitempty"`                 // http/sse
	Headers    map[string]string `json:"headers,omitempty"`             // http/sse
	TimeoutSec int               `json:"timeout_sec,omitempty"`         // override default
}

// ConfigField describes a configuration field for an MCP server.
type ConfigField struct {
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Description string `json:"description,omitempty"`
}
