package tool

import (
	"context"
	"fmt"
	"io"
	"net/http"
	nurl "net/url"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	readability "codeberg.org/readeck/go-readability/v2"
)

// ContentExtractor extracts readable content from raw HTML.
type ContentExtractor interface {
	Extract(ctx context.Context, rawHTML string, url string) (string, error)
	Name() string
}

// ExtractorChain tries extractors in order, returning the first result
// that passes the quality check (≥minChars characters, ≥minWords words).
type ExtractorChain struct {
	extractors []ContentExtractor
	minChars   int
	minWords   int
}

// NewDefaultChain returns an extractor chain with:
// Defuddle (if endpoint configured) → Readability → Markdown → PlainText.
func NewDefaultChain(defuddleURL string) *ExtractorChain {
	var extractors []ContentExtractor
	if defuddleURL != "" {
		extractors = append(extractors, NewDefuddleExtractor(defuddleURL))
	}
	extractors = append(extractors,
		&ReadabilityExtractor{},
		&MarkdownExtractor{},
		&PlainTextExtractor{},
	)
	return &ExtractorChain{
		extractors: extractors,
		minChars:   100,
		minWords:   10,
	}
}

// ExtractWithLabel tries each extractor in order and returns the first result
// that passes the quality threshold, along with the extractor name that succeeded.
func (c *ExtractorChain) ExtractWithLabel(ctx context.Context, rawHTML string, url string) (string, string, error) {
	var lastErr error
	for _, ext := range c.extractors {
		result, err := ext.Extract(ctx, rawHTML, url)
		if err != nil {
			lastErr = err
			continue
		}
		if c.passesQuality(result) {
			return result, ext.Name(), nil
		}
		// Result too short/sparse — try next extractor.
		lastErr = fmt.Errorf("%s: result below quality threshold (%d chars, need %d)", ext.Name(), len(result), c.minChars)
	}
	if lastErr != nil {
		return "", "", fmt.Errorf("all extractors failed: %w", lastErr)
	}
	return "", "", fmt.Errorf("no extractors configured")
}

// Extract tries each extractor in order and returns the first result
// that passes the quality threshold.
func (c *ExtractorChain) Extract(ctx context.Context, rawHTML string, url string) (string, error) {
	result, _, err := c.ExtractWithLabel(ctx, rawHTML, url)
	return result, err
}

func (c *ExtractorChain) passesQuality(text string) bool {
	if len(text) < c.minChars {
		return false
	}
	words := len(strings.Fields(text))
	return words >= c.minWords
}

// --- MarkdownExtractor ---

// MarkdownExtractor converts HTML to markdown using html-to-markdown library.
type MarkdownExtractor struct{}

func (m *MarkdownExtractor) Name() string { return "markdown" }

func (m *MarkdownExtractor) Extract(_ context.Context, rawHTML string, _ string) (string, error) {
	md, err := htmltomarkdown.ConvertString(rawHTML)
	if err != nil {
		return "", fmt.Errorf("markdown conversion: %w", err)
	}
	return md, nil
}

// --- DefuddleExtractor ---

// DefuddleExtractor calls an external Defuddle service (e.g. Cloudflare Worker)
// to extract article content from HTML. Includes a circuit breaker.
type DefuddleExtractor struct {
	endpoint string
	client   *http.Client

	mu         sync.Mutex
	failures   int
	lastFail   time.Time
	tripped    bool
	tripWindow time.Duration // failures within this window trip the breaker
	maxFails   int
	cooldown   time.Duration // how long breaker stays open
}

// NewDefuddleExtractor creates a Defuddle extractor pointing at the given endpoint.
func NewDefuddleExtractor(endpoint string) *DefuddleExtractor {
	return &DefuddleExtractor{
		endpoint: endpoint,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
		tripWindow: 60 * time.Second,
		maxFails:   3,
		cooldown:   120 * time.Second,
	}
}

func (d *DefuddleExtractor) Name() string { return "defuddle" }

func (d *DefuddleExtractor) Extract(ctx context.Context, rawHTML string, url string) (string, error) {
	if d.isTripped() {
		return "", fmt.Errorf("defuddle circuit breaker open")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, strings.NewReader(rawHTML))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "text/html")
	if url != "" {
		req.Header.Set("X-Source-URL", url)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		d.recordFailure()
		return "", fmt.Errorf("defuddle request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		d.recordFailure()
		return "", fmt.Errorf("defuddle returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1*1024*1024)) // 1MB limit
	if err != nil {
		return "", fmt.Errorf("defuddle read: %w", err)
	}

	d.recordSuccess()
	return string(body), nil
}

func (d *DefuddleExtractor) isTripped() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.tripped {
		return false
	}
	if time.Since(d.lastFail) > d.cooldown {
		d.tripped = false
		d.failures = 0
		return false
	}
	return true
}

func (d *DefuddleExtractor) recordFailure() {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := time.Now()
	if time.Since(d.lastFail) > d.tripWindow {
		d.failures = 0
	}
	d.failures++
	d.lastFail = now
	if d.failures >= d.maxFails {
		d.tripped = true
	}
}

func (d *DefuddleExtractor) recordSuccess() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.failures = 0
	d.tripped = false
}

// --- ReadabilityExtractor ---

// ReadabilityExtractor uses Mozilla's Readability algorithm (go-readability)
// to isolate main article content from boilerplate.
type ReadabilityExtractor struct{}

func (r *ReadabilityExtractor) Name() string { return "readability" }

func (r *ReadabilityExtractor) Extract(_ context.Context, rawHTML string, sourceURL string) (string, error) {
	parsedURL, err := nurl.Parse(sourceURL)
	if err != nil {
		parsedURL = &nurl.URL{}
	}
	article, err := readability.FromReader(strings.NewReader(rawHTML), parsedURL)
	if err != nil {
		return "", fmt.Errorf("readability parse: %w", err)
	}
	var sb strings.Builder
	if err := article.RenderText(&sb); err != nil {
		return "", fmt.Errorf("readability render: %w", err)
	}
	return strings.TrimSpace(sb.String()), nil
}

// --- PlainTextExtractor ---

// PlainTextExtractor is the legacy fallback — strips HTML tags, scripts, styles.
type PlainTextExtractor struct{}

func (p *PlainTextExtractor) Name() string { return "plaintext" }

func (p *PlainTextExtractor) Extract(_ context.Context, rawHTML string, _ string) (string, error) {
	return extractPageText(rawHTML), nil
}
