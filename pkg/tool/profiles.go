package tool

import (
	"fmt"
	"strings"
)

// Tool group constants.
const (
	GroupFS            = "fs"
	GroupRuntime       = "runtime"
	GroupWeb           = "web"
	GroupMemory        = "memory"
	GroupKnowledge     = "knowledge"
	GroupOrchestration = "orchestration"
	GroupTeam          = "team"
	GroupCron          = "cron"
	GroupAudit         = "audit"
	GroupMCP           = "mcp"
	GroupOther         = "other"
)

// Profile constants.
const (
	ProfileFull      = "full"
	ProfileCoding    = "coding"
	ProfileMessaging = "messaging"
	ProfileReadonly  = "readonly"
	ProfileMinimal   = "minimal"
)

// profileDefs maps profile name to allowed groups.
// ProfileFull uses nil (handled specially in IsInProfile — all groups allowed).
// ProfileMinimal uses an empty slice (no groups in-profile — every tool requires consent).
var profileDefs = map[string][]string{
	ProfileFull: nil, // nil means all groups allowed — handled in IsInProfile
	ProfileCoding: {
		GroupFS, GroupRuntime, GroupWeb, GroupMemory,
		GroupKnowledge, GroupOrchestration, GroupAudit,
	},
	ProfileMessaging: {
		GroupWeb, GroupMemory, GroupTeam,
	},
	ProfileReadonly: {
		GroupFS, GroupWeb, GroupMemory, GroupAudit,
	},
	ProfileMinimal: {}, // empty slice = no groups in-profile; all tools trigger consent prompts
}

// ProfileGroups returns the set of allowed groups for a profile.
// Returns nil for "full" or unknown profiles (all groups allowed).
// Returns empty map for "minimal" (no groups in-profile).
func ProfileGroups(profile string) map[string]bool {
	groups, ok := profileDefs[profile]
	if !ok || groups == nil {
		// Unknown or "full" profile → all groups allowed.
		return nil
	}
	m := make(map[string]bool, len(groups))
	for _, g := range groups {
		m[g] = true
	}
	return m
}

// ValidProfile returns true if the profile name is recognized.
func ValidProfile(profile string) bool {
	_, ok := profileDefs[profile]
	return ok
}

// AllProfiles returns the list of available profile names.
func AllProfiles() []string {
	return []string{ProfileFull, ProfileCoding, ProfileMessaging, ProfileReadonly, ProfileMinimal}
}

// AlwaysConsentGroups require consent regardless of profile.
// These are security-critical and non-configurable.
var AlwaysConsentGroups = map[string]bool{
	GroupRuntime:       true,
	GroupMCP:           true,
	GroupOrchestration: true,
}

// IsInProfile returns true if the group is allowed by the given profile.
// Empty or "full" profile allows all groups. Unknown profiles are treated as "full".
func IsInProfile(profile, group string) bool {
	if profile == "" || profile == ProfileFull {
		return true
	}
	groups := ProfileGroups(profile)
	if groups == nil {
		return true // unknown profile → full access
	}
	return groups[group]
}

// GroupExplanation returns a human-readable explanation of what a tool group can do.
// Used in consent prompts. For MCP tools, includes the server name.
func GroupExplanation(group, source string) string {
	if strings.HasPrefix(source, "mcp:") {
		server := strings.TrimPrefix(source, "mcp:")
		return fmt.Sprintf("This tool calls external MCP server '%s'. Results come from outside the system.", server)
	}
	switch group {
	case GroupFS:
		return "This tool can read and write files on the server."
	case GroupRuntime:
		return "This tool can execute shell commands on the server."
	case GroupWeb:
		return "This tool can access the internet and make HTTP requests."
	case GroupOrchestration:
		return "This tool can create or delegate to other AI agents."
	case GroupCron:
		return "This tool can schedule recurring tasks."
	case GroupTeam:
		return "This tool can manage team members and their configurations."
	default:
		return "This tool requires permission to proceed."
	}
}

