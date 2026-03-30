package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"net"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

// RegisterBrowser registers the browser automation tool.
func RegisterBrowser(reg *Registry, bm *BrowserManager) {
	reg.RegisterWithGroup("browser", "Automate a headless browser — navigate, click, fill, screenshot, evaluate, scroll, wait_for_selector, list_links, get_page_info",
		json.RawMessage(`{"type":"object","properties":{`+
			`"action":{"type":"string","enum":["navigate","click","fill","screenshot","get_text","evaluate","scroll","wait_for_selector","list_links","get_page_info"],"description":"Browser action"},`+
			`"url":{"type":"string","description":"URL to navigate to (for navigate)"},`+
			`"selector":{"type":"string","description":"CSS selector (for click, fill, get_text, scroll, wait_for_selector)"},`+
			`"text":{"type":"string","description":"Text to fill (for fill)"},`+
			`"expression":{"type":"string","description":"JavaScript expression to evaluate (for evaluate)"},`+
			`"direction":{"type":"string","enum":["up","down","left","right"],"description":"Scroll direction (for scroll, default: down)"},`+
			`"amount":{"type":"integer","description":"Scroll amount in pixels (for scroll, default: 500)"},`+
			`"timeout_ms":{"type":"integer","description":"Timeout in milliseconds (for wait_for_selector, default: 10000)"}`+
			`},"required":["action"]}`),
		GroupBrowser, RiskSensitive, "builtin", browserTool(bm))
}

func browserTool(bm *BrowserManager) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Action     string `json:"action"`
			URL        string `json:"url"`
			Selector   string `json:"selector"`
			Text       string `json:"text"`
			Expression string `json:"expression"`
			Direction  string `json:"direction"`
			Amount     int    `json:"amount"`
			TimeoutMs  int    `json:"timeout_ms"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		switch params.Action {
		case "navigate":
			return browserNavigate(ctx, bm, params.URL)
		case "click":
			return browserClick(ctx, bm, params.Selector)
		case "fill":
			return browserFill(ctx, bm, params.Selector, params.Text)
		case "screenshot":
			return browserScreenshot(ctx, bm)
		case "get_text":
			return browserGetText(ctx, bm, params.Selector)
		case "evaluate":
			return browserEvaluate(ctx, bm, params.Expression)
		case "scroll":
			return browserScroll(ctx, bm, params.Selector, params.Direction, params.Amount)
		case "wait_for_selector":
			return browserWaitForSelector(ctx, bm, params.Selector, params.TimeoutMs)
		case "list_links":
			return browserListLinks(ctx, bm)
		case "get_page_info":
			return browserGetPageInfo(ctx, bm)
		default:
			return errorResult(fmt.Sprintf("unknown action %q — use: navigate, click, fill, screenshot, get_text, evaluate, scroll, wait_for_selector, list_links, get_page_info", params.Action)), nil
		}
	}
}

// resolveAndValidateSSRF resolves DNS for the URL's host, validates that all IPs
// are non-private, and returns a safe IP. This is the single point of resolution —
// the browser is then navigated via IP to eliminate the TOCTOU DNS rebinding gap.
func resolveAndValidateSSRF(urlStr string) (safeIP string, err error) {
	host := extractHost(urlStr)
	ips, err := net.LookupHost(host)
	if err != nil {
		return "", fmt.Errorf("DNS resolution failed: %w", err)
	}
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return "", fmt.Errorf("SSRF blocked: %s resolves to private IP %s", host, ip)
		}
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("no DNS records for %s", host)
	}
	return ips[0], nil
}

// rewriteURLToIP replaces the hostname in a URL with a resolved IP, preventing
// DNS rebinding between SSRF validation and the browser's own DNS resolution.
func rewriteURLToIP(urlStr, ip string) string {
	host := extractHost(urlStr)
	// Find the host in the URL and replace it with the IP.
	idx := strings.Index(urlStr, host)
	if idx == -1 {
		return urlStr
	}
	return urlStr[:idx] + ip + urlStr[idx+len(host):]
}

func browserNavigate(ctx context.Context, bm *BrowserManager, url string) (*canonical.ToolResult, error) {
	if url == "" {
		return errorResult("url is required for navigate"), nil
	}

	// Resolve DNS and validate against SSRF in a single step. We then navigate
	// the browser to the resolved IP directly, eliminating the TOCTOU DNS
	// rebinding window that exists when checkSSRF and Navigate resolve separately.
	safeIP, err := resolveAndValidateSSRF(url)
	if err != nil {
		return errorResult(err.Error()), nil
	}

	_, err = bm.EnsureBrowser()
	if err != nil {
		return errorResult("browser init failed: " + err.Error()), nil
	}

	page, _, err := bm.NewPage()
	if err != nil {
		return errorResult("new page: " + err.Error()), nil
	}

	// Navigate to the IP-rewritten URL to prevent DNS rebinding.
	// Set the Host header via ExtraHeaders so TLS SNI and virtual hosting still work.
	host := extractHost(url)
	pinnedURL := rewriteURLToIP(url, safeIP)

	// Set Host header so the target server routes correctly.
	removeHeaders, _ := page.SetExtraHeaders([]string{"Host", host})
	if removeHeaders != nil {
		defer removeHeaders()
	}

	err = page.Timeout(30 * time.Second).Navigate(pinnedURL)
	if err != nil {
		return errorResult("navigate failed: " + err.Error()), nil
	}

	// Wait for page load.
	err = page.Timeout(30 * time.Second).WaitLoad()
	if err != nil {
		// Continue even if wait fails — page may be partially loaded.
	}

	// Extract page text.
	text, err := page.Eval(`() => document.body.innerText || ''`)
	if err != nil {
		return errorResult("failed to extract page text: " + err.Error()), nil
	}

	content := text.Value.String()
	if len(content) > maxFetchChars {
		content = content[:maxFetchChars] + "\n... [truncated]"
	}

	// Wrap external content with trust boundary markers.
	content = security.WrapBoundary(content, "browser_navigate", url)

	return &canonical.ToolResult{Content: fmt.Sprintf("Navigated to %s\n\n%s", url, content)}, nil
}

// activePage safely retrieves the most recent browser page without panicking.
// Uses BrowserManager.ActivePage to avoid data races with the idle checker.
func activePage(bm *BrowserManager) (*rod.Page, *canonical.ToolResult) {
	page, err := bm.ActivePage()
	if err != nil {
		return nil, errorResult(err.Error())
	}
	return page, nil
}

func browserClick(ctx context.Context, bm *BrowserManager, selector string) (*canonical.ToolResult, error) {
	if selector == "" {
		return errorResult("selector is required for click"), nil
	}

	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err != nil {
		return errorResult(fmt.Sprintf("element %q not found: %v", selector, err)), nil
	}

	if err := el.Click(proto.InputMouseButtonLeft, 1); err != nil {
		return errorResult("click failed: " + err.Error()), nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Clicked element: %s", selector)}, nil
}

func browserFill(ctx context.Context, bm *BrowserManager, selector, text string) (*canonical.ToolResult, error) {
	if selector == "" {
		return errorResult("selector is required for fill"), nil
	}

	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err != nil {
		return errorResult(fmt.Sprintf("element %q not found: %v", selector, err)), nil
	}

	if err := el.SelectAllText(); err != nil {
		return errorResult("select text failed: " + err.Error()), nil
	}
	if err := el.Input(text); err != nil {
		return errorResult("fill failed: " + err.Error()), nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Filled %s with text (%d chars)", selector, len(text))}, nil
}

func browserScreenshot(ctx context.Context, bm *BrowserManager) (*canonical.ToolResult, error) {
	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	name := fmt.Sprintf("screenshot_%d.png", time.Now().UnixMilli())
	path, err := bm.ScreenshotPath(name)
	if err != nil {
		return errorResult("screenshot dir: " + err.Error()), nil
	}

	data, err := page.Screenshot(true, &proto.PageCaptureScreenshot{
		Format: proto.PageCaptureScreenshotFormatPng,
	})
	if err != nil {
		return errorResult("screenshot failed: " + err.Error()), nil
	}

	if err := writeFile(path, data); err != nil {
		return errorResult("save screenshot: " + err.Error()), nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Screenshot saved: %s", path)}, nil
}

func browserGetText(ctx context.Context, bm *BrowserManager, selector string) (*canonical.ToolResult, error) {
	if selector == "" {
		return errorResult("selector is required for get_text"), nil
	}

	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	el, err := page.Timeout(10 * time.Second).Element(selector)
	if err != nil {
		return errorResult(fmt.Sprintf("element %q not found: %v", selector, err)), nil
	}

	text, err := el.Text()
	if err != nil {
		return errorResult("get text failed: " + err.Error()), nil
	}

	// Wrap external content with trust boundary markers.
	text = security.WrapBoundary(text, "browser_get_text", selector)

	return &canonical.ToolResult{Content: text}, nil
}

func browserEvaluate(ctx context.Context, bm *BrowserManager, expression string) (*canonical.ToolResult, error) {
	if expression == "" {
		return errorResult("expression is required for evaluate"), nil
	}

	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	result, err := page.Eval(expression)
	if err != nil {
		return errorResult("eval failed: " + err.Error()), nil
	}

	// Wrap external content with trust boundary markers.
	content := security.WrapBoundary(result.Value.String(), "browser_evaluate", expression)

	return &canonical.ToolResult{Content: content}, nil
}

func browserScroll(ctx context.Context, bm *BrowserManager, selector, direction string, amount int) (*canonical.ToolResult, error) {
	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	// Scroll to element if selector provided.
	if selector != "" {
		el, err := page.Timeout(10 * time.Second).Element(selector)
		if err != nil {
			return errorResult(fmt.Sprintf("element %q not found: %v", selector, err)), nil
		}
		if err := el.ScrollIntoView(); err != nil {
			return errorResult("scroll to element failed: " + err.Error()), nil
		}
		return &canonical.ToolResult{Content: fmt.Sprintf("Scrolled to element: %s", selector)}, nil
	}

	// Directional scroll.
	if amount <= 0 {
		amount = 500
	}
	if direction == "" {
		direction = "down"
	}

	var deltaX, deltaY float64
	switch direction {
	case "down":
		deltaY = float64(amount)
	case "up":
		deltaY = -float64(amount)
	case "right":
		deltaX = float64(amount)
	case "left":
		deltaX = -float64(amount)
	default:
		return errorResult(fmt.Sprintf("invalid direction %q — use: up, down, left, right", direction)), nil
	}

	if err := page.Mouse.Scroll(deltaX, deltaY, 1); err != nil {
		return errorResult("scroll failed: " + err.Error()), nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Scrolled %s by %d pixels", direction, amount)}, nil
}

func browserWaitForSelector(ctx context.Context, bm *BrowserManager, selector string, timeoutMs int) (*canonical.ToolResult, error) {
	if selector == "" {
		return errorResult("selector is required for wait_for_selector"), nil
	}

	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	if timeoutMs <= 0 {
		timeoutMs = 10000
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond

	_, err := page.Timeout(timeout).Element(selector)
	if err != nil {
		return errorResult(fmt.Sprintf("wait_for_selector timed out after %dms: element %q not found", timeoutMs, selector)), nil
	}

	return &canonical.ToolResult{Content: fmt.Sprintf("Element found: %s", selector)}, nil
}

func browserListLinks(ctx context.Context, bm *BrowserManager) (*canonical.ToolResult, error) {
	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	result, err := page.Eval(`() => {
		const links = Array.from(document.querySelectorAll('a[href]'));
		return links.slice(0, 100).map(a => ({
			text: (a.textContent || '').trim().substring(0, 100),
			href: a.href
		}));
	}`)
	if err != nil {
		return errorResult("list links failed: " + err.Error()), nil
	}

	// Parse the JSON array result.
	raw := result.Value.String()
	var links []struct {
		Text string `json:"text"`
		Href string `json:"href"`
	}
	if err := json.Unmarshal([]byte(raw), &links); err != nil {
		return errorResult("parse links failed: " + err.Error()), nil
	}

	if len(links) == 0 {
		return &canonical.ToolResult{Content: "No links found on this page."}, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d links:\n", len(links))
	for i, link := range links {
		text := link.Text
		if text == "" {
			text = "(no text)"
		}
		fmt.Fprintf(&sb, "%d. [%s](%s)\n", i+1, text, link.Href)
	}

	content := security.WrapBoundary(sb.String(), "browser_list_links", "")

	return &canonical.ToolResult{Content: content}, nil
}

func browserGetPageInfo(ctx context.Context, bm *BrowserManager) (*canonical.ToolResult, error) {
	page, errResult := activePage(bm)
	if errResult != nil {
		return errResult, nil
	}

	result, err := page.Eval(`() => ({
		url: window.location.href,
		title: document.title,
		width: window.innerWidth,
		height: window.innerHeight
	})`)
	if err != nil {
		return errorResult("get page info failed: " + err.Error()), nil
	}

	raw := result.Value.String()
	var info struct {
		URL    string `json:"url"`
		Title  string `json:"title"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := json.Unmarshal([]byte(raw), &info); err != nil {
		return errorResult("parse page info failed: " + err.Error()), nil
	}

	content := fmt.Sprintf("URL: %s\nTitle: %s\nViewport: %dx%d", info.URL, info.Title, info.Width, info.Height)
	return &canonical.ToolResult{Content: content}, nil
}

func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0644)
}
