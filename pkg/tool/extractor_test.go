package tool

import (
	"context"
	"testing"
)

func TestMarkdownExtractor(t *testing.T) {
	ext := &MarkdownExtractor{}
	html := `<html><body><h1>Hello</h1><p>This is a <strong>test</strong> paragraph.</p></body></html>`
	result, err := ext.Extract(context.Background(), html, "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty markdown output")
	}
	// Should contain markdown heading or bold.
	if !containsAny(result, "# Hello", "**test**") {
		t.Errorf("expected markdown formatting, got: %s", result)
	}
}

func TestPlainTextExtractor(t *testing.T) {
	ext := &PlainTextExtractor{}
	html := `<html><body><script>var x=1;</script><p>Hello world</p></body></html>`
	result, err := ext.Extract(context.Background(), html, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty text")
	}
	if containsAny(result, "var x=1", "<script>") {
		t.Errorf("script content should be stripped, got: %s", result)
	}
}

func TestExtractorChain_QualityFallback(t *testing.T) {
	chain := &ExtractorChain{
		extractors: []ContentExtractor{
			&tinyExtractor{}, // Returns very short text — fails quality check.
			&PlainTextExtractor{},
		},
		minChars: 10,
		minWords: 3,
	}
	html := `<p>This is enough text to pass the quality threshold check easily.</p>`
	result, err := chain.Extract(context.Background(), html, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result from fallback")
	}
}

func TestExtractorChain_AllFail(t *testing.T) {
	chain := &ExtractorChain{
		extractors: []ContentExtractor{
			&tinyExtractor{},
		},
		minChars: 1000,
		minWords: 100,
	}
	_, err := chain.Extract(context.Background(), "<p>short</p>", "")
	if err == nil {
		t.Fatal("expected error when all extractors fail quality check")
	}
}

func TestReadabilityExtractor_ProducesMarkdown(t *testing.T) {
	ext := &ReadabilityExtractor{}
	// Readability needs enough content to identify an article.
	html := `<html><head><title>Test</title></head><body>
		<article>
			<h2>Section Title</h2>
			<p>This is a <strong>bold</strong> paragraph with a <a href="https://example.com">link</a>.</p>
			<p>Another paragraph with enough text to pass the readability content threshold.
			This needs to be a reasonable length article for readability to extract it properly.
			Adding more sentences to ensure the algorithm identifies this as the main content.</p>
		</article>
	</body></html>`
	result, err := ext.Extract(context.Background(), html, "https://example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should contain markdown formatting, not plain text.
	if !containsAny(result, "##", "**bold**") {
		t.Errorf("expected markdown headings or bold, got: %s", result)
	}
	if !containsAny(result, "[link](https://example.com)") {
		t.Errorf("expected markdown link, got: %s", result)
	}
}

func TestNewDefaultChain_WithoutDefuddle(t *testing.T) {
	chain := NewDefaultChain("")
	if len(chain.extractors) != 3 {
		t.Fatalf("expected 3 extractors (readability + markdown + plaintext), got %d", len(chain.extractors))
	}
	if chain.extractors[0].Name() != "readability" {
		t.Errorf("first extractor should be readability, got %s", chain.extractors[0].Name())
	}
}

func TestNewDefaultChain_WithDefuddle(t *testing.T) {
	chain := NewDefaultChain("https://defuddle.example.com")
	if len(chain.extractors) != 4 {
		t.Fatalf("expected 4 extractors (defuddle + readability + markdown + plaintext), got %d", len(chain.extractors))
	}
	if chain.extractors[0].Name() != "defuddle" {
		t.Errorf("first extractor should be defuddle, got %s", chain.extractors[0].Name())
	}
	if chain.extractors[1].Name() != "readability" {
		t.Errorf("second extractor should be readability, got %s", chain.extractors[1].Name())
	}
}

// tinyExtractor always returns a very short string (for testing quality fallback).
type tinyExtractor struct{}

func (t *tinyExtractor) Name() string { return "tiny" }
func (t *tinyExtractor) Extract(_ context.Context, _ string, _ string) (string, error) {
	return "hi", nil
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if contains(s, sub) {
			return true
		}
	}
	return false
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
