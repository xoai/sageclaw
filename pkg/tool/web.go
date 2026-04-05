package tool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
	"github.com/xoai/sageclaw/pkg/security"
)

const (
	maxResponseBytes  = 10 * 1024 * 1024 // 10MB download limit
	maxFetchChars     = 50_000           // ~12.5K tokens inline — matches OpenClaw default
	fetchTimeout      = 30 * time.Second
)

// WebConfig holds dependencies for web tools.
type WebConfig struct {
	FetchCache      *ToolCache              // Cache for web_fetch (15min TTL recommended). Nil disables caching.
	SearchCache     *ToolCache              // Cache for web_search (60min TTL recommended). Nil disables caching.
	ExtractorChain  *ExtractorChain         // Content extraction pipeline. Nil uses legacy extractPageText.
	BraveAPIKey     string                  // Brave Search API key. Empty uses DuckDuckGo only. Deprecated: use ConfigReader.
	InjectionConfig security.InjectionConfig // Content sanitization config. Zero value uses DefaultInjectionConfig().
	BrowserFallback *BrowserManager         // If set, web_fetch falls back to headless browser when extraction is thin (JS-heavy pages).
}

// RegisterWeb registers web search and fetch tools.
func RegisterWeb(reg *Registry, cfg *WebConfig, configReader ...ConfigReader) {
	if cfg == nil {
		cfg = &WebConfig{}
	}
	// Default injection config if not explicitly set.
	if !cfg.InjectionConfig.Enabled && cfg.InjectionConfig.BlockThreshold == 0 {
		cfg.InjectionConfig = security.DefaultInjectionConfig()
	}
	var cr ConfigReader
	if len(configReader) > 0 {
		cr = configReader[0]
	}

	reg.RegisterFull("web_fetch", "Fetch a URL and return its text content",
		json.RawMessage(`{"type":"object","properties":{`+
			`"url":{"type":"string","description":"URL to fetch"},`+
			`"extractMode":{"type":"string","enum":["markdown","text"],"description":"Output format (default: markdown)"},`+
			`"maxChars":{"type":"integer","description":"Maximum output characters (100-50000, default: 50000)"}`+
			`},"required":["url"]}`),
		GroupWeb, RiskModerate, "builtin", true, webFetch(cfg, cr))

	reg.RegisterFull("web_search", "Search the web",
		json.RawMessage(`{"type":"object","properties":{`+
			`"query":{"type":"string","description":"Search query"},`+
			`"num_results":{"type":"integer","description":"Number of results (default 5)"},`+
			`"freshness":{"type":"string","description":"Time filter: pd (past day), pw (past week), pm (past month), py (past year)"},`+
			`"country":{"type":"string","description":"Country code (e.g. us, gb, de)"},`+
			`"search_lang":{"type":"string","description":"Search language (e.g. en, vi, de)"}`+
			`},"required":["query"]}`),
		GroupWeb, RiskModerate, "builtin", true, webSearch(cfg, cr))

	// Config schemas.
	reg.SetConfigSchema("web_search", map[string]ToolConfigField{
		"brave_api_key": {
			Type:        "password",
			Required:    false,
			Description: "Brave Search API key. Without it, web_search uses DuckDuckGo.",
			Link:        "https://brave.com/search/api/",
		},
	})
	reg.SetConfigSchema("web_fetch", map[string]ToolConfigField{
		"max_chars": {
			Type:        "number",
			Description: "Maximum output characters (100-50000)",
			Default:     50000,
		},
		"extract_mode": {
			Type:        "select",
			Description: "Default output format",
			Default:     "markdown",
			Options:     []string{"markdown", "text"},
		},
	})
}

func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return true // fail closed on unparseable IPs
	}
	// Normalize IPv6-mapped IPv4 (e.g. ::ffff:127.0.0.1) to IPv4.
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func checkSSRF(urlStr string) error {
	// Extract hostname.
	host := extractHost(urlStr)

	// Resolve DNS and check IP.
	ips, err := net.LookupHost(host)
	if err != nil {
		return fmt.Errorf("DNS resolution failed: %w", err)
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			return fmt.Errorf("SSRF blocked: %s resolves to private IP %s", host, ip)
		}
	}

	return nil
}

// extractHost extracts the hostname from a URL string.
func extractHost(urlStr string) string {
	host := urlStr
	if idx := strings.Index(host, "://"); idx != -1 {
		host = host[idx+3:]
	}
	if idx := strings.Index(host, "/"); idx != -1 {
		host = host[:idx]
	}
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

// pinnedDialer returns a DialContext function that resolves DNS, validates the IP
// against private ranges, and connects directly to the validated IP. This eliminates
// the TOCTOU race between DNS resolution in checkSSRF and the HTTP client's own
// DNS resolution.
func pinnedDialer() func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}

		// Resolve DNS.
		ips, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("no DNS records for %s", host)
		}

		// Validate all resolved IPs — connect to the first safe one.
		var lastErr error
		for _, ipStr := range ips {
			if isPrivateIP(ipStr) {
				lastErr = fmt.Errorf("SSRF blocked: %s resolves to private IP %s", host, ipStr)
				continue
			}
			// Connect directly to the validated IP.
			pinnedAddr := net.JoinHostPort(ipStr, port)
			conn, err := (&net.Dialer{}).DialContext(ctx, network, pinnedAddr)
			if err != nil {
				lastErr = err
				continue
			}
			return conn, nil
		}
		if lastErr != nil {
			return nil, lastErr
		}
		return nil, fmt.Errorf("SSRF blocked: all IPs for %s are private", host)
	}
}

// newPinnedHTTPClient creates an HTTP client with DNS-pinning SSRF protection.
// The pinned dialer validates IPs at connect time, making the CheckRedirect SSRF
// check redundant for DNS validation (kept for redirect count limiting only).
func newPinnedHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: pinnedDialer(),
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

// cacheChannel returns the agent ID from context for cache isolation.
// Falls back to "_global" if no agent ID is set.
func cacheChannel(ctx context.Context) string {
	if id, ok := ctx.Value(agentIDKey{}).(string); ok && id != "" {
		return id
	}
	return "_global"
}

func webFetch(cfg *WebConfig, cr ConfigReader) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			URL         string `json:"url"`
			ExtractMode string `json:"extractMode"`
			MaxChars    int    `json:"maxChars"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}
		if params.ExtractMode == "" {
			if cr != nil {
				params.ExtractMode = cr(ctx, "web_fetch", "extract_mode")
			}
			if params.ExtractMode == "" {
				params.ExtractMode = "markdown"
			}
		}
		if params.MaxChars <= 0 {
			if cr != nil {
				if s := cr(ctx, "web_fetch", "max_chars"); s != "" {
					if n, err := strconv.Atoi(s); err == nil && n > 0 {
						params.MaxChars = n
					}
				}
			}
			if params.MaxChars <= 0 {
				params.MaxChars = maxFetchChars
			}
		}
		if params.MaxChars < 100 {
			params.MaxChars = 100
		}
		if params.MaxChars > maxFetchChars {
			params.MaxChars = maxFetchChars
		}

		// Adaptive maxChars: step-function scaling (GoClaw pattern).
		// Aggressively reduce context budget as iterations progress.
		if iter, ok := GetIteration(ctx); ok && iter.Max > 0 {
			pct := float64(iter.Current) / float64(iter.Max)
			switch {
			case pct >= 0.75:
				if params.MaxChars > 10_000 {
					params.MaxChars = 10_000
				}
			case pct >= 0.50:
				if params.MaxChars > 20_000 {
					params.MaxChars = 20_000
				}
			}
			if params.MaxChars < 2000 {
				params.MaxChars = 2000
			}
		}

		// Build cache key from all params that affect output.
		cacheKey := params.URL + "|" + params.ExtractMode + "|" + fmt.Sprintf("%d", params.MaxChars)

		// Check cache first.
		if cfg.FetchCache != nil {
			if cached, ok := cfg.FetchCache.Get(cacheChannel(ctx), cacheKey); ok {
				return &canonical.ToolResult{Content: cached + "\n[cached]"}, nil
			}
		}

		// SSRF protection via DNS-pinning dialer.
		// The pinned dialer resolves DNS, validates IPs, and connects to the validated
		// IP directly — eliminating the TOCTOU race of separate check + connect.
		client := newPinnedHTTPClient(fetchTimeout)

		req, err := http.NewRequestWithContext(ctx, "GET", params.URL, nil)
		if err != nil {
			return errorResult("invalid URL: " + err.Error()), nil
		}
		req.Header.Set("User-Agent", "SageClaw/0.1")

		resp, err := client.Do(req)
		if err != nil {
			return errorResult("fetch failed: " + err.Error()), nil
		}
		defer resp.Body.Close()

		ct := resp.Header.Get("Content-Type")
		if isBinaryContentType(ct) {
			return &canonical.ToolResult{
				Content: fmt.Sprintf("HTTP %d — binary content (%s). "+
					"web_fetch only handles text. To download this file, use execute_command with: "+
					"curl -LO '%s'", resp.StatusCode, ct, params.URL),
			}, nil
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return errorResult("reading response: " + err.Error()), nil
		}

		rawBody := string(body)

		var text string
		var extractor string

		// Cloudflare Markdown passthrough: skip extraction for pre-extracted markdown.
		if strings.Contains(ct, "text/markdown") {
			text = rawBody
			extractor = "cf-markdown"
			if params.ExtractMode == "text" {
				text = markdownToText(text)
			}
			if mdTokens := resp.Header.Get("x-markdown-tokens"); mdTokens != "" {
				log.Printf("[web_fetch] cf-markdown tokens hint: %s for %s", mdTokens, params.URL)
			}
		} else {
			// Extraction waterfall (OpenClaw pattern):
			// ExtractorChain: Defuddle → Readability → Markdown → PlainText
			// Fallback: DOM tree walker (GoClaw pattern — skips nav/footer/hidden)
			// Quality signal if all extractors return thin content

			// Stage 1: ExtractorChain (Defuddle → Readability → Markdown → PlainText).
			if cfg.ExtractorChain != nil {
				if extracted, label, err := cfg.ExtractorChain.ExtractWithLabel(ctx, rawBody, params.URL); err == nil && isQualityContent(extracted) {
					text = extracted
					extractor = label
				}
			}

			// Stage 2: DOM tree walker fallback (skips nav/footer/hidden elements).
			if text == "" {
				if params.ExtractMode == "markdown" {
					text = htmlToMarkdown(rawBody)
				} else {
					text = htmlToText(rawBody)
				}
				extractor = "dom-walker"
			}

			// Stage 3: Browser fallback for JS-heavy pages.
			needsBrowser := !isRichContent(text) ||
				(len(rawBody) > 5000 && len(strings.TrimSpace(text))*50 < len(rawBody))
			if needsBrowser && cfg.BrowserFallback != nil {
				if browserText, err := browserFallbackFetch(ctx, cfg.BrowserFallback, params.URL); err == nil && len(browserText) > len(text) {
					text = browserText
					extractor = "browser"
				}
			}

			// Quality signal — prevent LLM from retrying when extraction is thin.
			if text == "" && len(rawBody) > 0 {
				text = "[No content extracted. The page may require JavaScript to render, " +
					"or returned a bot-protection challenge. Do NOT retry this URL — " +
					"try using execute_command with curl instead, or ask the user for the content.]"
				extractor = "none"
			}

			// Text mode post-processing: if extractor chain returned markdown but
			// user requested text mode, convert now. (Readability now produces markdown.)
			if params.ExtractMode == "text" && extractor != "dom-walker" && extractor != "none" {
				text = markdownToText(text)
			}
		}
		// Sanitize external content: homoglyph normalization, zero-width stripping,
		// injection detection (warn/block).
		text = security.SanitizeExternal(text, cfg.InjectionConfig)

		// Compute overhead so truncation is wrapper-aware.
		// This ensures the final output (including boundary markers) fits within maxChars.
		// The boundary closing tag is never chopped because we truncate the inner text
		// before wrapping — the wrapper is applied to already-truncated content.
		metaLine := fmt.Sprintf("HTTP %d [extractor: %s]\n\n", resp.StatusCode, extractor)
		wrapShell := security.WrapBoundary("", "web_fetch", params.URL)
		overhead := len(metaLine) + len(wrapShell)
		contentBudget := params.MaxChars - overhead
		if contentBudget < 500 {
			contentBudget = 500
		}

		truncated := len(text) > contentBudget
		fullText := text // preserve for temp file

		if !truncated {
			// Fits within budget — wrap and return.
			wrapped := security.WrapBoundary(text, "web_fetch", params.URL)
			content := metaLine + wrapped
			if cfg.FetchCache != nil {
				cfg.FetchCache.Set(cacheChannel(ctx), cacheKey, content)
			}
			return &canonical.ToolResult{Content: content}, nil
		}

		// Overflow: write full content to temp file, return preview + path.
		tmpPath, writeErr := writeWebFetchTempFile(fullText)
		// Reserve space for the overflow suffix in the content budget.
		suffixTemplate := "\n\n... [truncated — full content (%d chars) saved to: %s]\n" +
			"Use read_file or execute_command to access the full content."
		suffixApprox := fmt.Sprintf(suffixTemplate, len(fullText), "/tmp/sageclaw-web-fetch/placeholder.txt")
		previewBudget := contentBudget - len(suffixApprox)
		if previewBudget < 200 {
			previewBudget = 200
		}

		preview := text[:previewBudget]
		wrapped := security.WrapBoundary(preview, "web_fetch", params.URL)
		content := metaLine + wrapped

		if writeErr != nil {
			content += "\n... [truncated]"
			return &canonical.ToolResult{Content: content}, nil
		}

		suffix := fmt.Sprintf(suffixTemplate, len(fullText), tmpPath)
		result := content + suffix

		if cfg.FetchCache != nil {
			cfg.FetchCache.Set(cacheChannel(ctx), cacheKey, result)
		}
		return &canonical.ToolResult{Content: result}, nil
	}
}

func webSearch(cfg *WebConfig, cr ConfigReader) ToolFunc {
	// Lazy rate limiter — created on first use with a key.
	var braveLimiter *rateLimiter
	var limiterMu sync.Mutex

	// Pre-init if startup key exists (backward compat).
	if cfg.BraveAPIKey != "" {
		braveLimiter = newRateLimiter(1, time.Second)
	}

	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Query      string `json:"query"`
			NumResults int    `json:"num_results"`
			Freshness  string `json:"freshness"`
			Country    string `json:"country"`
			SearchLang string `json:"search_lang"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}
		if params.NumResults <= 0 {
			params.NumResults = 5
		}
		if params.NumResults > 10 {
			params.NumResults = 10
		}

		// Adaptive result count: reduce at late iterations to save context.
		adaptedCount := adaptiveSearchCount(ctx, params.NumResults)

		// Build cache key (uses adapted count so late iterations get separate cache).
		searchCacheKey := fmt.Sprintf("%s|%d|%s|%s|%s",
			params.Query, adaptedCount, params.Freshness, params.Country, params.SearchLang)

		// Check cache.
		if cfg.SearchCache != nil {
			if cached, ok := cfg.SearchCache.Get(cacheChannel(ctx), searchCacheKey); ok {
				return &canonical.ToolResult{Content: cached + "\n[cached]"}, nil
			}
		}

		var results []searchResult
		var err error
		var source string

		// Resolve Brave API key: ConfigReader (live) -> WebConfig (startup fallback).
		braveKey := ""
		if cr != nil {
			braveKey = cr(ctx, "web_search", "brave_api_key")
		}
		if braveKey == "" {
			braveKey = cfg.BraveAPIKey
		}

		// Lazy limiter init.
		if braveKey != "" {
			limiterMu.Lock()
			if braveLimiter == nil {
				braveLimiter = newRateLimiter(1, time.Second)
			}
			limiterMu.Unlock()
		}

		// Try Brave Search first if configured.
		if braveKey != "" && braveLimiter != nil && braveLimiter.allow() {
			results, err = braveSearch(ctx, braveKey, params.Query, params.NumResults,
				params.Freshness, params.Country, params.SearchLang)
			if err == nil {
				source = "Brave"
			}
		}

		// Fall back to DuckDuckGo.
		if results == nil {
			results, err = duckDuckGoSearch(ctx, params.Query, params.NumResults)
			source = "DuckDuckGo"
		}

		if err != nil {
			return &canonical.ToolResult{
				Content: fmt.Sprintf("Web search failed: %v. Try rephrasing the query or use web_fetch with a specific URL instead.", err),
				IsError: true,
			}, nil
		}

		if len(results) == 0 {
			return &canonical.ToolResult{Content: fmt.Sprintf("No results found for: %s. Try a different query or use web_fetch to access a specific URL.", params.Query)}, nil
		}

		// Apply adaptive count limit.
		if len(results) > adaptedCount {
			results = results[:adaptedCount]
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Search results for: %s (via %s)\n\n", params.Query, source))
		for i, r := range results {
			snippet := truncateSnippet(r.snippet, 200)
			sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.title, r.url, snippet))
		}

		content := sb.String()
		if cfg.SearchCache != nil {
			cfg.SearchCache.Set(cacheChannel(ctx), searchCacheKey, content)
		}
		return &canonical.ToolResult{Content: content}, nil
	}
}

type searchResult struct {
	title   string
	url     string
	snippet string
}

// truncateSnippet limits a search result description to maxLen characters.
func truncateSnippet(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// adaptiveSearchCount reduces the number of search results at late iterations.
func adaptiveSearchCount(ctx context.Context, requested int) int {
	if iter, ok := GetIteration(ctx); ok && iter.Max > 0 {
		pct := float64(iter.Current) / float64(iter.Max)
		switch {
		case pct >= 0.75:
			if requested > 3 {
				return 3
			}
		case pct >= 0.50:
			if requested > 5 {
				return 5
			}
		}
	}
	return requested
}

// duckDuckGoSearch uses DuckDuckGo's HTML interface (no API key needed).
func duckDuckGoSearch(ctx context.Context, query string, numResults int) ([]searchResult, error) {
	searchURL := "https://html.duckduckgo.com/html/?q=" + strings.ReplaceAll(query, " ", "+")

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024)) // 1MB limit
	if err != nil {
		return nil, err
	}

	return parseDDGResults(string(body), numResults), nil
}

// parseDDGResults extracts search results from DuckDuckGo HTML.
func parseDDGResults(html string, max int) []searchResult {
	var results []searchResult

	// DuckDuckGo HTML results are in <a class="result__a"> tags.
	remaining := html
	for len(results) < max {
		// Find result link.
		linkIdx := strings.Index(remaining, "class=\"result__a\"")
		if linkIdx == -1 {
			break
		}
		remaining = remaining[linkIdx:]

		// Extract URL from href.
		hrefIdx := strings.Index(remaining, "href=\"")
		if hrefIdx == -1 {
			break
		}
		remaining = remaining[hrefIdx+6:]
		hrefEnd := strings.Index(remaining, "\"")
		if hrefEnd == -1 {
			break
		}
		rawURL := remaining[:hrefEnd]

		// DuckDuckGo wraps URLs — extract the actual URL.
		if uddgIdx := strings.Index(rawURL, "uddg="); uddgIdx >= 0 {
			rawURL = rawURL[uddgIdx+5:]
			if ampIdx := strings.Index(rawURL, "&"); ampIdx >= 0 {
				rawURL = rawURL[:ampIdx]
			}
			// URL decode (handles all percent-encoded characters).
			if decoded, err := url.QueryUnescape(rawURL); err == nil {
				rawURL = decoded
			}
		}

		// Extract title (text inside the <a> tag).
		titleEnd := strings.Index(remaining, "</a>")
		if titleEnd == -1 {
			break
		}
		title := stripHTML(remaining[hrefEnd+2 : titleEnd])
		remaining = remaining[titleEnd:]

		// Extract snippet from result__snippet.
		snippet := ""
		snippetIdx := strings.Index(remaining, "class=\"result__snippet\"")
		if snippetIdx >= 0 && snippetIdx < 500 {
			snipContent := remaining[snippetIdx:]
			// Find the closing tag.
			closeIdx := strings.Index(snipContent, "</a>")
			if closeIdx == -1 {
				closeIdx = strings.Index(snipContent, "</span>")
			}
			if closeIdx > 0 {
				// Find the > that starts content.
				startIdx := strings.Index(snipContent, ">")
				if startIdx >= 0 && startIdx < closeIdx {
					snippet = stripHTML(snipContent[startIdx+1 : closeIdx])
				}
			}
		}

		if title != "" && rawURL != "" {
			results = append(results, searchResult{
				title:   strings.TrimSpace(title),
				url:     strings.TrimSpace(rawURL),
				snippet: strings.TrimSpace(snippet),
			})
		}
	}

	return results
}

// writeWebFetchTempFile writes content to a temp file and returns the path.
func writeWebFetchTempFile(content string) (string, error) {
	dir := filepath.Join(os.TempDir(), "sageclaw-web-fetch")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	var buf [8]byte
	rand.Read(buf[:])
	name := "web-fetch-" + hex.EncodeToString(buf[:]) + ".txt"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		return "", err
	}
	return path, nil
}

// isBinaryContentType returns true for content types that are not human-readable text.
func isBinaryContentType(ct string) bool {
	ct = strings.ToLower(ct)
	// Split off parameters (e.g. "text/html; charset=utf-8" → "text/html").
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	// Text and JSON/XML types are readable.
	if strings.HasPrefix(ct, "text/") {
		return false
	}
	if ct == "application/json" || ct == "application/xml" ||
		ct == "application/xhtml+xml" || ct == "application/rss+xml" ||
		ct == "application/atom+xml" || ct == "application/javascript" ||
		ct == "application/x-javascript" {
		return false
	}
	// Everything else under application/, image/, audio/, video/ is binary.
	if strings.HasPrefix(ct, "application/") || strings.HasPrefix(ct, "image/") ||
		strings.HasPrefix(ct, "audio/") || strings.HasPrefix(ct, "video/") {
		return true
	}
	// Unknown or empty — allow through (let text extraction handle it).
	return false
}

// extractPageText uses the DOM tree walker for text extraction.
// Kept as a convenience wrapper for backward compatibility.
func extractPageText(rawHTML string) string {
	return htmlToText(rawHTML)
}

// isQualityContent checks if extracted content has enough substance.
func isQualityContent(s string) bool {
	trimmed := strings.TrimSpace(s)
	return len(trimmed) >= 100 && len(strings.Fields(trimmed)) >= 10
}

// isRichContent checks if content is substantial enough to skip browser fallback.
// Higher bar than isQualityContent — we want real article text, not just a footer.
func isRichContent(s string) bool {
	trimmed := strings.TrimSpace(s)
	return len(trimmed) >= 200 && len(strings.Fields(trimmed)) >= 30
}

// browserFallbackFetch renders a page via headless browser and extracts its text.
// Used when HTTP-based extraction yields thin content (JS-heavy sites).
func browserFallbackFetch(ctx context.Context, bm *BrowserManager, urlStr string) (string, error) {
	_, err := bm.EnsureBrowser()
	if err != nil {
		return "", fmt.Errorf("browser init: %w", err)
	}

	page, _, err := bm.NewPage()
	if err != nil {
		return "", fmt.Errorf("new page: %w", err)
	}

	if err := page.Timeout(30 * time.Second).Navigate(urlStr); err != nil {
		return "", fmt.Errorf("navigate: %w", err)
	}

	// Wait for page load + extra settle time for JS rendering.
	_ = page.Timeout(30 * time.Second).WaitLoad()
	time.Sleep(2 * time.Second) // Allow JS frameworks to hydrate.

	result, err := page.Eval(`() => document.body.innerText || ''`)
	if err != nil {
		return "", fmt.Errorf("extract text: %w", err)
	}

	text := result.Value.String()
	if len(text) > maxFetchChars {
		text = text[:maxFetchChars]
	}
	return text, nil
}

// --- Brave Search ---

// braveSearch queries the Brave Search API.
func braveSearch(ctx context.Context, apiKey, query string, count int,
	freshness, country, searchLang string) ([]searchResult, error) {

	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d",
		strings.ReplaceAll(query, " ", "+"), count)
	if freshness != "" {
		u += "&freshness=" + freshness
	}
	if country != "" {
		u += "&country=" + country
	}
	if searchLang != "" {
		u += "&search_lang=" + searchLang
	}

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("X-Subscription-Token", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brave search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave search returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024))
	if err != nil {
		return nil, err
	}

	return parseBraveResults(body, count)
}

func parseBraveResults(data []byte, max int) ([]searchResult, error) {
	var resp struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("brave response parse: %w", err)
	}

	var results []searchResult
	for _, r := range resp.Web.Results {
		if len(results) >= max {
			break
		}
		results = append(results, searchResult{
			title:   r.Title,
			url:     r.URL,
			snippet: r.Description,
		})
	}
	return results, nil
}

// --- Rate Limiter ---

// rateLimiter is a simple token bucket rate limiter.
type rateLimiter struct {
	mu       sync.Mutex
	tokens   int
	max      int
	interval time.Duration
	lastFill time.Time
}

func newRateLimiter(maxTokens int, interval time.Duration) *rateLimiter {
	return &rateLimiter{
		tokens:   maxTokens,
		max:      maxTokens,
		interval: interval,
		lastFill: time.Now(),
	}
}

func (r *rateLimiter) allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastFill)
	refill := int(elapsed / r.interval)
	if refill > 0 {
		r.tokens += refill
		if r.tokens > r.max {
			r.tokens = r.max
		}
		r.lastFill = now
	}

	if r.tokens > 0 {
		r.tokens--
		return true
	}
	return false
}

// stripHTML removes HTML tags from a string.
func stripHTML(s string) string {
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
