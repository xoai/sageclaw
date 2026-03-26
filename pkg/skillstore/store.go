package skillstore

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// InstalledSkill represents a skill that has been installed locally.
type InstalledSkill struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source"`
	SourceType  string   `json:"sourceType"`
	InstalledAt string   `json:"installedAt"`
	UpdatedAt   string   `json:"updatedAt"`
	HasScripts  bool     `json:"hasScripts"`
	Files       []string `json:"files,omitempty"`
	Agents      []string `json:"agents,omitempty"`  // populated by caller
}

// SkillPreview is the pre-install consent view of a skill.
type SkillPreview struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Source      string      `json:"source"`
	SkillMD     string      `json:"skillMd"`
	Files       []FileEntry `json:"files"`
	HasScripts  bool        `json:"hasScripts"`
	Scripts     []ScriptFile `json:"scripts,omitempty"`
}

// ScriptFile represents a shell script in a skill, shown for consent review.
type ScriptFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// UpdateInfo describes an available update for an installed skill.
type UpdateInfo struct {
	Name       string `json:"name"`
	Source     string `json:"source"`
	CurrentSHA string `json:"currentSha"`
	LatestSHA  string `json:"latestSha"`
}

// Option configures the Store.
type Option func(*Store)

// WithSearchURL sets the skills.sh API base URL.
func WithSearchURL(url string) Option {
	return func(s *Store) { s.search = NewSearchClient(url, nil) }
}

// WithGitHubToken sets the GitHub API token.
func WithGitHubToken(token string) Option {
	return func(s *Store) { s.github = NewGitHubClient(token, nil) }
}

// Store orchestrates skill search, install, and management.
type Store struct {
	installDir string
	lockFile   *LockFile
	search     *SearchClient
	github     *GitHubClient
}

// NewStore creates a skill store.
func NewStore(installDir, lockPath string, opts ...Option) (*Store, error) {
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		return nil, fmt.Errorf("create install dir: %w", err)
	}

	lf, err := LoadLockFile(lockPath)
	if err != nil {
		return nil, fmt.Errorf("load lock file: %w", err)
	}

	s := &Store{
		installDir: installDir,
		lockFile:   lf,
		search:     NewSearchClient("", nil),
		github:     NewGitHubClient("", nil),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s, nil
}

// Search queries the skills.sh marketplace.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	return s.search.Search(ctx, query, limit)
}

// Preview fetches skill info for consent review without installing.
func (s *Store) Preview(ctx context.Context, source string) (*SkillPreview, error) {
	owner, repo, skill, err := ParseSource(source)
	if err != nil {
		return nil, err
	}

	manifest, err := s.github.Preview(ctx, owner, repo, skill)
	if err != nil {
		return nil, err
	}

	preview := &SkillPreview{
		Name:       manifest.Name,
		Source:     fmt.Sprintf("%s/%s", owner, repo),
		SkillMD:    manifest.SkillMD,
		Files:      manifest.Files,
		HasScripts: manifest.HasScripts,
	}

	// Parse description from SKILL.md frontmatter.
	if manifest.SkillMD != "" {
		preview.Description = parseDescription(manifest.SkillMD)
	}

	// Fetch script contents for review.
	if manifest.HasScripts {
		for _, f := range manifest.Files {
			if strings.HasSuffix(f.Path, ".sh") || strings.HasSuffix(f.Path, ".py") || strings.HasSuffix(f.Path, ".js") {
				content, err := s.github.fetchFileContent(ctx, owner, repo, f.Path)
				if err == nil {
					preview.Scripts = append(preview.Scripts, ScriptFile{
						Name:    filepath.Base(f.Path),
						Content: string(content),
					})
				}
			}
		}
	}

	return preview, nil
}

// Install downloads and installs a skill.
func (s *Store) Install(ctx context.Context, source string) (*InstalledSkill, error) {
	owner, repo, skill, err := ParseSource(source)
	if err != nil {
		return nil, err
	}

	// Check if already installed.
	if entry := s.lockFile.Get(skill); entry != nil {
		return nil, fmt.Errorf("skill %q is already installed (from %s)", skill, entry.Source)
	}

	destDir := filepath.Join(s.installDir, skill)
	manifest, err := s.github.Download(ctx, owner, repo, skill, destDir)
	if err != nil {
		// Clean up partial download.
		os.RemoveAll(destDir)
		return nil, fmt.Errorf("download skill: %w", err)
	}

	// Update lock file.
	s.lockFile.Add(skill, LockEntry{
		Source:     fmt.Sprintf("%s/%s", owner, repo),
		SourceType: "github",
		SkillID:    skill,
		TreeSHA:    manifest.TreeSHA,
		HasScripts: manifest.HasScripts,
	})
	if err := s.lockFile.Save(""); err != nil {
		return nil, fmt.Errorf("save lock file: %w", err)
	}

	return &InstalledSkill{
		Name:        skill,
		Description: parseDescription(manifest.SkillMD),
		Source:      fmt.Sprintf("%s/%s", owner, repo),
		SourceType:  "github",
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		HasScripts:  manifest.HasScripts,
	}, nil
}

// Uninstall removes a skill and cleans up.
func (s *Store) Uninstall(name string) error {
	entry := s.lockFile.Get(name)
	if entry == nil {
		return fmt.Errorf("skill %q is not installed", name)
	}

	// Remove files.
	skillDir := filepath.Join(s.installDir, name)
	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("remove skill directory: %w", err)
	}

	// Update lock file.
	s.lockFile.Remove(name)
	if err := s.lockFile.Save(""); err != nil {
		return fmt.Errorf("save lock file: %w", err)
	}

	return nil
}

// Update checks for and applies an update to an installed skill.
func (s *Store) Update(ctx context.Context, name string) (*InstalledSkill, error) {
	entry := s.lockFile.Get(name)
	if entry == nil {
		return nil, fmt.Errorf("skill %q is not installed", name)
	}
	if entry.SourceType != "github" {
		return nil, fmt.Errorf("skill %q was installed locally, cannot auto-update", name)
	}

	owner, repo, skill, err := ParseSource(entry.Source + "@" + entry.SkillID)
	if err != nil {
		return nil, err
	}

	// Remove old files and re-download.
	destDir := filepath.Join(s.installDir, name)
	if err := os.RemoveAll(destDir); err != nil {
		return nil, fmt.Errorf("remove old skill: %w", err)
	}

	manifest, err := s.github.Download(ctx, owner, repo, skill, destDir)
	if err != nil {
		return nil, fmt.Errorf("download update: %w", err)
	}

	// Update lock file with new SHA and timestamp.
	now := time.Now().UTC().Format(time.RFC3339)
	s.lockFile.Add(name, LockEntry{
		Source:      entry.Source,
		SourceType:  entry.SourceType,
		SkillID:     entry.SkillID,
		InstalledAt: entry.InstalledAt,
		UpdatedAt:   now,
		TreeSHA:     manifest.TreeSHA,
		HasScripts:  manifest.HasScripts,
	})
	if err := s.lockFile.Save(""); err != nil {
		return nil, fmt.Errorf("save lock file: %w", err)
	}

	return &InstalledSkill{
		Name:        name,
		Description: parseDescription(manifest.SkillMD),
		Source:      entry.Source,
		SourceType:  entry.SourceType,
		InstalledAt: entry.InstalledAt,
		UpdatedAt:   now,
		HasScripts:  manifest.HasScripts,
	}, nil
}

// CheckUpdates checks all GitHub-sourced skills for available updates.
func (s *Store) CheckUpdates(ctx context.Context) ([]UpdateInfo, error) {
	var updates []UpdateInfo

	for name, entry := range s.lockFile.Skills {
		if entry.SourceType != "github" || entry.TreeSHA == "" {
			continue
		}

		owner, repo, skill, err := ParseSource(entry.Source + "@" + entry.SkillID)
		if err != nil {
			continue
		}

		latestSHA, err := s.github.GetTreeSHA(ctx, owner, repo, skill)
		if err != nil {
			continue // Skip skills we can't check.
		}

		if latestSHA != entry.TreeSHA {
			updates = append(updates, UpdateInfo{
				Name:       name,
				Source:     entry.Source,
				CurrentSHA: entry.TreeSHA,
				LatestSHA:  latestSHA,
			})
		}
	}

	return updates, nil
}

// Installed returns all locally installed skills.
func (s *Store) Installed() []InstalledSkill {
	var skills []InstalledSkill

	for name, entry := range s.lockFile.Skills {
		sk := InstalledSkill{
			Name:        name,
			Source:      entry.Source,
			SourceType:  entry.SourceType,
			InstalledAt: entry.InstalledAt,
			UpdatedAt:   entry.UpdatedAt,
			HasScripts:  entry.HasScripts,
		}

		// Try to read description from SKILL.md on disk.
		skillMD := filepath.Join(s.installDir, name, "SKILL.md")
		if data, err := os.ReadFile(skillMD); err == nil {
			sk.Description = parseDescription(string(data))
		}

		// List files.
		filepath.WalkDir(filepath.Join(s.installDir, name), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, _ := filepath.Rel(filepath.Join(s.installDir, name), path)
			sk.Files = append(sk.Files, rel)
			return nil
		})

		skills = append(skills, sk)
	}

	return skills
}

// ImportLocal imports a skill from a local filesystem path.
func (s *Store) ImportLocal(srcPath, name string) (*InstalledSkill, error) {
	// Validate SKILL.md exists.
	skillMD := filepath.Join(srcPath, "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		return nil, fmt.Errorf("no SKILL.md found at %s", srcPath)
	}

	if name == "" {
		name = filepath.Base(srcPath)
	}

	if entry := s.lockFile.Get(name); entry != nil {
		return nil, fmt.Errorf("skill %q is already installed", name)
	}

	destDir := filepath.Join(s.installDir, name)
	if err := copyDir(srcPath, destDir); err != nil {
		return nil, fmt.Errorf("copy skill: %w", err)
	}

	hasScripts := false
	if _, err := os.Stat(filepath.Join(destDir, "tools")); err == nil {
		hasScripts = true
	}

	s.lockFile.Add(name, LockEntry{
		Source:     srcPath,
		SourceType: "local",
		SkillID:    name,
		HasScripts: hasScripts,
	})
	if err := s.lockFile.Save(""); err != nil {
		return nil, fmt.Errorf("save lock file: %w", err)
	}

	desc := ""
	if data, err := os.ReadFile(filepath.Join(destDir, "SKILL.md")); err == nil {
		desc = parseDescription(string(data))
	}

	return &InstalledSkill{
		Name:        name,
		Description: desc,
		Source:      srcPath,
		SourceType:  "local",
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		HasScripts:  hasScripts,
	}, nil
}

// --- helpers ---

func parseDescription(skillMD string) string {
	// Extract description from YAML frontmatter.
	if !strings.HasPrefix(skillMD, "---") {
		return ""
	}
	end := strings.Index(skillMD[3:], "---")
	if end < 0 {
		return ""
	}
	frontmatter := skillMD[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimPrefix(line, "description:")
			return strings.TrimSpace(strings.Trim(desc, `"'`))
		}
	}
	return ""
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, 0o644)
	})
}
