package tool

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

// BrowserManager manages a lazy-initialized headless browser via Rod.
// Max concurrent pages, idle timeout, and CHROME_PATH support.
type BrowserManager struct {
	mu            sync.Mutex
	browser       *rod.Browser
	pages         map[string]*rod.Page
	pageLastUsed  map[string]time.Time // LRU tracking per page
	maxPages      int
	idleTimeout   time.Duration
	screenshotDir string
	lastUsed      time.Time
	idleStop      chan struct{} // signals idle checker goroutine to stop
}

// NewBrowserManager creates a browser manager.
// screenshotDir is where screenshots are saved (e.g. workspace/screenshots).
func NewBrowserManager(screenshotDir string) *BrowserManager {
	return &BrowserManager{
		pages:         make(map[string]*rod.Page),
		pageLastUsed:  make(map[string]time.Time),
		maxPages:      5,
		idleTimeout:   5 * time.Minute,
		screenshotDir: screenshotDir,
	}
}

// EnsureBrowser lazily initializes the browser on first use.
func (bm *BrowserManager) EnsureBrowser() (*rod.Browser, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser != nil {
		bm.lastUsed = time.Now()
		return bm.browser, nil
	}

	// Try CHROME_PATH first, then Rod auto-download.
	chromePath := os.Getenv("CHROME_PATH")
	var l *launcher.Launcher
	if chromePath != "" {
		l = launcher.New().Bin(chromePath)
	} else {
		l = launcher.New()
	}

	u, err := l.Headless(true).Launch()
	if err != nil {
		return nil, fmt.Errorf("failed to launch browser (set CHROME_PATH env var if Chromium is not auto-downloadable): %w", err)
	}

	browser := rod.New().ControlURL(u)
	if err := browser.Connect(); err != nil {
		return nil, fmt.Errorf("browser connect: %w", err)
	}

	bm.browser = browser
	bm.lastUsed = time.Now()
	log.Printf("browser: launched (chrome_path=%q)", chromePath)

	// Start idle timeout checker.
	bm.idleStop = make(chan struct{})
	go bm.idleChecker()

	return bm.browser, nil
}

// idleChecker periodically checks if the browser has been idle and closes it.
func (bm *BrowserManager) idleChecker() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-bm.idleStop:
			return
		case <-ticker.C:
			bm.mu.Lock()
			if bm.browser != nil && time.Since(bm.lastUsed) > bm.idleTimeout {
				log.Printf("browser: closing after %v idle", bm.idleTimeout)
				for id, page := range bm.pages {
					page.Close()
					delete(bm.pages, id)
					delete(bm.pageLastUsed, id)
				}
				bm.browser.Close()
				bm.browser = nil
			}
			bm.mu.Unlock()
		}
	}
}

// NewPage creates a new page, respecting the max pages limit.
// Returns a page ID for tracking.
func (bm *BrowserManager) NewPage() (*rod.Page, string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if len(bm.pages) >= bm.maxPages {
		// Close least recently used page (proper LRU).
		var oldestID string
		var oldestTime time.Time
		for id, t := range bm.pageLastUsed {
			if oldestID == "" || t.Before(oldestTime) {
				oldestID = id
				oldestTime = t
			}
		}
		if oldestID != "" {
			bm.pages[oldestID].Close()
			delete(bm.pages, oldestID)
			delete(bm.pageLastUsed, oldestID)
		}
	}

	if bm.browser == nil {
		return nil, "", fmt.Errorf("browser not initialized — call EnsureBrowser first")
	}

	page, err := bm.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return nil, "", fmt.Errorf("new page: %w", err)
	}

	id := fmt.Sprintf("page_%d", time.Now().UnixNano())
	bm.pages[id] = page
	bm.pageLastUsed[id] = time.Now()
	return page, id, nil
}

// ScreenshotDir returns the screenshot directory, creating it if needed.
func (bm *BrowserManager) ScreenshotDir() (string, error) {
	if err := os.MkdirAll(bm.screenshotDir, 0755); err != nil {
		return "", err
	}
	return bm.screenshotDir, nil
}

// ScreenshotPath returns a path for a new screenshot.
func (bm *BrowserManager) ScreenshotPath(name string) (string, error) {
	dir, err := bm.ScreenshotDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// Close shuts down the browser, all pages, and the idle checker.
func (bm *BrowserManager) Close() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Stop idle checker.
	if bm.idleStop != nil {
		close(bm.idleStop)
		bm.idleStop = nil
	}

	for id, page := range bm.pages {
		page.Close()
		delete(bm.pages, id)
		delete(bm.pageLastUsed, id)
	}
	if bm.browser != nil {
		bm.browser.Close()
		bm.browser = nil
	}
}

// ActivePage returns the most recent browser page, holding the mutex to prevent
// races with the idle checker. Also refreshes lastUsed to prevent idle timeout
// during active sessions.
func (bm *BrowserManager) ActivePage() (*rod.Page, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	if bm.browser == nil {
		return nil, fmt.Errorf("no browser session — navigate first")
	}

	pages, err := bm.browser.Pages()
	if err != nil {
		return nil, fmt.Errorf("failed to list pages: %w", err)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("no pages open — navigate first")
	}

	bm.lastUsed = time.Now()
	return pages[len(pages)-1], nil
}

// PageCount returns the number of active pages.
func (bm *BrowserManager) PageCount() int {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	return len(bm.pages)
}
