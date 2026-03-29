package registry

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	// DefaultIndexURL is the default download URL for fresh MCP index.
	DefaultIndexURL = "https://raw.githubusercontent.com/xoai/sageclaw/main/pkg/mcp/registry/mcp_index.json.gz"

	// maxIndexDownload is the maximum size for a downloaded index file.
	maxIndexDownload = 10 << 20 // 10 MB

	// IndexFilename is the local override filename.
	IndexFilename = "mcp_index.json.gz"
)

// DownloadIndex fetches a gzipped MCP index from url, validates it,
// and saves to destPath atomically. Returns the parsed index.
func DownloadIndex(indexURL, destPath string) (*CuratedIndex, error) {
	// HTTPS-only (except localhost for testing).
	parsed, err := url.Parse(indexURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("HTTPS required (got %s)", parsed.Scheme)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(indexURL)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Read with size limit.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxIndexDownload+1))
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(data) > maxIndexDownload {
		return nil, fmt.Errorf("index too large (%d bytes, max %d)", len(data), maxIndexDownload)
	}

	// Validate content.
	idx, err := parseGzippedIndex(data)
	if err != nil {
		return nil, fmt.Errorf("invalid index: %w", err)
	}
	if err := ValidateIndex(idx); err != nil {
		return nil, err
	}

	// Atomic write with cross-process lock.
	// O_CREATE|O_EXCL ensures only one process writes at a time.
	tmpPath := destPath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating directory: %w", err)
	}
	tmpFile, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another update is in progress")
		}
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return nil, fmt.Errorf("writing temp file: %w", err)
	}
	tmpFile.Close()
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return nil, fmt.Errorf("atomic rename: %w", err)
	}

	return idx, nil
}

// LoadLocalIndex reads and parses a local gzipped index file.
// Returns nil, nil if the file doesn't exist.
func LoadLocalIndex(path string) (*CuratedIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading local index: %w", err)
	}

	idx, err := parseGzippedIndex(data)
	if err != nil {
		return nil, fmt.Errorf("parsing local index: %w", err)
	}
	if err := ValidateIndex(idx); err != nil {
		return nil, fmt.Errorf("local index invalid: %w", err)
	}
	return idx, nil
}

// ValidateIndex checks that an index has required fields.
func ValidateIndex(idx *CuratedIndex) error {
	if idx.Version <= 0 {
		return fmt.Errorf("invalid index: version must be > 0 (got %d)", idx.Version)
	}
	if len(idx.Servers) == 0 {
		return fmt.Errorf("invalid index: no servers")
	}
	return nil
}

// parseGzippedIndex decompresses gzip data and parses as CuratedIndex JSON.
func parseGzippedIndex(data []byte) (*CuratedIndex, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decompressing: %w", err)
	}
	defer gr.Close()

	jsonData, err := io.ReadAll(gr)
	if err != nil {
		return nil, fmt.Errorf("reading decompressed data: %w", err)
	}

	var idx CuratedIndex
	if err := json.Unmarshal(jsonData, &idx); err != nil {
		return nil, fmt.Errorf("parsing JSON: %w", err)
	}
	return &idx, nil
}

// IndexVersion returns version info for display.
type IndexVersion struct {
	Version  int    `json:"version"`
	Servers  int    `json:"servers"`
	UpdatedAt string `json:"updated_at"`
}

// GetIndexVersion extracts version info from an index.
func GetIndexVersion(idx *CuratedIndex) IndexVersion {
	return IndexVersion{
		Version:   idx.Version,
		Servers:   len(idx.Servers),
		UpdatedAt: idx.UpdatedAt,
	}
}
