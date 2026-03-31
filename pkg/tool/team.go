package tool

import "log"

// TeamOps abstracts team operations (breaks import cycle with orchestration).
// Retained for interface compatibility; new code should use team_tasks tool directly.
type TeamOps interface{}

// RegisterTeamForRole is deprecated — use RegisterTeamTasks instead.
// Legacy individual team tools (team_create_task, team_send, etc.) have been
// consolidated into the unified team_tasks tool.
func RegisterTeamForRole(reg *Registry, ops TeamOps, isLead bool) {
	log.Println("[DEPRECATED] RegisterTeamForRole: use RegisterTeamTasks instead")
}

// RegisterTeam is deprecated — use RegisterTeamTasks instead.
func RegisterTeam(reg *Registry, ops TeamOps) {
	log.Println("[DEPRECATED] RegisterTeam: use RegisterTeamTasks instead")
}
