package skillstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SkillManifest describes a skill's files and metadata from GitHub.
type SkillManifest struct {
	Name        string
	Description string
	Files       []FileEntry
	HasScripts  bool
	TreeSHA     string
	SkillMD     string // raw SKILL.md content
}

// FileEntry represents a single file in a skill.
type FileEntry struct {
	Path    string `json:"path"`
	Size    int    `json:"size"`
	SHA     string `json:"sha"`
	Content []byte `json:"-"`
}

// GitHubClient fetches individual skills from GitHub repos.
type GitHubClient struct {
	httpClient *http.Client
	token      string
}

// NewGitHubClient creates a GitHub API client.
// token is optional — provides higher rate limits if set.
func NewGitHubClient(token string, client *http.Client) *GitHubClient {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &GitHubClient{
		httpClient: client,
		token:      token,
	}
}

// ParseSource parses "owner/repo@skill-name" into components.
// Also supports "owner/repo" (skill name derived from repo name).
func ParseSource(source string) (owner, repo, skillName string, err error) {
	source = strings.TrimSpace(source)

	// Remove https://github.com/ prefix if present.
	source = strings.TrimPrefix(source, "https://github.com/")
	source = strings.TrimPrefix(source, "http://github.com/")
	source = strings.TrimSuffix(source, ".git")
	source = strings.TrimSuffix(source, "/")

	// Split on @.
	parts := strings.SplitN(source, "@", 2)
	ownerRepo := parts[0]
	if len(parts) == 2 {
		skillName = parts[1]
	}

	// Split owner/repo.
	orParts := strings.SplitN(ownerRepo, "/", 2)
	if len(orParts) != 2 || orParts[0] == "" || orParts[1] == "" {
		return "", "", "", fmt.Errorf("invalid source %q: expected owner/repo[@skill]", source)
	}
	owner = orParts[0]
	repo = orParts[1]

	// If no skill name, try to find skills in the repo root.
	if skillName == "" {
		skillName = repo
	}

	return owner, repo, skillName, nil
}

// Preview fetches skill metadata without downloading all file contents.
func (c *GitHubClient) Preview(ctx context.Context, owner, repo, skill string) (*SkillManifest, error) {
	// Try skills/{skill}/ first, then root SKILL.md.
	paths := []string{
		fmt.Sprintf("skills/%s", skill),
		skill,
		"", // root
	}

	for _, basePath := range paths {
		manifest, err := c.fetchManifest(ctx, owner, repo, basePath, skill)
		if err == nil {
			return manifest, nil
		}
	}

	return nil, fmt.Errorf("skill %q not found in %s/%s", skill, owner, repo)
}

// Download fetches all skill files and saves them to destDir.
func (c *GitHubClient) Download(ctx context.Context, owner, repo, skill, destDir string) (*SkillManifest, error) {
	manifest, err := c.Preview(ctx, owner, repo, skill)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	for i, f := range manifest.Files {
		content, err := c.fetchFileContent(ctx, owner, repo, f.Path)
		if err != nil {
			return nil, fmt.Errorf("download %s: %w", f.Path, err)
		}
		manifest.Files[i].Content = content

		// Determine relative path within the skill.
		relPath := f.Path
		// Strip the skill directory prefix if present.
		for _, prefix := range []string{
			fmt.Sprintf("skills/%s/", skill),
			fmt.Sprintf("%s/", skill),
		} {
			if strings.HasPrefix(relPath, prefix) {
				relPath = strings.TrimPrefix(relPath, prefix)
				break
			}
		}

		destPath := filepath.Join(destDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return nil, fmt.Errorf("create dir for %s: %w", relPath, err)
		}
		if err := os.WriteFile(destPath, content, 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", relPath, err)
		}
	}

	return manifest, nil
}

// GetTreeSHA fetches the current tree SHA for a skill path in the repo.
func (c *GitHubClient) GetTreeSHA(ctx context.Context, owner, repo, skill string) (string, error) {
	// Try skills/{skill} first, then root.
	for _, path := range []string{
		fmt.Sprintf("skills/%s", skill),
		skill,
	} {
		sha, err := c.fetchTreeSHA(ctx, owner, repo, path)
		if err == nil {
			return sha, nil
		}
	}
	return "", fmt.Errorf("could not determine tree SHA for %s in %s/%s", skill, owner, repo)
}

// --- internal ---

func (c *GitHubClient) fetchManifest(ctx context.Context, owner, repo, path, skillName string) (*SkillManifest, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)

	body, err := c.doGet(ctx, apiURL)
	if err != nil {
		return nil, err
	}

	// Could be a single file or a directory listing.
	var items []ghContentItem
	if err := json.Unmarshal(body, &items); err != nil {
		// Might be a single file — check if SKILL.md.
		var single ghContentItem
		if err2 := json.Unmarshal(body, &single); err2 != nil {
			return nil, fmt.Errorf("parse contents: %w", err)
		}
		if strings.ToUpper(single.Name) != "SKILL.MD" {
			return nil, fmt.Errorf("not a skill directory")
		}
		content, _ := base64.StdEncoding.DecodeString(single.Content)
		return &SkillManifest{
			Name:    skillName,
			Files:   []FileEntry{{Path: single.Path, Size: single.Size, SHA: single.SHA}},
			SkillMD: string(content),
		}, nil
	}

	// Directory listing — find SKILL.md and enumerate files.
	manifest := &SkillManifest{Name: skillName}
	hasSkillMD := false

	for _, item := range items {
		if item.Type == "dir" {
			if item.Name == "tools" {
				manifest.HasScripts = true
				// Recurse into tools/ to list scripts.
				subItems, err := c.listDir(ctx, owner, repo, item.Path)
				if err == nil {
					for _, sub := range subItems {
						manifest.Files = append(manifest.Files, FileEntry{
							Path: sub.Path, Size: sub.Size, SHA: sub.SHA,
						})
					}
				}
			}
			continue
		}

		manifest.Files = append(manifest.Files, FileEntry{
			Path: item.Path, Size: item.Size, SHA: item.SHA,
		})

		if strings.ToUpper(item.Name) == "SKILL.MD" {
			hasSkillMD = true
			content, _ := base64.StdEncoding.DecodeString(item.Content)
			if len(content) == 0 {
				// Content not inline — fetch it.
				fetched, err := c.fetchFileContent(ctx, owner, repo, item.Path)
				if err == nil {
					content = fetched
				}
			}
			manifest.SkillMD = string(content)
		}
	}

	if !hasSkillMD {
		return nil, fmt.Errorf("no SKILL.md found at %s", path)
	}

	// Get tree SHA for the directory.
	manifest.TreeSHA, _ = c.fetchTreeSHA(ctx, owner, repo, path)

	return manifest, nil
}

func (c *GitHubClient) listDir(ctx context.Context, owner, repo, path string) ([]ghContentItem, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	body, err := c.doGet(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	var items []ghContentItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *GitHubClient) fetchFileContent(ctx context.Context, owner, repo, path string) ([]byte, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	body, err := c.doGet(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	var item ghContentItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, err
	}
	if item.Encoding == "base64" {
		return base64.StdEncoding.DecodeString(item.Content)
	}
	return []byte(item.Content), nil
}

func (c *GitHubClient) fetchTreeSHA(ctx context.Context, owner, repo, path string) (string, error) {
	// Use the Trees API to get the SHA for a path.
	// First get the default branch SHA, then walk to the path.
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", owner, repo, path)
	body, err := c.doGet(ctx, apiURL)
	if err != nil {
		return "", err
	}

	// For a directory, the response doesn't directly give us the tree SHA.
	// We'll use the git/trees endpoint instead.
	// Fetch the repo's default branch ref.
	refURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/heads/main", owner, repo)
	refBody, err := c.doGet(ctx, refURL)
	if err != nil {
		// Try master.
		refURL = fmt.Sprintf("https://api.github.com/repos/%s/%s/git/ref/heads/master", owner, repo)
		refBody, err = c.doGet(ctx, refURL)
		if err != nil {
			return "", err
		}
	}
	_ = body

	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(refBody, &ref); err != nil {
		return "", err
	}

	// Get the tree recursively and find our path.
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1",
		owner, repo, ref.Object.SHA)
	treeBody, err := c.doGet(ctx, treeURL)
	if err != nil {
		return "", err
	}

	var tree struct {
		SHA  string `json:"sha"`
		Tree []struct {
			Path string `json:"path"`
			SHA  string `json:"sha"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	if err := json.Unmarshal(treeBody, &tree); err != nil {
		return "", err
	}

	for _, t := range tree.Tree {
		if t.Path == path && t.Type == "tree" {
			return t.SHA, nil
		}
	}

	// Fallback: hash all file SHAs in the path.
	return ref.Object.SHA, nil
}

func (c *GitHubClient) doGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found: %s", url)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("rate limited or forbidden: %s", url)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d for %s", resp.StatusCode, url)
	}

	// Read body with size limit (10MB).
	limited := http.MaxBytesReader(nil, resp.Body, 10<<20)
	var buf []byte
	buf, err = readAll(limited)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	return buf, nil
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			if err.Error() == "EOF" || strings.Contains(err.Error(), "EOF") {
				break
			}
			return buf, err
		}
	}
	return buf, nil
}

type ghContentItem struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA      string `json:"sha"`
	Size     int    `json:"size"`
	Type     string `json:"type"` // "file" or "dir"
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}
