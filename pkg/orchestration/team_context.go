package orchestration

import (
	"fmt"
	"strings"
)

// TeamContext generates role-aware TEAM.md content for injection into an agent's system prompt.
func TeamContext(team *Team, agentID string) string {
	if team == nil {
		return ""
	}

	isLead := agentID == team.LeadID
	memberList := strings.Join(team.Members, ", ")

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Team: %s\n\n", team.Name))
	sb.WriteString(fmt.Sprintf("You are part of a team. Team ID: `%s`\n", team.ID))
	sb.WriteString(fmt.Sprintf("Members: %s\n", memberList))
	sb.WriteString(fmt.Sprintf("Lead: %s\n\n", team.LeadID))

	if isLead {
		sb.WriteString("## Your Role: Lead\n\n")
		sb.WriteString("You orchestrate work for this team. Your responsibilities:\n\n")
		sb.WriteString("1. **Break down user requests** into discrete tasks\n")
		sb.WriteString("2. **Create tasks** using `team_create_task` with clear titles and descriptions\n")
		sb.WriteString("3. **Assign tasks** to the right member using `team_assign_task`\n")
		sb.WriteString("4. **Track progress** using `team_list_tasks` — check what's open, claimed, completed\n")
		sb.WriteString("5. **Synthesize results** — when members complete tasks, combine their results into a final response\n\n")
		sb.WriteString("### Task Dependencies\n\n")
		sb.WriteString("When tasks depend on each other, create them with `blocked_by` to express the dependency.\n")
		sb.WriteString("Blocked tasks auto-unblock when their dependencies complete.\n\n")
		sb.WriteString("### Patterns\n\n")
		sb.WriteString("- **Parallel work**: Assign independent tasks to different members simultaneously\n")
		sb.WriteString("- **Sequential work**: Use `blocked_by` for tasks that depend on earlier results\n")
		sb.WriteString("- **Follow-up**: After a member completes, create follow-up tasks if needed\n\n")
		sb.WriteString("### Important\n\n")
		sb.WriteString("- Do NOT use `team_send` or `team_inbox` — coordinate exclusively through tasks\n")
		sb.WriteString("- Members will communicate with each other via the team mailbox if needed\n")
	} else {
		sb.WriteString("## Your Role: Team Member\n\n")
		sb.WriteString(fmt.Sprintf("Your agent ID is `%s`. You receive tasks from the lead (%s).\n\n", agentID, team.LeadID))
		sb.WriteString("Your responsibilities:\n\n")
		sb.WriteString("1. **Check your tasks** using `team_list_tasks` — look for tasks assigned to you\n")
		sb.WriteString("2. **Work on assigned tasks** independently and thoroughly\n")
		sb.WriteString("3. **Complete tasks** using `team_complete_task` with a clear result summary\n")
		sb.WriteString("4. **Communicate** with other members using `team_send` if you need their input\n")
		sb.WriteString("5. **Check inbox** using `team_inbox` for messages from other members\n\n")
		sb.WriteString("### Important\n\n")
		sb.WriteString("- Complete tasks with detailed results — the lead synthesizes your work\n")
		sb.WriteString("- If a task is unclear, check with the lead by completing with questions\n")
		sb.WriteString("- You can claim open tasks using `team_claim_task`\n")
	}

	return sb.String()
}

// LeadExcludedTools returns tool names that the lead should NOT have.
var LeadExcludedTools = []string{
	"team_send",
	"team_inbox",
}
