package agent

import "testing"

func TestLoopPool_Get_CreatesLazily(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1", SystemPrompt: "You are agent1"},
		"agent2": {AgentID: "agent2", SystemPrompt: "You are agent2"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	l := pool.Get("agent1")
	if l == nil {
		t.Fatal("expected loop for agent1")
	}
	if l.config.AgentID != "agent1" {
		t.Errorf("expected AgentID agent1, got %s", l.config.AgentID)
	}
	if l.config.SystemPrompt != "You are agent1" {
		t.Errorf("expected prompt 'You are agent1', got %s", l.config.SystemPrompt)
	}
}

func TestLoopPool_Get_ReusesExisting(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	l1 := pool.Get("agent1")
	l2 := pool.Get("agent1")
	if l1 != l2 {
		t.Error("expected same loop instance on repeated Get")
	}
}

func TestLoopPool_Get_UnknownAgent(t *testing.T) {
	pool := NewLoopPool(map[string]Config{}, nil, nil, nil, nil, nil)

	l := pool.Get("nonexistent")
	if l != nil {
		t.Error("expected nil for unknown agent")
	}
}

func TestLoopPool_UpdateConfig_ReplacesLoop(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1", SystemPrompt: "old"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	l1 := pool.Get("agent1")
	if l1.config.SystemPrompt != "old" {
		t.Fatalf("expected 'old', got %s", l1.config.SystemPrompt)
	}

	pool.UpdateConfig("agent1", Config{AgentID: "agent1", SystemPrompt: "new"})

	l2 := pool.Get("agent1")
	if l2.config.SystemPrompt != "new" {
		t.Errorf("expected 'new' after update, got %s", l2.config.SystemPrompt)
	}
	if l1 == l2 {
		t.Error("expected different loop instance after update")
	}
}

func TestLoopPool_RemoveConfig(t *testing.T) {
	configs := map[string]Config{
		"agent1": {AgentID: "agent1"},
	}
	pool := NewLoopPool(configs, nil, nil, nil, nil, nil)

	l := pool.Get("agent1")
	if l == nil {
		t.Fatal("expected loop before remove")
	}

	pool.RemoveConfig("agent1")

	l = pool.Get("agent1")
	if l != nil {
		t.Error("expected nil after remove")
	}
}
