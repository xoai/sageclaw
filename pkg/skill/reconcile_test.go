package skill

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/tool"
)

func TestReconcile_AddSkill(t *testing.T) {
	reg := tool.NewRegistry()

	oldSkills := []Skill{}
	newSkills := []Skill{{
		Name: "test",
		BundledTools: []BundledTool{{
			Name:        "greet",
			Description: "Greet someone",
			Schema:      json.RawMessage(`{}`),
			ScriptPath:  "/tmp/greet.sh",
		}},
	}}

	Reconcile(reg, oldSkills, newSkills)

	_, _, ok := reg.Get("test_greet")
	if !ok {
		t.Fatal("expected test_greet to be registered")
	}
}

func TestReconcile_RemoveSkill(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Register("old_tool", "Old tool", json.RawMessage(`{}`),
		func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
			return nil, nil
		})

	oldSkills := []Skill{{
		Name: "old",
		BundledTools: []BundledTool{{Name: "tool", ScriptPath: "/tmp/old.sh"}},
	}}
	newSkills := []Skill{} // Skill removed.

	Reconcile(reg, oldSkills, newSkills)

	_, _, ok := reg.Get("old_tool")
	if ok {
		t.Fatal("expected old_tool to be unregistered")
	}
}

func TestReconcile_UpdateSkill(t *testing.T) {
	reg := tool.NewRegistry()

	oldSkills := []Skill{{
		Name: "myskill",
		BundledTools: []BundledTool{{
			Name: "mytool", ScriptPath: "/tmp/v1.sh",
		}},
	}}

	newSkills := []Skill{{
		Name: "myskill",
		BundledTools: []BundledTool{{
			Name: "mytool", ScriptPath: "/tmp/v2.sh", // Changed script.
		}},
	}}

	Reconcile(reg, oldSkills, newSkills)

	_, _, ok := reg.Get("myskill_mytool")
	if !ok {
		t.Fatal("expected myskill_mytool to be registered with new script")
	}
}
