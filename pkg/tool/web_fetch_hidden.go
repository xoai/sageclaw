package tool

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// hiddenClasses contains CSS class names used by popular frameworks to hide elements.
// Combined from GoClaw + OpenClaw patterns.
var hiddenClasses = map[string]bool{
	// Tailwind CSS
	"hidden": true, "invisible": true, "collapse": true, "sr-only": true,
	// Bootstrap
	"d-none": true, "visually-hidden": true,
	// Bulma
	"is-hidden": true, "is-invisible": true, "is-sr-only": true,
	// Foundation
	"hide": true, "show-for-sr": true,
	// UIKit
	"uk-hidden": true, "uk-invisible": true,
	// Spectre.css
	"d-hide": true, "d-invisible": true, "text-hide": true, "text-assistive": true,
	// Tachyons
	"clip": true, "dn": true, "vis-hidden": true,
	// WordPress
	"screen-reader-text": true,
	// Angular Material / CDK
	"cdk-visually-hidden": true,
	// General (OpenClaw additions)
	"offscreen": true, "clip-hide": true, "screen-reader-only": true,
}

// reOffScreen matches negative positions used to push elements off-screen.
var reOffScreen = regexp.MustCompile(`-[5-9]\d{3,}|-\d{5,}`)

// reZeroFontSize matches font-size:0 but not font-size:0.5em etc.
var reZeroFontSize = regexp.MustCompile(`(?i)font-size\s*:\s*0(?:\s*[;"]|$)`)

// reZeroOpacity matches opacity:0 but not opacity:0.5 etc.
var reZeroOpacity = regexp.MustCompile(`(?i)opacity\s*:\s*0(?:\s*[;"]|$)`)

// --- OpenClaw-sourced patterns ---

// reTransparentColor matches "color: transparent" but NOT "background-color: transparent".
var reTransparentColor = regexp.MustCompile(`(?i)(?:^|;)\s*color\s*:\s*transparent`)

// reClipPathInset matches clip-path:inset(100%) or similar high-percentage insets.
var reClipPathInset = regexp.MustCompile(`(?i)clip-path\s*:\s*inset\s*\(\s*100`)

// reTransformHide matches transform:scale(0) or translateX/Y with large negative values.
var reTransformHide = regexp.MustCompile(`(?i)transform\s*:.*(?:scale\s*\(\s*0\s*\)|translate[XY]\s*\(\s*-[5-9]\d{3})`)

// reTextIndentHide matches text-indent with large negative values (e.g., -9999px).
var reTextIndentHide = regexp.MustCompile(`(?i)text-indent\s*:\s*-[5-9]\d{3}`)

// Pre-compiled patterns for zero-size container detection (width:0 + height:0 + overflow:hidden).
// The (?:\s*[;"]|$) terminator prevents matching fractional values like 0.5em.
var reZeroWidth = regexp.MustCompile(`(?i)width\s*:\s*0(?:\s*[;"]|$)`)
var reZeroHeight = regexp.MustCompile(`(?i)height\s*:\s*0(?:\s*[;"]|$)`)

// isHiddenElement detects elements hidden via HTML attributes, CSS classes,
// or inline styles. Prevents hidden-text prompt injection.
// Patterns sourced from GoClaw + OpenClaw.
func isHiddenElement(n *html.Node) bool {
	// HTML5 hidden attribute.
	for _, a := range n.Attr {
		if a.Key == "hidden" {
			return true
		}
	}
	// aria-hidden="true".
	if getAttr(n, "aria-hidden") == "true" {
		return true
	}
	// type="hidden" on input elements.
	if getAttr(n, "type") == "hidden" {
		return true
	}
	// Known hidden CSS classes.
	classAttr := getAttr(n, "class")
	if classAttr != "" {
		for _, cls := range strings.Fields(classAttr) {
			if hiddenClasses[strings.ToLower(cls)] {
				return true
			}
		}
	}
	// Inline style checks.
	style := getAttr(n, "style")
	if style == "" {
		return false
	}
	styleLower := strings.ToLower(style)
	if strings.Contains(styleLower, "display") && strings.Contains(styleLower, "none") {
		return true
	}
	if strings.Contains(styleLower, "visibility") && strings.Contains(styleLower, "hidden") {
		return true
	}
	if reOffScreen.MatchString(style) {
		return true
	}
	if reZeroFontSize.MatchString(style) {
		return true
	}
	if reZeroOpacity.MatchString(style) {
		return true
	}
	// OpenClaw patterns.
	if reTransparentColor.MatchString(style) {
		return true
	}
	if reClipPathInset.MatchString(style) {
		return true
	}
	if reTransformHide.MatchString(style) {
		return true
	}
	if reTextIndentHide.MatchString(style) {
		return true
	}
	// Zero-size container: width:0 + height:0 + overflow:hidden (all three required).
	if strings.Contains(styleLower, "overflow") && strings.Contains(styleLower, "hidden") {
		if reZeroWidth.MatchString(style) && reZeroHeight.MatchString(style) {
			return true
		}
	}
	return false
}
