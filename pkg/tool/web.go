package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/xoai/sageclaw/pkg/canonical"
)

const (
	maxResponseBytes = 10 * 1024 * 1024 // 10MB
	fetchTimeout     = 30 * time.Second
)

// RegisterWeb registers web search and fetch tools.
func RegisterWeb(reg *Registry) {
	reg.RegisterWithGroup("web_fetch", "Fetch a URL and return its text content",
		json.RawMessage(`{"type":"object","properties":{"url":{"type":"string","description":"URL to fetch"}},"required":["url"]}`),
		GroupWeb, RiskModerate, "builtin", webFetch())

	reg.RegisterWithGroup("web_search", "Search the web",
		json.RawMessage(`{"type":"object","properties":{"query":{"type":"string","description":"Search query"},"num_results":{"type":"integer","description":"Number of results (default 5)"}},"required":["query"]}`),
		GroupWeb, RiskModerate, "builtin", webSearch())
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

func webFetch() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			URL string `json:"url"`
		}
		if err := json.Unmarshal(input, &params); err != nil {
			return errorResult("invalid input: " + err.Error()), nil
		}

		// SSRF protection.
		if err := checkSSRF(params.URL); err != nil {
			return errorResult(err.Error()), nil
		}

		client := &http.Client{
			Timeout: fetchTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 10 {
					return fmt.Errorf("too many redirects")
				}
				// Re-check SSRF on redirect target.
				if err := checkSSRF(req.URL.String()); err != nil {
					return err
				}
				return nil
			},
		}

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

		body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if err != nil {
			return errorResult("reading response: " + err.Error()), nil
		}

		content := fmt.Sprintf("HTTP %d\n\n%s", resp.StatusCode, string(body))
		if len(content) > maxOutputBytes {
			content = content[:maxOutputBytes] + "\n... [truncated at 16KB — full output too large for context]"
		}

		return &canonical.ToolResult{Content: content}, nil
	}
}

func webSearch() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (*canonical.ToolResult, error) {
		var params struct {
			Query      string `json:"query"`
			NumResults int    `json:"num_results"`
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

		// Search using DuckDuckGo HTML (no API key required).
		results, err := duckDuckGoSearch(ctx, params.Query, params.NumResults)
		if err != nil {
			return &canonical.ToolResult{
				Content: fmt.Sprintf("Web search failed: %v. Try rephrasing the query or use web_fetch with a specific URL instead.", err),
				IsError: true,
			}, nil
		}

		if len(results) == 0 {
			return &canonical.ToolResult{Content: fmt.Sprintf("No results found for: %s. Try a different query or use web_fetch to access a specific URL.", params.Query)}, nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", params.Query))
		for i, r := range results {
			sb.WriteString(fmt.Sprintf("%d. %s\n   %s\n   %s\n\n", i+1, r.title, r.url, r.snippet))
		}
		return &canonical.ToolResult{Content: sb.String()}, nil
	}
}

type searchResult struct {
	title   string
	url     string
	snippet string
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
			// URL decode.
			rawURL = strings.ReplaceAll(rawURL, "%3A", ":")
			rawURL = strings.ReplaceAll(rawURL, "%2F", "/")
			rawURL = strings.ReplaceAll(rawURL, "%3F", "?")
			rawURL = strings.ReplaceAll(rawURL, "%3D", "=")
			rawURL = strings.ReplaceAll(rawURL, "%26", "&")
			rawURL = strings.ReplaceAll(rawURL, "%25", "%")
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
