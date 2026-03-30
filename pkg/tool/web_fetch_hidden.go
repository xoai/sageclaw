package tool

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// hiddenClasses contains CSS class names used by popular frameworks to hide elements.
// Ported from GoClaw's web_fetch_hidden.go.
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
	// General
	"offscreen": true, "clip-hide": true,
}

// reOffScreen matches negative positions used to push elements off-screen.
var reOffScreen = regexp.MustCompile(`-[5-9]\d{3,}|-\d{5,}`)

// reZeroFontSize matches font-size:0 but not font-size:0.5em etc.
var reZeroFontSize = regexp.MustCompile(`(?i)font-size\s*:\s*0(?:\s*[;"]|$)`)

// reZeroOpacity matches opacity:0 but not opacity:0.5 etc.
var reZeroOpacity = regexp.MustCompile(`(?i)opacity\s*:\s*0(?:\s*[;"]|$)`)

// isHiddenElement detects elements hidden via HTML attributes, CSS classes,
// or inline styles. Prevents hidden-text prompt injection.
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
	style := strings.ToLower(getAttr(n, "style"))
	if style == "" {
		return false
	}
	if strings.Contains(style, "display") && strings.Contains(style, "none") {
		return true
	}
	if strings.Contains(style, "visibility") && strings.Contains(style, "hidden") {
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
	return false
}
