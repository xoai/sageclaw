package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrOutsideWorkspace = errors.New("path outside workspace")
	ErrSymlinkEscape    = errors.New("symlink resolves outside workspace")
)

// Sandbox enforces workspace path boundaries for file operations.
type Sandbox struct {
	root string // Absolute, cleaned workspace root.
}

// NewSandbox creates a sandbox rooted at the given directory.
func NewSandbox(root string) (*Sandbox, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	// Ensure root exists and is a directory.
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, errors.New("workspace root must be a directory")
	}
	return &Sandbox{root: abs}, nil
}

// Root returns the workspace root path.
func (s *Sandbox) Root() string {
	return s.root
}

// Resolve resolves a path relative to the workspace root and validates
// that it stays within bounds. Returns the absolute resolved path.
func (s *Sandbox) Resolve(path string) (string, error) {
	// If path is relative, join with root.
	var abs string
	if filepath.IsAbs(path) {
		abs = filepath.Clean(path)
	} else {
		abs = filepath.Join(s.root, filepath.Clean(path))
	}

	// Ensure resolved path is under workspace root.
	if !isUnder(abs, s.root) {
		return "", ErrOutsideWorkspace
	}

	// Check symlink traversal — resolve symlinks and re-check.
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// If the file doesn't exist yet (write operation), check the parent.
		dir := filepath.Dir(abs)
		realDir, dirErr := filepath.EvalSymlinks(dir)
		if dirErr != nil {
			// Parent also doesn't exist; only allow if path is still under root.
			return abs, nil
		}
		if !isUnder(realDir, s.root) {
			return "", ErrSymlinkEscape
		}
		return abs, nil
	}

	if !isUnder(real, s.root) {
		return "", ErrSymlinkEscape
	}

	return abs, nil
}

func isUnder(path, root string) bool {
	// Add separator to prevent prefix matching like /workspace-ext matching /workspace.
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}
