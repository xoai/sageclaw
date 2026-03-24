package agentcfg

import "testing"

func TestServesChannel_EmptyServeAll(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{
		"agent1": {ID: "agent1", Channels: ChannelsConfig{}},
	})

	if !p.ServesChannel("agent1", "telegram") {
		t.Error("empty serve list should serve all channels")
	}
	if !p.ServesChannel("agent1", "zalo_bot") {
		t.Error("empty serve list should serve all channels")
	}
}

func TestServesChannel_Filtered(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{
		"tg_only": {
			ID: "tg_only",
			Channels: ChannelsConfig{
				Serve: []string{"telegram"},
			},
		},
	})

	if !p.ServesChannel("tg_only", "telegram") {
		t.Error("should serve telegram")
	}
	if p.ServesChannel("tg_only", "zalo_bot") {
		t.Error("should NOT serve zalo_bot")
	}
	if p.ServesChannel("tg_only", "discord") {
		t.Error("should NOT serve discord")
	}
}

func TestServesChannel_UnknownAgent(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{})

	if !p.ServesChannel("nonexistent", "telegram") {
		t.Error("unknown agent should return true (permissive default)")
	}
}

func TestUpdate(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{})

	if p.Get("new_agent") != nil {
		t.Error("should be nil before update")
	}

	cfg := &AgentConfig{ID: "new_agent", Identity: Identity{Name: "New"}}
	p.Update("new_agent", cfg)

	got := p.Get("new_agent")
	if got == nil || got.Identity.Name != "New" {
		t.Errorf("expected Name=New, got %+v", got)
	}
}

func TestRemove(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{
		"agent1": {ID: "agent1"},
	})

	if p.Get("agent1") == nil {
		t.Error("should exist before remove")
	}

	p.Remove("agent1")

	if p.Get("agent1") != nil {
		t.Error("should be nil after remove")
	}
}

func TestList(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{
		"a": {ID: "a"},
		"b": {ID: "b"},
	})

	list := p.List()
	if len(list) != 2 {
		t.Errorf("expected 2 agents, got %d", len(list))
	}

	// Verify List returns a copy (modifying it doesn't affect provider).
	delete(list, "a")
	if p.Get("a") == nil {
		t.Error("deleting from List result should not affect provider")
	}
}

func TestServesChannel_MultipleChannels(t *testing.T) {
	p := NewMapProvider(map[string]*AgentConfig{
		"multi": {
			ID: "multi",
			Channels: ChannelsConfig{
				Serve: []string{"telegram", "zalo_bot", "web"},
			},
		},
	})

	for _, ch := range []string{"telegram", "zalo_bot", "web"} {
		if !p.ServesChannel("multi", ch) {
			t.Errorf("should serve %s", ch)
		}
	}
	if p.ServesChannel("multi", "discord") {
		t.Error("should NOT serve discord")
	}
}
