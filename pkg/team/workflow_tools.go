package team

import (
	"encoding/json"

	"github.com/xoai/sageclaw/pkg/canonical"
)

// Internal workflow tool names — prefixed with underscore to distinguish from public tools.
const (
	ToolWorkflowAnalyze = "_workflow_analyze"
	ToolWorkflowPlan    = "_workflow_plan"
)

// WorkflowToolDefs returns the internal tool definitions injected into a team lead's tool list.
func WorkflowToolDefs() []canonical.ToolDef {
	return []canonical.ToolDef{
		{
			Name:        ToolWorkflowAnalyze,
			Description: "Analyze whether this request should be delegated to team members or handled directly. Call this BEFORE _workflow_plan.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"delegate": {
						"type": "boolean",
						"description": "true if team members should handle this, false if you should handle directly"
					},
					"confidence": {
						"type": "number",
						"minimum": 0,
						"maximum": 1,
						"description": "How confident you are in this decision (0-1)"
					},
					"reason": {
						"type": "string",
						"description": "Brief explanation of your decision"
					}
				},
				"required": ["delegate", "confidence", "reason"]
			}`),
		},
		{
			Name:        ToolWorkflowPlan,
			Description: "Create a task plan for team delegation. Each task is assigned to a team member. Call _workflow_analyze first.",
			InputSchema: json.RawMessage(`{
				"type": "object",
				"properties": {
					"tasks": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"subject": {
									"type": "string",
									"description": "Short task title"
								},
								"assignee": {
									"type": "string",
									"description": "Team member agent ID"
								},
								"description": {
									"type": "string",
									"description": "Detailed task instructions"
								},
								"blocked_by": {
									"type": "array",
									"items": { "type": "string" },
									"description": "References to earlier tasks: $TASK_0, $TASK_1, etc."
								}
							},
							"required": ["subject", "assignee", "description"]
						},
						"minItems": 1
					},
					"announcement": {
						"type": "string",
						"description": "Message to show the user about what you are delegating"
					}
				},
				"required": ["tasks", "announcement"]
			}`),
		},
	}
}

// IsWorkflowTool returns true if the tool name is an internal workflow tool.
func IsWorkflowTool(name string) bool {
	return name == ToolWorkflowAnalyze || name == ToolWorkflowPlan
}
