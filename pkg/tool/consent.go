package tool

import "sync"

// ConsentStore tracks per-session tool group consent.
// Safe tools auto-consent. Moderate and sensitive require explicit user approval.
type ConsentStore struct {
	mu       sync.RWMutex
	consents map[string]map[string]bool // sessionID → group → consented
}

// NewConsentStore creates a new consent tracker.
func NewConsentStore() *ConsentStore {
	return &ConsentStore{
		consents: make(map[string]map[string]bool),
	}
}

// HasConsent checks if a session has consent for a tool group.
// Safe tools always return true (auto-consent).
func (cs *ConsentStore) HasConsent(sessionID, group string) bool {
	// Safe tools never need consent.
	if GroupRisk[group] == RiskSafe {
		return true
	}

	cs.mu.RLock()
	defer cs.mu.RUnlock()

	groups, ok := cs.consents[sessionID]
	if !ok {
		return false
	}
	return groups[group]
}

// Grant records consent for a tool group in a session.
func (cs *ConsentStore) Grant(sessionID, group string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.consents[sessionID] == nil {
		cs.consents[sessionID] = make(map[string]bool)
	}
	cs.consents[sessionID][group] = true
}

// GrantAll grants consent for all groups in a session (for non-interactive channels).
func (cs *ConsentStore) GrantAll(sessionID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	groups := make(map[string]bool)
	for group := range GroupRisk {
		groups[group] = true
	}
	cs.consents[sessionID] = groups
}

// Deny records a denial for a tool group in a session.
// Denied groups are tracked so the agent knows not to retry.
func (cs *ConsentStore) Deny(sessionID, group string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	if cs.consents[sessionID] == nil {
		cs.consents[sessionID] = make(map[string]bool)
	}
	// Explicitly false = denied (different from missing = not asked yet).
	cs.consents[sessionID][group] = false
}

// IsDenied checks if a group was explicitly denied in this session.
func (cs *ConsentStore) IsDenied(sessionID, group string) bool {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	groups, ok := cs.consents[sessionID]
	if !ok {
		return false
	}
	consented, exists := groups[group]
	return exists && !consented
}

// ClearSession removes all consent records for a session.
func (cs *ConsentStore) ClearSession(sessionID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.consents, sessionID)
}

// RiskExplanation returns a human-readable explanation of the risk level for a group.
func RiskExplanation(group string) string {
	switch GroupRisk[group] {
	case RiskSafe:
		return "This tool reads stored data. No external effects."
	case RiskModerate:
		switch group {
		case GroupFS:
			return "This tool can read and write files on the server."
		case GroupWeb:
			return "This tool can access the internet and make HTTP requests."
		case GroupCron:
			return "This tool can schedule recurring tasks."
		case GroupTeam:
			return "This tool can manage team members and their configurations."
		default:
			return "This tool can read or write data."
		}
	case RiskSensitive:
		switch group {
		case GroupRuntime:
			return "This tool can execute shell commands on the server. It can read, modify, or delete files and interact with system processes."
		case GroupMCP:
			return "This tool calls an external MCP server. Results come from outside the system."
		case GroupOrchestration:
			return "This tool can create or delegate to other AI agents."
		default:
			return "This tool has elevated permissions and can affect system state."
		}
	}
	return "Unknown tool group."
}
