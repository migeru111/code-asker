// Package safepath resolves user-supplied paths strictly inside a root directory.
package safepath

import (
	"errors"
	"path/filepath"
	"strings"
)

var (
	// ErrOutside is returned when a path would escape the root.
	ErrOutside = errors.New("path is outside the allowed root")
	// ErrInvalid is returned for malformed input.
	ErrInvalid = errors.New("invalid path")
)

// Root is a canonical (symlink-resolved) directory that bounds all lookups.
type Root struct {
	dir string
}

// New canonicalizes dir and returns a Root.
func New(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	return &Root{dir: real}, nil
}

// Dir returns the canonical root directory.
func (r *Root) Dir() string { return r.dir }

// Resolve joins the slash-separated relative path rel under the root,
// follows symlinks, and verifies the result is still inside the root.
// Any ".git" component is refused. The target must exist.
func (r *Root) Resolve(rel string) (string, error) {
	if strings.ContainsRune(rel, 0) || strings.ContainsRune(rel, '\\') {
		return "", ErrInvalid
	}
	if strings.HasPrefix(rel, "/") {
		return "", ErrInvalid
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || part == ".git" {
			return "", ErrOutside
		}
	}
	full := filepath.Join(r.dir, filepath.FromSlash(rel))
	real, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if !r.contains(real) {
		return "", ErrOutside
	}
	return real, nil
}

func (r *Root) contains(p string) bool {
	if p == r.dir {
		return true
	}
	return strings.HasPrefix(p, r.dir+string(filepath.Separator))
}

// Rel returns the slash-separated path of p relative to the root.
func (r *Root) Rel(p string) (string, error) {
	if !r.contains(p) {
		return "", ErrOutside
	}
	rel, err := filepath.Rel(r.dir, p)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}
