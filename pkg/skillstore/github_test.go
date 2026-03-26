package skillstore

import "testing"

func TestParseSource(t *testing.T) {
	tests := []struct {
		input     string
		owner     string
		repo      string
		skill     string
		wantError bool
	}{
		{"vercel-labs/agent-skills@react-best-practices", "vercel-labs", "agent-skills", "react-best-practices", false},
		{"owner/repo", "owner", "repo", "repo", false},
		{"https://github.com/owner/repo.git", "owner", "repo", "repo", false},
		{"https://github.com/owner/repo@skill", "owner", "repo", "skill", false},
		{"invalid", "", "", "", true},
		{"", "", "", "", true},
		{"/slash", "", "", "", true},
	}

	for _, tt := range tests {
		owner, repo, skill, err := ParseSource(tt.input)
		if tt.wantError {
			if err == nil {
				t.Errorf("ParseSource(%q): expected error", tt.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSource(%q): unexpected error: %v", tt.input, err)
			continue
		}
		if owner != tt.owner {
			t.Errorf("ParseSource(%q): owner = %q, want %q", tt.input, owner, tt.owner)
		}
		if repo != tt.repo {
			t.Errorf("ParseSource(%q): repo = %q, want %q", tt.input, repo, tt.repo)
		}
		if skill != tt.skill {
			t.Errorf("ParseSource(%q): skill = %q, want %q", tt.input, skill, tt.skill)
		}
	}
}
