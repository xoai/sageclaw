package canonical

import (
	"errors"
	"testing"
)

func TestValidate_ValidRequest(t *testing.T) {
	r := &Request{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}},
		},
		MaxTokens: 1024,
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("expected valid request, got error: %v", err)
	}
}

func TestValidate_MissingModel(t *testing.T) {
	r := &Request{
		Messages: []Message{
			{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}},
		},
	}
	if err := r.Validate(); !errors.Is(err, ErrMissingModel) {
		t.Fatalf("expected ErrMissingModel, got: %v", err)
	}
}

func TestValidate_NoMessages(t *testing.T) {
	r := &Request{Model: "test"}
	if err := r.Validate(); !errors.Is(err, ErrNoMessages) {
		t.Fatalf("expected ErrNoMessages, got: %v", err)
	}
}

func TestValidate_EmptyContent(t *testing.T) {
	r := &Request{
		Model:    "test",
		Messages: []Message{{Role: "user", Content: nil}},
	}
	if err := r.Validate(); !errors.Is(err, ErrEmptyContent) {
		t.Fatalf("expected ErrEmptyContent, got: %v", err)
	}
}

func TestValidate_InvalidRole(t *testing.T) {
	r := &Request{
		Model: "test",
		Messages: []Message{
			{Role: "invalid", Content: []Content{{Type: "text", Text: "hello"}}},
		},
	}
	if err := r.Validate(); !errors.Is(err, ErrInvalidRole) {
		t.Fatalf("expected ErrInvalidRole, got: %v", err)
	}
}

func TestValidate_NegativeMaxTokens(t *testing.T) {
	r := &Request{
		Model: "test",
		Messages: []Message{
			{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}},
		},
		MaxTokens: -1,
	}
	if err := r.Validate(); !errors.Is(err, ErrInvalidMaxToken) {
		t.Fatalf("expected ErrInvalidMaxToken, got: %v", err)
	}
}

func TestValidate_ZeroMaxTokens_OK(t *testing.T) {
	r := &Request{
		Model: "test",
		Messages: []Message{
			{Role: "user", Content: []Content{{Type: "text", Text: "hello"}}},
		},
		MaxTokens: 0,
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("zero max_tokens should be valid (use default), got: %v", err)
	}
}

func TestValidate_AllRoles(t *testing.T) {
	for _, role := range []string{"user", "assistant", "tool"} {
		r := &Request{
			Model: "test",
			Messages: []Message{
				{Role: role, Content: []Content{{Type: "text", Text: "hello"}}},
			},
		}
		if err := r.Validate(); err != nil {
			t.Fatalf("role %q should be valid, got: %v", role, err)
		}
	}
}
