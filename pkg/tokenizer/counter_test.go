package tokenizer

import (
	"testing"

	"github.com/xoai/sageclaw/pkg/canonical"
)

func TestCounter_Count(t *testing.T) {
	c, err := Get()
	if err != nil {
		t.Fatalf("failed to get counter: %v", err)
	}

	// "Hello, world!" should be a small number of tokens.
	n := c.Count("Hello, world!")
	if n < 1 || n > 10 {
		t.Errorf("expected 1-10 tokens for 'Hello, world!', got %d", n)
	}

	// Empty string.
	if c.Count("") != 0 {
		t.Errorf("expected 0 tokens for empty string")
	}

	// Longer text should produce more tokens.
	short := c.Count("hello")
	long := c.Count("This is a much longer sentence with many words that should produce significantly more tokens.")
	if long <= short {
		t.Errorf("longer text should have more tokens: short=%d, long=%d", short, long)
	}
}

func TestCounter_CountMessages(t *testing.T) {
	c, err := Get()
	if err != nil {
		t.Fatalf("failed to get counter: %v", err)
	}

	msgs := []canonical.Message{
		{Role: "user", Content: []canonical.Content{{Type: "text", Text: "Hello"}}},
		{Role: "assistant", Content: []canonical.Content{{Type: "text", Text: "Hi there! How can I help?"}}},
	}

	n := c.CountMessages(msgs)
	if n < 10 {
		t.Errorf("expected at least 10 tokens for 2 messages, got %d", n)
	}
}

func TestCounter_Singleton(t *testing.T) {
	c1, _ := Get()
	c2, _ := Get()
	if c1 != c2 {
		t.Error("Get() should return the same singleton")
	}
}

func TestCounter_NilFallback(t *testing.T) {
	var c *Counter
	n := c.Count("hello world")
	if n != 2 { // len("hello world") / 4 = 2
		t.Errorf("nil counter fallback should use len/4, got %d", n)
	}
}
