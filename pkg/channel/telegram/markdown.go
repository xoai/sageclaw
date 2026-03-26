package telegram

import (
	"regexp"
	"strings"
)

// MarkdownV2 special characters that must be escaped outside of entities.
// Reference: https://core.telegram.org/bots/api#markdownv2-style
const mdv2SpecialChars = `_*[]()~` + "`" + `>#+-=|{}.!`

// Regex patterns for markdown conversion.
var (
	// Headings: # Heading → *Heading* (bold)
	reHeading = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

	// Bold: **text** → *text*
	reBold = regexp.MustCompile(`\*\*(.+?)\*\*`)

	// Italic: single *text* (standard markdown italic, not bold)
	// Note: handled after bold conversion to avoid conflicts.

	// Strikethrough: ~~text~~ → ~text~
	reStrikethrough = regexp.MustCompile(`~~(.+?)~~`)

	// Horizontal rules: --- or *** or ___ → ———
	reHorizontalRule = regexp.MustCompile(`(?m)^[-*_]{3,}\s*$`)

	// Images: ![alt](url) → remove (Telegram doesn't support inline images in text)
	reImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

	// Links: [text](url) — preserved but needs escaping inside.
	reLink = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

	// Unordered list markers: - item or * item → • item
	reUnorderedList = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)

	// Ordered list: 1. item → 1\. item (escape the dot)
	reOrderedList = regexp.MustCompile(`(?m)^(\s*)(\d+)\.\s+`)

	// Blockquote: > text → >text (MarkdownV2 native blockquote)
	reBlockquote = regexp.MustCompile(`(?m)^>\s?(.*)$`)

	// Inline code: `code` — preserved, content inside must not be escaped.
	reInlineCode = regexp.MustCompile("`([^`]+)`")

	// Code blocks: ```...``` — preserved, content inside must not be escaped.

	// HTML tags that LLMs sometimes emit.
	reHTMLTags = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
)

// toTelegramMarkdown converts standard markdown to Telegram MarkdownV2 format.
//
// MarkdownV2 supports: *bold*, _italic_, __underline__, ~strikethrough~,
// ||spoiler||, `code`, ```pre```, [link](url), >blockquote.
//
// Special characters must be escaped with backslash outside of entities.
func toTelegramMarkdown(text string) string {
	// Step 1: Split on code blocks to protect their content.
	parts := strings.Split(text, "```")
	for i := range parts {
		if i%2 == 0 {
			// Non-code content — apply full conversion.
			parts[i] = convertMarkdownV2(parts[i])
		} else {
			// Code block content — only escape isn't needed inside ```...```
			// in MarkdownV2. Keep language identifier for syntax highlighting.
			// No escaping needed inside code blocks.
		}
	}

	return strings.Join(parts, "```")
}

func convertMarkdownV2(text string) string {
	// Extract and protect inline code spans before escaping.
	// Replace with placeholders, restore after escaping.
	type codeSpan struct {
		placeholder string
		original    string
	}
	var codeSpans []codeSpan
	spanIdx := 0
	text = reInlineCode.ReplaceAllStringFunc(text, func(m string) string {
		inner := reInlineCode.FindStringSubmatch(m)
		if len(inner) < 2 {
			return m
		}
		ph := "\x00CODE" + string(rune('A'+spanIdx)) + "\x00"
		codeSpans = append(codeSpans, codeSpan{ph, "`" + inner[1] + "`"})
		spanIdx++
		return ph
	})

	// Extract and protect links before escaping.
	type linkSpan struct {
		placeholder string
		original    string
	}
	var linkSpans []linkSpan
	linkIdx := 0
	text = reLink.ReplaceAllStringFunc(text, func(m string) string {
		inner := reLink.FindStringSubmatch(m)
		if len(inner) < 3 {
			return m
		}
		ph := "\x00LINK" + string(rune('A'+linkIdx)) + "\x00"
		// In MarkdownV2 links: [text](url) — text inside [] needs escaping,
		// but url inside () does not need escaping (except for `)` and `\`).
		escapedText := escapeMarkdownV2(inner[1])
		linkSpans = append(linkSpans, linkSpan{ph, "[" + escapedText + "](" + inner[2] + ")"})
		linkIdx++
		return ph
	})

	// Convert markdown structures BEFORE escaping special chars.

	// Headings → bold (extract content, will be wrapped in * after escaping).
	type heading struct {
		placeholder string
		content     string
	}
	var headings []heading
	headIdx := 0
	text = reHeading.ReplaceAllStringFunc(text, func(m string) string {
		inner := reHeading.FindStringSubmatch(m)
		if len(inner) < 2 {
			return m
		}
		ph := "\x00HEAD" + string(rune('A'+headIdx)) + "\x00"
		headings = append(headings, heading{ph, inner[1]})
		headIdx++
		return ph
	})

	// Bold **text** → extract, will wrap in * after escaping.
	type boldSpan struct {
		placeholder string
		content     string
	}
	var boldSpans []boldSpan
	boldIdx := 0
	text = reBold.ReplaceAllStringFunc(text, func(m string) string {
		inner := reBold.FindStringSubmatch(m)
		if len(inner) < 2 {
			return m
		}
		ph := "\x00BOLD" + string(rune('A'+boldIdx)) + "\x00"
		boldSpans = append(boldSpans, boldSpan{ph, inner[1]})
		boldIdx++
		return ph
	})

	// Strikethrough ~~text~~ → extract, will wrap in ~ after escaping.
	type strikeSpan struct {
		placeholder string
		content     string
	}
	var strikeSpans []strikeSpan
	strikeIdx := 0
	text = reStrikethrough.ReplaceAllStringFunc(text, func(m string) string {
		inner := reStrikethrough.FindStringSubmatch(m)
		if len(inner) < 2 {
			return m
		}
		ph := "\x00STRK" + string(rune('A'+strikeIdx)) + "\x00"
		strikeSpans = append(strikeSpans, strikeSpan{ph, inner[1]})
		strikeIdx++
		return ph
	})

	// Images → plain link text.
	text = reImage.ReplaceAllString(text, "$1: $2")

	// Horizontal rules → em dash line.
	text = reHorizontalRule.ReplaceAllString(text, "———")

	// Unordered lists → bullet.
	text = reUnorderedList.ReplaceAllString(text, "${1}• ")

	// Ordered lists — use placeholder for escaped dot to avoid double-escaping.
	text = reOrderedList.ReplaceAllString(text, "${1}${2}\x01EDOT\x01 ")

	// Blockquotes → MarkdownV2 native >
	text = reBlockquote.ReplaceAllString(text, ">$1")

	// Strip HTML tags.
	text = reHTMLTags.ReplaceAllString(text, "")

	// Now escape all MarkdownV2 special characters in plain text.
	text = escapeMarkdownV2(text)

	// Restore ordered list escaped dots.
	text = strings.ReplaceAll(text, "\x01EDOT\x01", "\\.")

	// Restore formatted spans with their MarkdownV2 syntax.
	for _, h := range headings {
		escaped := escapeMarkdownV2(h.content)
		text = strings.Replace(text, escapeMarkdownV2(h.placeholder), "*"+escaped+"*", 1)
	}
	for _, b := range boldSpans {
		escaped := escapeMarkdownV2(b.content)
		text = strings.Replace(text, escapeMarkdownV2(b.placeholder), "*"+escaped+"*", 1)
	}
	for _, s := range strikeSpans {
		escaped := escapeMarkdownV2(s.content)
		text = strings.Replace(text, escapeMarkdownV2(s.placeholder), "~"+escaped+"~", 1)
	}
	for _, c := range codeSpans {
		text = strings.Replace(text, escapeMarkdownV2(c.placeholder), c.original, 1)
	}
	for _, l := range linkSpans {
		text = strings.Replace(text, escapeMarkdownV2(l.placeholder), l.original, 1)
	}

	return text
}

// escapeMarkdownV2 escapes special characters for Telegram MarkdownV2.
// Characters that must be escaped: _ * [ ] ( ) ~ ` > # + - = | { } . !
func escapeMarkdownV2(text string) string {
	var b strings.Builder
	b.Grow(len(text) + len(text)/4)
	for _, r := range text {
		if strings.ContainsRune(mdv2SpecialChars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}
