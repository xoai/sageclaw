package canonical

import "testing"

func TestJoinSystemParts_Empty(t *testing.T) {
	result := JoinSystemParts(nil)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestJoinSystemParts_Single(t *testing.T) {
	parts := []SystemPart{{Content: "You are a helpful assistant.", Cacheable: true}}
	result := JoinSystemParts(parts)
	if result != "You are a helpful assistant." {
		t.Errorf("got %q", result)
	}
}

func TestJoinSystemParts_Multiple(t *testing.T) {
	parts := []SystemPart{
		{Content: "Base prompt", Cacheable: true},
		{Content: "Injections", Cacheable: false},
		{Content: "Task context", Cacheable: false},
	}
	expected := "Base prompt\n\nInjections\n\nTask context"
	result := JoinSystemParts(parts)
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestJoinSystemParts_EmptyContent(t *testing.T) {
	parts := []SystemPart{
		{Content: "Part1", Cacheable: true},
		{Content: "", Cacheable: false},
		{Content: "Part3", Cacheable: true},
	}
	// Empty parts are still joined (caller is responsible for filtering).
	expected := "Part1\n\n\n\nPart3"
	result := JoinSystemParts(parts)
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestSystemPartOnRequest(t *testing.T) {
	req := Request{
		Model: "test",
		SystemParts: []SystemPart{
			{Content: "Base", Cacheable: true},
			{Content: "Dynamic", Cacheable: false},
		},
		System: "Base\n\nDynamic",
	}
	if len(req.SystemParts) != 2 {
		t.Errorf("expected 2 SystemParts, got %d", len(req.SystemParts))
	}
	if req.System != "Base\n\nDynamic" {
		t.Errorf("System field mismatch: %q", req.System)
	}
}
