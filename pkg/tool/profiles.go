package tool

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
var profileDefs = map[string][]string{
	ProfileFull: {}, // empty means all — handled specially in ListForAgent
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
	ProfileMinimal: {}, // no groups — only explicitly allowed tools
}

// ProfileGroups returns the set of allowed groups for a profile.
func ProfileGroups(profile string) map[string]bool {
	groups, ok := profileDefs[profile]
	if !ok {
		// Unknown profile → full access.
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

// GroupRisk maps each group to its default risk level.
var GroupRisk = map[string]string{
	GroupFS:            RiskModerate,
	GroupRuntime:       RiskSensitive,
	GroupWeb:           RiskModerate,
	GroupMemory:        RiskSafe,
	GroupKnowledge:     RiskSafe,
	GroupOrchestration: RiskSensitive,
	GroupTeam:          RiskModerate,
	GroupCron:          RiskModerate,
	GroupAudit:         RiskSafe,
	GroupMCP:           RiskSensitive,
	GroupOther:         RiskModerate,
}
