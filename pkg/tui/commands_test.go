package tui

import (
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantArgs string
		wantOK   bool
	}{
		{"/help", "help", "", true},
		{"/agent", "agent", "", true},
		{"/think high", "think", "high", true},
		{"/attach file.png", "attach", "file.png", true},
		{"hello world", "", "", false},
		{"", "", "", false},
		{"/", "", "", false}, // bare slash is not a command
		{"/HELP", "help", "", true}, // case insensitive
	}
	for _, tt := range tests {
		name, args, ok := ParseCommand(tt.input)
		if ok != tt.wantOK {
			t.Errorf("ParseCommand(%q): ok=%v, want %v", tt.input, ok, tt.wantOK)
			continue
		}
		if name != tt.wantName {
			t.Errorf("ParseCommand(%q): name=%q, want %q", tt.input, name, tt.wantName)
		}
		if args != tt.wantArgs {
			t.Errorf("ParseCommand(%q): args=%q, want %q", tt.input, args, tt.wantArgs)
		}
	}
}

func TestFindCommand(t *testing.T) {
	cmd := FindCommand("help")
	if cmd == nil {
		t.Fatal("expected to find /help command")
	}
	if cmd.Name != "help" {
		t.Errorf("expected name 'help', got %q", cmd.Name)
	}

	cmd = FindCommand("nonexistent")
	if cmd != nil {
		t.Error("expected nil for unknown command")
	}
}

func TestMatchCommands(t *testing.T) {
	matches := MatchCommands("he")
	if len(matches) != 1 || matches[0].Name != "help" {
		t.Errorf("expected [help], got %v", matches)
	}

	matches = MatchCommands("s")
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.Name
	}
	// Should match: session, status, settings
	if len(matches) != 3 {
		t.Errorf("expected 3 matches for 's', got %d: %v", len(matches), names)
	}

	matches = MatchCommands("xyz")
	if len(matches) != 0 {
		t.Error("expected no matches for 'xyz'")
	}
}

func TestAllCommands(t *testing.T) {
	cmds := AllCommands()
	if len(cmds) < 10 {
		t.Errorf("expected at least 10 commands, got %d", len(cmds))
	}

	// Verify essential commands exist.
	names := map[string]bool{}
	for _, cmd := range cmds {
		names[cmd.Name] = true
	}
	for _, required := range []string{"help", "agent", "session", "model", "new", "status", "settings", "think", "exit"} {
		if !names[required] {
			t.Errorf("missing required command: /%s", required)
		}
	}
}
