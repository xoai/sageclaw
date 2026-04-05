package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

// TUIClient is an HTTP client for the SageClaw RPC server.
// It handles auth cookie management and provides typed methods
// for all RPC/REST endpoints the TUI needs.
type TUIClient struct {
	baseURL   string
	client    *http.Client // For RPC/REST calls (30s timeout).
	sseClient *http.Client // For SSE connections (no timeout).
}

// NewTUIClient creates a client targeting the given RPC server URL.
func NewTUIClient(baseURL string) *TUIClient {
	jar, _ := cookiejar.New(nil)
	return &TUIClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
			Jar:     jar,
		},
		sseClient: &http.Client{
			Timeout: 0, // No timeout — SSE connections are long-lived.
			Jar:     jar,
		},
	}
}

// HTTPClient returns the underlying http.Client (for RPC/REST).
func (c *TUIClient) HTTPClient() *http.Client { return c.client }

// SSEClient returns the HTTP client for SSE (no timeout).
func (c *TUIClient) SSEClient() *http.Client { return c.sseClient }
func (c *TUIClient) BaseURL() string           { return c.baseURL }

// --- Auth ---

// AuthState represents the auth check result.
type AuthState struct {
	State       string `json:"state"` // "ready", "setup", "login"
	TOTPEnabled bool   `json:"totp_enabled,omitempty"`
}

// CheckAuth returns the current auth state.
func (c *TUIClient) CheckAuth() (AuthState, error) {
	var state AuthState
	if err := c.getJSON("/api/auth/check", &state); err != nil {
		return state, err
	}
	return state, nil
}

// Login authenticates with the given password. The cookie jar
// captures the Set-Cookie header automatically.
func (c *TUIClient) Login(password string) error {
	body := map[string]string{"password": password}
	var resp map[string]string
	if err := c.postJSON("/api/auth/login", body, &resp); err != nil {
		return err
	}
	if errMsg, ok := resp["error"]; ok {
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

// Setup creates the initial password.
func (c *TUIClient) Setup(password string) error {
	body := map[string]string{"password": password, "confirm": password}
	var resp map[string]string
	if err := c.postJSON("/api/auth/setup", body, &resp); err != nil {
		return err
	}
	if errMsg, ok := resp["error"]; ok {
		return fmt.Errorf("%s", errMsg)
	}
	return nil
}

// --- RPC ---

// rpcRequest is the JSON-RPC request envelope.
type rpcRequest struct {
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
	ID     int    `json:"id"`
}

// rpcResponse is the JSON-RPC response envelope.
type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// RPC calls a JSON-RPC method on the server.
func (c *TUIClient) RPC(method string, params any) (json.RawMessage, error) {
	req := rpcRequest{Method: method, Params: params, ID: 1}
	var resp rpcResponse
	if err := c.postJSON("/rpc", req, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc %s: %s", method, resp.Error.Message)
	}
	return resp.Result, nil
}

// --- Typed methods ---

// AgentInfo represents a minimal agent from the list API.
type AgentInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Avatar  string `json:"avatar"`
	Model   string `json:"model"`
	Profile string `json:"profile"`
}

// ListAgents fetches available agents.
func (c *TUIClient) ListAgents() ([]AgentInfo, error) {
	var agents []AgentInfo
	if err := c.getJSON("/api/v2/agents", &agents); err != nil {
		return nil, err
	}
	return agents, nil
}

// SessionInfo represents a session summary.
type SessionInfo struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Title      string `json:"title"`
	CreatedAt  string `json:"created_at"`
	TotalCost  string `json:"total_cost"`
	TokensUsed int    `json:"tokens_used"`
}

// ListSessions fetches sessions for an agent.
func (c *TUIClient) ListSessions(agentID string) ([]SessionInfo, error) {
	params := map[string]string{"agent_id": agentID}
	raw, err := c.RPC("sessions.list", params)
	if err != nil {
		return nil, err
	}
	var sessions []SessionInfo
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return nil, fmt.Errorf("parsing sessions: %w", err)
	}
	return sessions, nil
}

// MessageInfo represents a chat message.
type MessageInfo struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// LoadMessages fetches messages for a session.
func (c *TUIClient) LoadMessages(sessionID string, limit int) ([]MessageInfo, error) {
	if limit <= 0 {
		limit = 50
	}
	params := map[string]any{"session_id": sessionID, "limit": limit}
	raw, err := c.RPC("sessions.messages", params)
	if err != nil {
		return nil, err
	}
	var messages []MessageInfo
	if err := json.Unmarshal(raw, &messages); err != nil {
		return nil, fmt.Errorf("parsing messages: %w", err)
	}
	return messages, nil
}

// SendMessage sends a chat message. Uses channel "web" to avoid
// agent channel validation issues.
func (c *TUIClient) SendMessage(agentID, chatID, text string) error {
	params := map[string]string{
		"channel":  "web",
		"agent_id": agentID,
		"chat_id":  chatID,
		"text":     text,
	}
	_, err := c.RPC("chat.send", params)
	return err
}

// RespondConsent sends a consent response.
func (c *TUIClient) RespondConsent(nonce string, granted bool, tier string) error {
	body := map[string]any{
		"nonce":   nonce,
		"granted": granted,
		"tier":    tier,
	}
	var resp map[string]string
	return c.postJSON("/api/consent", body, &resp)
}

// ModelInfo represents a model from the providers API.
type ModelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Tier     string `json:"tier"`
}

// ListModels fetches available models.
func (c *TUIClient) ListModels() ([]ModelInfo, error) {
	var models []ModelInfo
	if err := c.getJSON("/api/providers/models", &models); err != nil {
		return nil, err
	}
	return models, nil
}

// HealthInfo represents the health check response.
type HealthInfo struct {
	Status    string `json:"status"`
	Providers int    `json:"providers"`
	Agents    int    `json:"agents"`
}

// GetHealth checks server health.
func (c *TUIClient) GetHealth() (HealthInfo, error) {
	var health HealthInfo
	if err := c.getJSON("/api/health", &health); err != nil {
		return health, err
	}
	return health, nil
}

// --- HTTP helpers ---

func (c *TUIClient) getJSON(path string, out any) error {
	resp, err := c.client.Get(c.baseURL + path)
	if err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: %d %s", path, resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *TUIClient) postJSON(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.client.Post(c.baseURL+path, "application/json", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode >= 400 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("POST %s: %d %s", path, resp.StatusCode, string(respBody))
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
