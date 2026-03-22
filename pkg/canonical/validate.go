package canonical

import (
	"errors"
	"fmt"
)

var (
	ErrMissingModel    = errors.New("model is required")
	ErrNoMessages      = errors.New("at least one message is required")
	ErrEmptyContent    = errors.New("message content must not be empty")
	ErrInvalidRole     = errors.New("invalid message role")
	ErrInvalidMaxToken = errors.New("max_tokens must be positive")
)

var validRoles = map[string]bool{
	"user":      true,
	"assistant": true,
	"tool":      true,
}

// Validate checks a Request for structural correctness.
func (r *Request) Validate() error {
	if r.Model == "" {
		return ErrMissingModel
	}
	if len(r.Messages) == 0 {
		return ErrNoMessages
	}
	if r.MaxTokens < 0 {
		return ErrInvalidMaxToken
	}
	for i, msg := range r.Messages {
		if !validRoles[msg.Role] {
			return fmt.Errorf("message %d: %w: %q", i, ErrInvalidRole, msg.Role)
		}
		if len(msg.Content) == 0 {
			return fmt.Errorf("message %d: %w", i, ErrEmptyContent)
		}
	}
	return nil
}
