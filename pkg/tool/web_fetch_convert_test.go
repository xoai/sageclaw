package tool

import (
	"strings"
	"testing"
)

func TestHtmlToMarkdown_Headings(t *testing.T) {
	html := `<html><body><h1>Title</h1><h2>Subtitle</h2><p>Content here.</p></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "# Title") {
		t.Errorf("expected '# Title', got: %s", md)
	}
	if !strings.Contains(md, "## Subtitle") {
		t.Errorf("expected '## Subtitle', got: %s", md)
	}
	if !strings.Contains(md, "Content here.") {
		t.Error("expected paragraph text")
	}
}

func TestHtmlToMarkdown_Links(t *testing.T) {
	html := `<html><body><p>Visit <a href="https://example.com">Example</a> for info.</p></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "[Example](https://example.com)") {
		t.Errorf("expected markdown link, got: %s", md)
	}
}

func TestHtmlToMarkdown_CodeBlock(t *testing.T) {
	html := `<html><body><pre><code class="language-go">func main() {}</code></pre></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "```go") {
		t.Errorf("expected fenced code block with language, got: %s", md)
	}
	if !strings.Contains(md, "func main()") {
		t.Error("expected code content")
	}
}

func TestHtmlToMarkdown_Lists(t *testing.T) {
	html := `<html><body>
		<ul><li>Apple</li><li>Banana</li></ul>
		<ol><li>First</li><li>Second</li></ol>
	</body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "- Apple") {
		t.Errorf("expected unordered list, got: %s", md)
	}
	if !strings.Contains(md, "1. First") {
		t.Errorf("expected ordered list, got: %s", md)
	}
}

func TestHtmlToMarkdown_Table(t *testing.T) {
	html := `<html><body><table>
		<tr><th>Name</th><th>Age</th></tr>
		<tr><td>Alice</td><td>30</td></tr>
	</table></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "| Name | Age |") {
		t.Errorf("expected table header, got: %s", md)
	}
	if !strings.Contains(md, "| --- | --- |") {
		t.Errorf("expected table separator, got: %s", md)
	}
	if !strings.Contains(md, "| Alice | 30 |") {
		t.Errorf("expected table row, got: %s", md)
	}
}

func TestHtmlToMarkdown_Blockquote(t *testing.T) {
	html := `<html><body><blockquote>Important quote here.</blockquote></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "> Important quote here.") {
		t.Errorf("expected blockquote, got: %s", md)
	}
}

func TestHtmlToMarkdown_Bold_Italic(t *testing.T) {
	html := `<html><body><p><strong>Bold</strong> and <em>italic</em> text.</p></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "**Bold**") {
		t.Errorf("expected bold, got: %s", md)
	}
	if !strings.Contains(md, "*italic*") {
		t.Errorf("expected italic, got: %s", md)
	}
}

func TestHtmlToMarkdown_SkipsScriptStyleNav(t *testing.T) {
	html := `<html><body>
		<script>alert('xss');</script>
		<style>.foo { color: red; }</style>
		<nav><a href="/">Home</a><a href="/about">About</a></nav>
		<p>Main content here.</p>
		<footer>Copyright 2024</footer>
	</body></html>`
	md := htmlToMarkdown(html)

	if strings.Contains(md, "alert") {
		t.Error("script content should be skipped")
	}
	if strings.Contains(md, "color: red") {
		t.Error("style content should be skipped")
	}
	if strings.Contains(md, "Home") {
		t.Error("nav content should be skipped")
	}
	if strings.Contains(md, "Copyright") {
		t.Error("footer content should be skipped")
	}
	if !strings.Contains(md, "Main content here.") {
		t.Error("expected main content to be preserved")
	}
}

func TestHtmlToText_BasicExtraction(t *testing.T) {
	html := `<html><body>
		<h1>Title</h1>
		<p>Paragraph with <strong>bold</strong> text.</p>
	</body></html>`
	text := htmlToText(html)

	if !strings.Contains(text, "Title") {
		t.Error("expected heading text")
	}
	if !strings.Contains(text, "Paragraph with") {
		t.Error("expected paragraph text")
	}
	// Text mode should NOT include markdown formatting.
	if strings.Contains(text, "# ") {
		t.Error("text mode should not include markdown heading markers")
	}
	if strings.Contains(text, "**") {
		t.Error("text mode should not include bold markers")
	}
}

func TestHtmlToText_SkipsHeaderAside(t *testing.T) {
	html := `<html><body>
		<header><h1>Site Name</h1></header>
		<aside>Sidebar content</aside>
		<article><p>Article content here.</p></article>
	</body></html>`
	text := htmlToText(html)

	if strings.Contains(text, "Site Name") {
		t.Error("header content should be skipped in text mode")
	}
	if strings.Contains(text, "Sidebar") {
		t.Error("aside content should be skipped in text mode")
	}
	if !strings.Contains(text, "Article content here.") {
		t.Error("expected article content")
	}
}

func TestHtmlToMarkdown_Image(t *testing.T) {
	html := `<html><body><img src="photo.jpg" alt="A photo"></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "![A photo](photo.jpg)") {
		t.Errorf("expected markdown image, got: %s", md)
	}
}

func TestHtmlToText_ImageAlt(t *testing.T) {
	html := `<html><body><img src="photo.jpg" alt="A photo"></body></html>`
	text := htmlToText(html)

	if !strings.Contains(text, "[Image: A photo]") {
		t.Errorf("expected image alt text, got: %s", text)
	}
}

func TestHtmlToMarkdown_CollapseWhitespace(t *testing.T) {
	html := `<html><body><p>  lots   of    spaces  </p></body></html>`
	md := htmlToMarkdown(html)

	if strings.Contains(md, "  ") {
		t.Errorf("expected whitespace collapsed, got: %q", md)
	}
}

func TestHtmlToMarkdown_FallbackOnParseError(t *testing.T) {
	// html.Parse is very lenient, so this tests the normal path.
	// An empty string should still produce empty output.
	md := htmlToMarkdown("")
	if md != "" {
		t.Errorf("expected empty output for empty input, got: %q", md)
	}
}

func TestHtmlToMarkdown_NestedList(t *testing.T) {
	html := `<html><body>
		<ul>
			<li>Parent
				<ul><li>Child</li></ul>
			</li>
		</ul>
	</body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "- Parent") {
		t.Errorf("expected parent list item, got: %s", md)
	}
	if !strings.Contains(md, "  - Child") {
		t.Errorf("expected indented child list item, got: %s", md)
	}
}

func TestHtmlToMarkdown_Hr(t *testing.T) {
	html := `<html><body><p>Above</p><hr><p>Below</p></body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "---") {
		t.Errorf("expected horizontal rule, got: %s", md)
	}
}

func TestMarkdownToText_StripsFormatting(t *testing.T) {
	md := "## Heading\n\n**bold** and *italic* text.\n\n[link](https://example.com)\n\n`code`\n\n```go\nfmt.Println()\n```\n\n- item 1\n- item 2\n\n1. first\n2. second\n\n![alt](image.png)"
	text := markdownToText(md)

	// Should strip markdown syntax.
	if strings.Contains(text, "##") {
		t.Error("heading markers should be stripped")
	}
	if strings.Contains(text, "**") {
		t.Error("bold markers should be stripped")
	}
	if strings.Contains(text, "[link]") {
		t.Error("link syntax should be stripped")
	}
	if strings.Contains(text, "```") {
		t.Error("code fence should be stripped")
	}
	// Should preserve text content.
	if !strings.Contains(text, "bold") {
		t.Error("bold text content should be preserved")
	}
	if !strings.Contains(text, "link") {
		t.Error("link text should be preserved")
	}
	if !strings.Contains(text, "item 1") {
		t.Error("list item text should be preserved")
	}
}

func TestMarkdownToText_EmptyInput(t *testing.T) {
	text := markdownToText("")
	if text != "" {
		t.Errorf("expected empty output, got: %s", text)
	}
}
