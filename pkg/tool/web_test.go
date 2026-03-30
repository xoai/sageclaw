package tool

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestExtractPageText_HTMLPage(t *testing.T) {
	html := `<html><head>
		<script type="text/javascript">var x = 1; console.log("hello");</script>
		<style>.foo { color: red; }</style>
		<script src="app.js"></script>
	</head><body>
		<h1>Hello World</h1>
		<p>This is <strong>important</strong> content.</p>
		<noscript>Enable JavaScript</noscript>
		<div>Footer text</div>
	</body></html>`

	result := extractPageText(html)

	// Should NOT contain script/style/noscript content.
	if strings.Contains(result, "console.log") {
		t.Fatal("expected script content to be removed")
	}
	if strings.Contains(result, "color: red") {
		t.Fatal("expected style content to be removed")
	}
	if strings.Contains(result, "Enable JavaScript") {
		t.Fatal("expected noscript content to be removed")
	}

	// Should contain actual text.
	if !strings.Contains(result, "Hello World") {
		t.Fatal("expected heading text to be preserved")
	}
	if !strings.Contains(result, "important") {
		t.Fatal("expected paragraph text to be preserved")
	}
	// Footer is inside a <div>, not <footer>, so it's preserved.
	if !strings.Contains(result, "Footer text") {
		t.Fatal("expected div text to be preserved")
	}
}

func TestExtractPageText_NonHTML(t *testing.T) {
	// JSON response — DOM parser wraps in <html><body>, but text should survive.
	jsonStr := `{"status": "ok", "data": [1, 2, 3]}`
	result := extractPageText(jsonStr)
	if !strings.Contains(result, "status") {
		t.Fatalf("expected JSON content to survive extraction, got: %s", result)
	}

	// Plain text should survive extraction.
	plain := "Hello, this is plain text without any HTML."
	result = extractPageText(plain)
	if !strings.Contains(result, "plain text") {
		t.Fatalf("expected plain text to survive, got: %s", result)
	}
}

func TestExtractPageText_GitHubLike(t *testing.T) {
	// Simulate a GitHub-like page with many script tags in <head>.
	var sb strings.Builder
	sb.WriteString("<html><head>\n")
	for i := 0; i < 100; i++ {
		sb.WriteString(`<script crossorigin="anonymous" type="application/javascript" src="https://github.githubassets.com/assets/chunk-12345.js" defer="defer"></script>` + "\n")
	}
	sb.WriteString(`<style>body { margin: 0; } .repo-header { display: flex; }</style>`)
	sb.WriteString("</head><body>\n")
	sb.WriteString("<h1>anthropics/claude-code</h1>\n")
	sb.WriteString("<p>Claude Code is an agentic coding tool.</p>\n")
	sb.WriteString("<div class='readme'>README content here with useful information.</div>\n")
	sb.WriteString("</body></html>")

	html := sb.String()
	result := extractPageText(html)

	// The raw HTML would be huge due to script tags.
	if len(html) < 10000 {
		t.Fatalf("test HTML should be large, got %d bytes", len(html))
	}

	// Clean text should be small.
	if len(result) > 500 {
		t.Fatalf("clean text should be compact, got %d bytes: %s", len(result), result)
	}

	// Should contain the actual content.
	if !strings.Contains(result, "anthropics/claude-code") {
		t.Fatal("expected repo name")
	}
	if !strings.Contains(result, "agentic coding tool") {
		t.Fatal("expected description")
	}
	if !strings.Contains(result, "README content") {
		t.Fatal("expected README content")
	}

	// Should NOT contain script references.
	if strings.Contains(result, "githubassets") {
		t.Fatal("expected script tags to be removed")
	}
}

func TestWriteWebFetchTempFile(t *testing.T) {
	content := "Hello, this is test content for temp file overflow."
	path, err := writeWebFetchTempFile(content)
	if err != nil {
		t.Fatalf("writeWebFetchTempFile: %v", err)
	}
	defer os.Remove(path)

	// Verify file exists and contains the content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading temp file: %v", err)
	}
	if string(data) != content {
		t.Fatalf("expected %q, got %q", content, string(data))
	}

	// Verify path is in the expected directory.
	if !strings.Contains(path, "sageclaw-web-fetch") {
		t.Fatalf("expected path in sageclaw-web-fetch dir, got %s", path)
	}
}

func TestExtractPageText_CollapseWhitespace(t *testing.T) {
	html := "<div>  lots   of    spaces  </div>\n\n\n<p>and blank lines</p>"
	result := extractPageText(html)

	if strings.Contains(result, "  ") {
		t.Fatalf("expected whitespace to be collapsed, got: %q", result)
	}
}

func TestParseBraveResults(t *testing.T) {
	braveJSON := []byte(`{
		"web": {
			"results": [
				{"title": "Go Programming", "url": "https://go.dev", "description": "The Go programming language"},
				{"title": "Go Wiki", "url": "https://go.dev/wiki", "description": "Go community wiki"}
			]
		}
	}`)

	results, err := parseBraveResults(braveJSON, 5)
	if err != nil {
		t.Fatalf("parseBraveResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].title != "Go Programming" {
		t.Errorf("expected title 'Go Programming', got %q", results[0].title)
	}
	if results[0].url != "https://go.dev" {
		t.Errorf("expected url 'https://go.dev', got %q", results[0].url)
	}
}

func TestParseBraveResults_LimitMax(t *testing.T) {
	braveJSON := []byte(`{
		"web": {
			"results": [
				{"title": "A", "url": "https://a.com", "description": "a"},
				{"title": "B", "url": "https://b.com", "description": "b"},
				{"title": "C", "url": "https://c.com", "description": "c"}
			]
		}
	}`)

	results, err := parseBraveResults(braveJSON, 2)
	if err != nil {
		t.Fatalf("parseBraveResults: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results (max), got %d", len(results))
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(2, 100*time.Millisecond)
	// Should allow 2 immediate requests.
	if !rl.allow() {
		t.Fatal("expected first allow")
	}
	if !rl.allow() {
		t.Fatal("expected second allow")
	}
	// Third should be denied.
	if rl.allow() {
		t.Fatal("expected third to be rate limited")
	}
	// Wait for refill.
	time.Sleep(150 * time.Millisecond)
	if !rl.allow() {
		t.Fatal("expected allow after refill")
	}
}

// --- SSRF Tests ---

func TestIsPrivateIP(t *testing.T) {
	tests := []struct {
		ip      string
		private bool
	}{
		// Loopback
		{"127.0.0.1", true},
		{"127.0.0.2", true},
		// RFC 1918
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.0.1", true},
		{"192.168.255.255", true},
		// Link-local
		{"169.254.1.1", true},
		// Unspecified
		{"0.0.0.0", true},
		// Public IPs
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"93.184.216.34", false},
		{"203.0.113.1", false},
		// IPv6 loopback
		{"::1", true},
		// IPv6-mapped IPv4 private
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:192.168.1.1", true},
		// IPv6-mapped IPv4 public
		{"::ffff:8.8.8.8", false},
		// Unparseable — fail closed
		{"notanip", true},
		{"", true},
	}
	for _, tt := range tests {
		got := isPrivateIP(tt.ip)
		if got != tt.private {
			t.Errorf("isPrivateIP(%q) = %v, want %v", tt.ip, got, tt.private)
		}
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"https://example.com/path", "example.com"},
		{"http://example.com:8080/path", "example.com"},
		{"https://sub.domain.com/", "sub.domain.com"},
		{"example.com/foo", "example.com"},
	}
	for _, tt := range tests {
		got := extractHost(tt.input)
		if got != tt.want {
			t.Errorf("extractHost(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCheckSSRF_PublicIP(t *testing.T) {
	// google.com should be allowed (public IP).
	if err := checkSSRF("https://google.com"); err != nil {
		t.Errorf("checkSSRF should allow public IPs, got: %v", err)
	}
}

func TestCheckSSRF_Localhost(t *testing.T) {
	if err := checkSSRF("http://localhost/secret"); err == nil {
		t.Error("checkSSRF should block localhost")
	}
}

func TestCheckSSRF_PrivateIP(t *testing.T) {
	// These resolve to their own IPs — test the IP check directly.
	privateHosts := []string{
		"http://127.0.0.1/admin",
		"http://[::1]/admin",
	}
	for _, u := range privateHosts {
		if err := checkSSRF(u); err == nil {
			t.Errorf("checkSSRF should block %q", u)
		}
	}
}

// --- DDG URL Decoder Tests ---

func TestDDGURLDecoder(t *testing.T) {
	// Simulate a DDG-wrapped URL with various percent-encoded chars.
	encoded := url.QueryEscape("https://example.com/path?q=hello+world&lang=en#section")
	rawURL := "//duckduckgo.com/l/?uddg=" + encoded + "&rut=abc"

	// Extract the uddg parameter.
	uddgIdx := strings.Index(rawURL, "uddg=")
	if uddgIdx < 0 {
		t.Fatal("uddg not found")
	}
	result := rawURL[uddgIdx+5:]
	if ampIdx := strings.Index(result, "&"); ampIdx >= 0 {
		result = result[:ampIdx]
	}
	// Decode using url.QueryUnescape (what our code now does).
	decoded, err := url.QueryUnescape(result)
	if err != nil {
		t.Fatalf("QueryUnescape: %v", err)
	}
	expected := "https://example.com/path?q=hello+world&lang=en#section"
	if decoded != expected {
		t.Errorf("decoded URL = %q, want %q", decoded, expected)
	}
}

func TestIsBinaryContentType(t *testing.T) {
	tests := []struct {
		ct     string
		binary bool
	}{
		{"text/html; charset=utf-8", false},
		{"text/plain", false},
		{"application/json", false},
		{"application/xml", false},
		{"application/javascript", false},
		{"application/xhtml+xml", false},
		{"application/zip", true},
		{"application/octet-stream", true},
		{"application/pdf", true},
		{"application/gzip", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"audio/mpeg", true},
		{"video/mp4", true},
		{"", false},               // empty — allow through
		{"unknown/type", false},   // unknown — allow through
	}
	for _, tt := range tests {
		got := isBinaryContentType(tt.ct)
		if got != tt.binary {
			t.Errorf("isBinaryContentType(%q) = %v, want %v", tt.ct, got, tt.binary)
		}
	}
}
