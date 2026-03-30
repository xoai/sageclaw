package tool

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// convertMode selects output format for the DOM walker.
type convertMode int

const (
	modeMarkdown convertMode = iota
	modeText
)

// converter walks a parsed HTML DOM tree and emits markdown or plain text.
// Ported from GoClaw's web_fetch_convert.go.
type converter struct {
	buf       strings.Builder
	mode      convertMode
	inPre     bool
	listDepth int
	listType  []atom.Atom // stack: atom.Ul / atom.Ol
	listIndex []int       // ordered list counters
	inLink    bool
}

// Elements to skip entirely (element + all descendants).
var skipElements = map[atom.Atom]bool{
	atom.Head:     true,
	atom.Script:   true,
	atom.Style:    true,
	atom.Noscript: true,
	atom.Svg:      true,
	atom.Template: true,
	atom.Iframe:   true,
	atom.Select:   true,
	atom.Option:   true,
	atom.Button:   true,
	atom.Input:    true,
	atom.Form:     true,
	atom.Nav:      true,
	atom.Footer:   true,
	atom.Picture:  true,
	atom.Source:   true,
}

// Additional elements to skip in text mode only.
var skipInTextMode = map[atom.Atom]bool{
	atom.Header: true,
	atom.Aside:  true,
}

// Block elements that need surrounding newlines.
var blockElements = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true,
	atom.Main: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Blockquote: true,
	atom.Pre: true, atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.Table: true, atom.Tr: true, atom.Hr: true, atom.Dl: true,
	atom.Dt: true, atom.Dd: true, atom.Figure: true, atom.Figcaption: true,
	atom.Details: true, atom.Summary: true, atom.Address: true,
}

// htmlToMarkdown converts HTML to markdown using DOM parsing.
func htmlToMarkdown(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return stripHTMLFallback(rawHTML)
	}
	body := findBody(doc)
	c := &converter{mode: modeMarkdown}
	c.walkChildren(body)
	return cleanOutput(c.buf.String())
}

// htmlToText extracts plain text from HTML content using DOM parsing.
func htmlToText(rawHTML string) string {
	doc, err := html.Parse(strings.NewReader(rawHTML))
	if err != nil {
		return stripHTMLFallback(rawHTML)
	}
	body := findBody(doc)
	c := &converter{mode: modeText}
	c.walkChildren(body)
	return cleanOutput(c.buf.String())
}

func (c *converter) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		c.handleText(n)
		return
	case html.ElementNode:
		// handled below
	case html.DocumentNode:
		c.walkChildren(n)
		return
	default:
		return
	}

	// Skip hidden elements to prevent hidden-text prompt injection.
	if isHiddenElement(n) {
		return
	}

	tag := n.DataAtom

	if skipElements[tag] {
		return
	}
	if c.mode == modeText && skipInTextMode[tag] {
		return
	}

	switch tag {
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		c.handleHeading(n)
	case atom.P:
		c.handleParagraph(n)
	case atom.A:
		c.handleLink(n)
	case atom.Img:
		c.handleImage(n)
	case atom.Pre:
		c.handlePre(n)
	case atom.Code:
		c.handleCode(n)
	case atom.Blockquote:
		c.handleBlockquote(n)
	case atom.Strong, atom.B:
		c.handleStrong(n)
	case atom.Em, atom.I:
		c.handleEmphasis(n)
	case atom.Br:
		c.buf.WriteByte('\n')
	case atom.Hr:
		c.ensureNewline()
		if c.mode == modeMarkdown {
			c.buf.WriteString("---\n")
		}
	case atom.Ul, atom.Ol:
		c.handleList(n)
	case atom.Li:
		c.handleListItem(n)
	case atom.Table:
		c.handleTable(n)
	case atom.Dt:
		c.ensureNewline()
		if c.mode == modeMarkdown {
			c.buf.WriteString("**")
		}
		c.walkChildren(n)
		if c.mode == modeMarkdown {
			c.buf.WriteString("**")
		}
		c.buf.WriteByte('\n')
	case atom.Dd:
		if c.mode == modeMarkdown {
			c.buf.WriteString(": ")
		}
		c.walkChildren(n)
		c.buf.WriteByte('\n')
	default:
		if blockElements[tag] {
			c.ensureNewline()
			c.walkChildren(n)
			c.ensureNewline()
		} else {
			c.walkChildren(n)
		}
	}
}

func (c *converter) walkChildren(n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		c.walk(child)
	}
}

func (c *converter) handleText(n *html.Node) {
	text := n.Data
	if c.inPre {
		c.buf.WriteString(text)
		return
	}
	// Collapse whitespace.
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return
	}
	c.buf.WriteString(text)
}

func (c *converter) handleHeading(n *html.Node) {
	c.ensureNewline()
	if c.mode == modeMarkdown {
		level := int(n.DataAtom-atom.H1) + 1
		c.buf.WriteString(strings.Repeat("#", level) + " ")
	}
	c.walkChildren(n)
	c.buf.WriteString("\n\n")
}

func (c *converter) handleParagraph(n *html.Node) {
	c.ensureNewline()
	c.walkChildren(n)
	c.buf.WriteString("\n\n")
}

func (c *converter) handleLink(n *html.Node) {
	href := getAttr(n, "href")
	if c.mode == modeText || href == "" || strings.HasPrefix(href, "#") || strings.HasPrefix(href, "javascript:") {
		c.walkChildren(n)
		return
	}
	c.buf.WriteByte('[')
	prev := c.inLink
	c.inLink = true
	c.walkChildren(n)
	c.inLink = prev
	c.buf.WriteString("](")
	c.buf.WriteString(href)
	c.buf.WriteByte(')')
}

func (c *converter) handleImage(n *html.Node) {
	if c.mode == modeText {
		alt := getAttr(n, "alt")
		if alt != "" {
			c.buf.WriteString("[Image: " + alt + "]")
		}
		return
	}
	alt := getAttr(n, "alt")
	src := getAttr(n, "src")
	if src == "" {
		return
	}
	c.buf.WriteString("![" + alt + "](" + src + ")")
}

func (c *converter) handlePre(n *html.Node) {
	c.ensureNewline()
	if c.mode == modeMarkdown {
		lang := ""
		// Check for <code> child with class="language-xxx".
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.DataAtom == atom.Code {
				cls := getAttr(child, "class")
				for _, part := range strings.Fields(cls) {
					if strings.HasPrefix(part, "language-") {
						lang = strings.TrimPrefix(part, "language-")
					}
				}
			}
		}
		c.buf.WriteString("```" + lang + "\n")
	}
	c.inPre = true
	c.walkChildren(n)
	c.inPre = false
	if c.mode == modeMarkdown {
		c.ensureNewline()
		c.buf.WriteString("```\n")
	}
	c.buf.WriteByte('\n')
}

func (c *converter) handleCode(n *html.Node) {
	if c.inPre {
		c.walkChildren(n)
		return
	}
	if c.mode == modeMarkdown {
		c.buf.WriteByte('`')
	}
	c.walkChildren(n)
	if c.mode == modeMarkdown {
		c.buf.WriteByte('`')
	}
}

func (c *converter) handleBlockquote(n *html.Node) {
	c.ensureNewline()
	if c.mode == modeMarkdown {
		// Capture children output and prefix each line with "> ".
		inner := &converter{mode: c.mode}
		inner.walkChildren(n)
		for _, line := range strings.Split(strings.TrimSpace(inner.buf.String()), "\n") {
			c.buf.WriteString("> " + line + "\n")
		}
		c.buf.WriteByte('\n')
		return
	}
	c.walkChildren(n)
	c.buf.WriteByte('\n')
}

func (c *converter) handleStrong(n *html.Node) {
	if c.mode == modeMarkdown {
		c.buf.WriteString("**")
	}
	c.walkChildren(n)
	if c.mode == modeMarkdown {
		c.buf.WriteString("**")
	}
}

func (c *converter) handleEmphasis(n *html.Node) {
	if c.mode == modeMarkdown {
		c.buf.WriteByte('*')
	}
	c.walkChildren(n)
	if c.mode == modeMarkdown {
		c.buf.WriteByte('*')
	}
}

func (c *converter) handleList(n *html.Node) {
	c.ensureNewline()
	c.listDepth++
	c.listType = append(c.listType, n.DataAtom)
	c.listIndex = append(c.listIndex, 0)
	c.walkChildren(n)
	c.listDepth--
	c.listType = c.listType[:len(c.listType)-1]
	c.listIndex = c.listIndex[:len(c.listIndex)-1]
	if c.listDepth == 0 {
		c.buf.WriteByte('\n')
	}
}

func (c *converter) handleListItem(n *html.Node) {
	indent := strings.Repeat("  ", c.listDepth-1)
	if len(c.listType) > 0 && c.listType[len(c.listType)-1] == atom.Ol {
		c.listIndex[len(c.listIndex)-1]++
		c.buf.WriteString(fmt.Sprintf("%s%d. ", indent, c.listIndex[len(c.listIndex)-1]))
	} else {
		c.buf.WriteString(indent + "- ")
	}
	c.walkChildren(n)
	c.buf.WriteByte('\n')
}

func (c *converter) handleTable(n *html.Node) {
	c.ensureNewline()
	if c.mode == modeText {
		c.walkChildren(n)
		c.buf.WriteByte('\n')
		return
	}

	// Collect rows.
	var rows [][]string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.DataAtom == atom.Tr {
			var cells []string
			for cell := node.FirstChild; cell != nil; cell = cell.NextSibling {
				if cell.DataAtom == atom.Td || cell.DataAtom == atom.Th {
					inner := &converter{mode: modeMarkdown}
					inner.walkChildren(cell)
					cells = append(cells, strings.TrimSpace(inner.buf.String()))
				}
			}
			if len(cells) > 0 {
				rows = append(rows, cells)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)

	if len(rows) == 0 {
		return
	}

	// Find max columns.
	maxCols := 0
	for _, row := range rows {
		if len(row) > maxCols {
			maxCols = len(row)
		}
	}

	// Emit markdown table.
	for i, row := range rows {
		for len(row) < maxCols {
			row = append(row, "")
		}
		c.buf.WriteString("| " + strings.Join(row, " | ") + " |\n")
		if i == 0 {
			sep := make([]string, maxCols)
			for j := range sep {
				sep[j] = "---"
			}
			c.buf.WriteString("| " + strings.Join(sep, " | ") + " |\n")
		}
	}
	c.buf.WriteByte('\n')
}

func (c *converter) ensureNewline() {
	s := c.buf.String()
	if len(s) > 0 && s[len(s)-1] != '\n' {
		c.buf.WriteByte('\n')
	}
}

// --- Helpers ---

func findBody(n *html.Node) *html.Node {
	var search func(*html.Node) *html.Node
	search = func(node *html.Node) *html.Node {
		if node.Type == html.ElementNode && node.DataAtom == atom.Body {
			return node
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if found := search(child); found != nil {
				return found
			}
		}
		return nil
	}
	if found := search(n); found != nil {
		return found
	}
	return n // fallback to document root if no <body>
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// cleanOutput collapses excess blank lines and trims.
func cleanOutput(s string) string {
	// Collapse 3+ newlines to 2.
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}

// stripHTMLFallback is a simple regex-like fallback when DOM parsing fails.
func stripHTMLFallback(s string) string {
	var result strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return strings.TrimSpace(result.String())
}
