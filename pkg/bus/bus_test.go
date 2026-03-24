package bus

import "testing"

func TestSanitizeThreadID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"99", "99"},
		{"1234567890", "1234567890"},
		{"1234567890.123456", "1234567890.123456"},
		{"", ""},
		{"has:colon", "has_colon"},
		{"a:b:c", "a_b_c"},
	}
	for _, tt := range tests {
		got := SanitizeThreadID(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeThreadID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
