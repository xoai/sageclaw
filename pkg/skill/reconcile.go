package skill

import (
	"encoding/json"
	"log"

	"github.com/xoai/sageclaw/pkg/tool"
)

// Reconcile diffs old and new skill sets and updates the tool registry.
func Reconcile(registry *tool.Registry, oldSkills, newSkills []Skill) {
	oldMap := make(map[string]Skill)
	for _, s := range oldSkills {
		oldMap[s.Name] = s
	}

	newMap := make(map[string]Skill)
	for _, s := range newSkills {
		newMap[s.Name] = s
	}

	// Remove tools from deleted skills.
	for name, old := range oldMap {
		if _, exists := newMap[name]; !exists {
			for _, bt := range old.BundledTools {
				toolName := name + "_" + bt.Name
				registry.Unregister(toolName)
				log.Printf("skill-reload: unregistered tool %s (skill %s removed)", toolName, name)
			}
		}
	}

	// Add/update tools from new skills.
	for name, newSkill := range newMap {
		old, existed := oldMap[name]

		for _, bt := range newSkill.BundledTools {
			toolName := name + "_" + bt.Name

			// Check if this tool existed before and is unchanged.
			if existed {
				found := false
				for _, obt := range old.BundledTools {
					if obt.Name == bt.Name && obt.ScriptPath == bt.ScriptPath {
						found = true
						break
					}
				}
				if found {
					continue // Unchanged, skip.
				}
			}

			// Register new or changed tool.
			registry.Unregister(toolName) // Remove old if exists.
			registry.Register(toolName, bt.Description, json.RawMessage(bt.Schema), MakeShellToolFunc(bt.ScriptPath))
			log.Printf("skill-reload: registered tool %s", toolName)
		}
	}
}
