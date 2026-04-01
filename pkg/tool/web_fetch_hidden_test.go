package tool

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func parseElement(t *testing.T, rawHTML string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		t.Fatalf("failed to parse HTML: %v", err)
	}
	// Find the first element node inside <body>.
	var find func(*html.Node) *html.Node
	find = func(n *html.Node) *html.Node {
		if n.Type == html.ElementNode && n.Data != "html" && n.Data != "head" && n.Data != "body" {
			return n
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if found := find(c); found != nil {
				return found
			}
		}
		return nil
	}
	node := find(doc)
	if node == nil {
		t.Fatal("no element found in parsed HTML")
	}
	return node
}

func TestIsHiddenElement_HTMLHiddenAttr(t *testing.T) {
	node := parseElement(t, `<div hidden>Secret</div>`)
	if !isHiddenElement(node) {
		t.Error("expected element with hidden attribute to be detected as hidden")
	}
}

func TestIsHiddenElement_AriaHidden(t *testing.T) {
	node := parseElement(t, `<span aria-hidden="true">Hidden</span>`)
	if !isHiddenElement(node) {
		t.Error("expected aria-hidden=true to be detected as hidden")
	}
}

func TestIsHiddenElement_AriaHiddenFalse(t *testing.T) {
	node := parseElement(t, `<span aria-hidden="false">Visible</span>`)
	if isHiddenElement(node) {
		t.Error("aria-hidden=false should not be hidden")
	}
}

func TestIsHiddenElement_TailwindHidden(t *testing.T) {
	node := parseElement(t, `<div class="hidden">Tailwind hidden</div>`)
	if !isHiddenElement(node) {
		t.Error("expected Tailwind 'hidden' class to be detected")
	}
}

func TestIsHiddenElement_BootstrapDNone(t *testing.T) {
	node := parseElement(t, `<div class="d-none">Bootstrap hidden</div>`)
	if !isHiddenElement(node) {
		t.Error("expected Bootstrap 'd-none' class to be detected")
	}
}

func TestIsHiddenElement_SrOnly(t *testing.T) {
	node := parseElement(t, `<span class="sr-only">Screen reader only</span>`)
	if !isHiddenElement(node) {
		t.Error("expected 'sr-only' class to be detected")
	}
}

func TestIsHiddenElement_VisuallyHidden(t *testing.T) {
	node := parseElement(t, `<span class="visually-hidden">Bootstrap 5 sr</span>`)
	if !isHiddenElement(node) {
		t.Error("expected 'visually-hidden' class to be detected")
	}
}

func TestIsHiddenElement_DisplayNone(t *testing.T) {
	node := parseElement(t, `<div style="display: none;">Inline hidden</div>`)
	if !isHiddenElement(node) {
		t.Error("expected display:none to be detected")
	}
}

func TestIsHiddenElement_VisibilityHidden(t *testing.T) {
	node := parseElement(t, `<div style="visibility: hidden;">Invisible</div>`)
	if !isHiddenElement(node) {
		t.Error("expected visibility:hidden to be detected")
	}
}

func TestIsHiddenElement_NegativePosition(t *testing.T) {
	node := parseElement(t, `<div style="position:absolute; left:-9999px;">Offscreen</div>`)
	if !isHiddenElement(node) {
		t.Error("expected negative position to be detected as hidden")
	}
}

func TestIsHiddenElement_ZeroFontSize(t *testing.T) {
	node := parseElement(t, `<span style="font-size: 0;">Zero font</span>`)
	if !isHiddenElement(node) {
		t.Error("expected font-size:0 to be detected")
	}
}

func TestIsHiddenElement_ZeroOpacity(t *testing.T) {
	node := parseElement(t, `<div style="opacity: 0;">Transparent</div>`)
	if !isHiddenElement(node) {
		t.Error("expected opacity:0 to be detected")
	}
}

func TestIsHiddenElement_PartialOpacity(t *testing.T) {
	node := parseElement(t, `<div style="opacity: 0.5;">Semi-transparent</div>`)
	if isHiddenElement(node) {
		t.Error("opacity:0.5 should not be hidden")
	}
}

func TestIsHiddenElement_NormalElement(t *testing.T) {
	node := parseElement(t, `<p class="content main-text">Visible paragraph</p>`)
	if isHiddenElement(node) {
		t.Error("normal element should not be detected as hidden")
	}
}

func TestIsHiddenElement_MultipleClasses(t *testing.T) {
	node := parseElement(t, `<div class="some-class d-none other-class">Hidden among classes</div>`)
	if !isHiddenElement(node) {
		t.Error("expected 'd-none' to be detected among multiple classes")
	}
}

func TestIsHiddenElement_CaseInsensitiveClass(t *testing.T) {
	node := parseElement(t, `<div class="HIDDEN">Upper case hidden</div>`)
	if !isHiddenElement(node) {
		t.Error("expected case-insensitive class matching")
	}
}

func TestIsHiddenElement_WordPressScreenReaderText(t *testing.T) {
	node := parseElement(t, `<span class="screen-reader-text">WP sr text</span>`)
	if !isHiddenElement(node) {
		t.Error("expected WordPress 'screen-reader-text' class to be detected")
	}
}

// --- OpenClaw-sourced pattern tests ---

func TestIsHiddenElement_TypeHidden(t *testing.T) {
	node := parseElement(t, `<input type="hidden" name="csrf" value="abc123">`)
	if !isHiddenElement(node) {
		t.Error("expected type=hidden input to be detected")
	}
}

func TestIsHiddenElement_TypeText(t *testing.T) {
	node := parseElement(t, `<input type="text" name="query">`)
	if isHiddenElement(node) {
		t.Error("type=text input should not be hidden")
	}
}

func TestIsHiddenElement_ColorTransparent(t *testing.T) {
	node := parseElement(t, `<span style="color: transparent;">Invisible text</span>`)
	if !isHiddenElement(node) {
		t.Error("expected color:transparent to be detected")
	}
}

func TestIsHiddenElement_BackgroundTransparent_NotHidden(t *testing.T) {
	node := parseElement(t, `<div style="background-color: transparent;">Visible</div>`)
	if isHiddenElement(node) {
		t.Error("background-color:transparent should NOT be detected as hidden")
	}
}

func TestIsHiddenElement_ClipPathInset(t *testing.T) {
	node := parseElement(t, `<span style="clip-path: inset(100%);">Clipped</span>`)
	if !isHiddenElement(node) {
		t.Error("expected clip-path:inset(100%) to be detected")
	}
}

func TestIsHiddenElement_TransformScale0(t *testing.T) {
	node := parseElement(t, `<div style="transform: scale(0);">Scaled to zero</div>`)
	if !isHiddenElement(node) {
		t.Error("expected transform:scale(0) to be detected")
	}
}

func TestIsHiddenElement_TransformTranslateX(t *testing.T) {
	node := parseElement(t, `<div style="transform: translateX(-9999px);">Far away</div>`)
	if !isHiddenElement(node) {
		t.Error("expected translateX(-9999px) to be detected")
	}
}

func TestIsHiddenElement_TextIndentHide(t *testing.T) {
	node := parseElement(t, `<span style="text-indent: -9999px;">Indented away</span>`)
	if !isHiddenElement(node) {
		t.Error("expected text-indent:-9999px to be detected")
	}
}

func TestIsHiddenElement_ZeroSizeOverflowHidden(t *testing.T) {
	node := parseElement(t, `<div style="width: 0; height: 0; overflow: hidden;">Tiny box</div>`)
	if !isHiddenElement(node) {
		t.Error("expected zero-size container with overflow:hidden to be detected")
	}
}

func TestIsHiddenElement_ZeroWidthOnly_NotHidden(t *testing.T) {
	node := parseElement(t, `<div style="width: 0; height: 100px; overflow: hidden;">Half zero</div>`)
	if isHiddenElement(node) {
		t.Error("width:0 alone (without height:0) should not be hidden")
	}
}

func TestIsHiddenElement_FractionalSize_NotHidden(t *testing.T) {
	node := parseElement(t, `<div style="width: 0.5em; height: 0.3em; overflow: hidden;">Small but visible</div>`)
	if isHiddenElement(node) {
		t.Error("fractional width/height (0.5em) should NOT be detected as zero-size hidden")
	}
}

func TestIsHiddenElement_ScreenReaderOnly(t *testing.T) {
	node := parseElement(t, `<span class="screen-reader-only">Generic sr class</span>`)
	if !isHiddenElement(node) {
		t.Error("expected 'screen-reader-only' class to be detected")
	}
}

// Integration: verify hidden elements are stripped from DOM walker output.
func TestHtmlToMarkdown_StripsHiddenElements(t *testing.T) {
	html := `<html><body>
		<p>Visible content</p>
		<div hidden>Hidden by attribute</div>
		<span aria-hidden="true">Hidden by aria</span>
		<div class="d-none">Hidden by class</div>
		<div style="display: none;">Hidden by style</div>
		<p>More visible content</p>
	</body></html>`
	md := htmlToMarkdown(html)

	if !strings.Contains(md, "Visible content") {
		t.Error("expected visible content to be preserved")
	}
	if !strings.Contains(md, "More visible content") {
		t.Error("expected second visible content to be preserved")
	}
	if strings.Contains(md, "Hidden by attribute") {
		t.Error("hidden attribute content should be stripped")
	}
	if strings.Contains(md, "Hidden by aria") {
		t.Error("aria-hidden content should be stripped")
	}
	if strings.Contains(md, "Hidden by class") {
		t.Error("d-none class content should be stripped")
	}
	if strings.Contains(md, "Hidden by style") {
		t.Error("display:none content should be stripped")
	}
}
