package telegram

import (
	"strings"
	"testing"
)

func TestEscapeMarkdownV2(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "hello"},
		{"hello.world", `hello\.world`},
		{"price: $5", `price: $5`},   // $ is NOT a MarkdownV2 special char
		{"a_b", `a\_b`},
		{"a*b", `a\*b`},
		{"(test)", `\(test\)`},
		{"#tag", `\#tag`},
		{">quote", `\>quote`},
	}
	for _, tt := range tests {
		got := escapeMarkdownV2(tt.input)
		if got != tt.want {
			t.Errorf("escapeMarkdownV2(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToTelegramMarkdown_Headings(t *testing.T) {
	input := "# Main Heading\n\nSome text"
	result := toTelegramMarkdown(input)

	// Heading should be wrapped in *bold* with escaped content.
	if !strings.Contains(result, "*Main Heading*") {
		t.Errorf("h1 not converted to bold: %s", result)
	}
	// The # should be gone.
	if strings.Contains(result, "# ") {
		t.Errorf("# should be removed: %s", result)
	}
	// "Some text" should be escaped but present.
	if !strings.Contains(result, "Some text") {
		t.Errorf("body text missing: %s", result)
	}
}

func TestToTelegramMarkdown_Bold(t *testing.T) {
	input := "This is **bold text** here"
	result := toTelegramMarkdown(input)

	// **bold** → *bold* in MarkdownV2.
	if !strings.Contains(result, "*bold text*") {
		t.Errorf("bold not converted: %s", result)
	}
	// No double asterisks should remain.
	if strings.Contains(result, "**") {
		t.Errorf("double asterisks should be gone: %s", result)
	}
}

func TestToTelegramMarkdown_Strikethrough(t *testing.T) {
	input := "This is ~~deleted~~ text"
	result := toTelegramMarkdown(input)

	if !strings.Contains(result, "~deleted~") {
		t.Errorf("strikethrough not converted: %s", result)
	}
}

func TestToTelegramMarkdown_Lists(t *testing.T) {
	input := "- item one\n- item two"
	result := toTelegramMarkdown(input)

	if !strings.Contains(result, "• item one") {
		t.Errorf("unordered list not converted: %s", result)
	}
}

func TestToTelegramMarkdown_OrderedList(t *testing.T) {
	input := "1. First\n2. Second"
	result := toTelegramMarkdown(input)

	// Dot should be escaped: 1\.  (single backslash-dot in the output string)
	if !strings.Contains(result, `1\.`) {
		t.Errorf("ordered list dot not escaped: %s", result)
	}
	// Should NOT be double-escaped.
	if strings.Contains(result, `1\\.`) {
		t.Errorf("ordered list dot double-escaped: %s", result)
	}
}

func TestToTelegramMarkdown_HorizontalRule(t *testing.T) {
	input := "Above\n\n---\n\nBelow"
	result := toTelegramMarkdown(input)

	if strings.Contains(result, "---") {
		t.Errorf("horizontal rule not converted: %s", result)
	}
	if !strings.Contains(result, "———") {
		t.Errorf("expected em dash: %s", result)
	}
}

func TestToTelegramMarkdown_Blockquote(t *testing.T) {
	input := "> This is a quote"
	result := toTelegramMarkdown(input)

	// MarkdownV2 blockquote uses > prefix directly.
	if !strings.Contains(result, ">This is a quote") {
		t.Errorf("blockquote not in MarkdownV2 format: %s", result)
	}
}

func TestToTelegramMarkdown_CodeBlockPreserved(t *testing.T) {
	input := "Text\n```python\ndef hello():\n    print('hi')\n```\nAfter"
	result := toTelegramMarkdown(input)

	// Code block content should not be escaped.
	if !strings.Contains(result, "print('hi')") {
		t.Errorf("code block content modified: %s", result)
	}
	// Language identifier should be preserved for MarkdownV2.
	if !strings.Contains(result, "python") {
		t.Errorf("language id should be preserved for MarkdownV2: %s", result)
	}
}

func TestToTelegramMarkdown_InlineCodePreserved(t *testing.T) {
	input := "Use `fmt.Println` for output"
	result := toTelegramMarkdown(input)

	// Inline code should be preserved with backticks, content not escaped.
	if !strings.Contains(result, "`fmt.Println`") {
		t.Errorf("inline code modified: %s", result)
	}
}

func TestToTelegramMarkdown_LinksPreserved(t *testing.T) {
	input := "Check [this link](https://example.com)"
	result := toTelegramMarkdown(input)

	// Link should be preserved with escaped text inside [].
	if !strings.Contains(result, "](https://example.com)") {
		t.Errorf("link URL modified: %s", result)
	}
	if !strings.Contains(result, "[this link]") {
		t.Errorf("link text missing: %s", result)
	}
}

func TestToTelegramMarkdown_SpecialCharsEscaped(t *testing.T) {
	input := "Price is $5.99 and (tax included)"
	result := toTelegramMarkdown(input)

	// Dots and parens should be escaped.
	if !strings.Contains(result, `5\.99`) {
		t.Errorf("dot not escaped: %s", result)
	}
	if !strings.Contains(result, `\(tax included\)`) {
		t.Errorf("parens not escaped: %s", result)
	}
}

func TestToTelegramMarkdown_HTMLStripped(t *testing.T) {
	input := "Hello <b>world</b>"
	result := toTelegramMarkdown(input)

	if strings.Contains(result, "<b>") {
		t.Errorf("HTML tags not stripped: %s", result)
	}
	if !strings.Contains(result, "world") {
		t.Errorf("text content missing: %s", result)
	}
}
