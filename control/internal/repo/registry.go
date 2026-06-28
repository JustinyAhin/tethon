// Package repo is the host daemon's registry of git repositories it will run
// agents against: stable ids, display names, and resolved base paths. It is
// host-owned and knows nothing about HTTP or sessions.
package repo

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Repo is a single registered repository.
type Repo struct {
	ID            string
	Name          string
	Path          string // absolute, symlink-resolved git work-tree root
	DefaultBranch string
}

// Registry holds the repos a host advertises, keyed by stable id.
type Registry struct {
	byID   map[string]Repo
	byPath map[string]string
	order  []string
}

// NewRegistry builds a registry from already-resolved repos. It rejects empty
// or duplicate ids so lookups stay unambiguous.
func NewRegistry(repos ...Repo) (*Registry, error) {
	r := &Registry{
		byID:   make(map[string]Repo, len(repos)),
		byPath: make(map[string]string, len(repos)),
	}
	for _, repo := range repos {
		if repo.ID == "" || repo.Path == "" {
			return nil, errors.New("repo id and path are required")
		}
		if _, ok := r.byID[repo.ID]; ok {
			return nil, fmt.Errorf("duplicate repo id %q", repo.ID)
		}
		if repo.Name == "" {
			repo.Name = repo.ID
		}
		r.byID[repo.ID] = repo
		r.byPath[repo.Path] = repo.ID
		r.order = append(r.order, repo.ID)
	}
	return r, nil
}

// Get returns the repo with the given id.
func (r *Registry) Get(id string) (Repo, bool) {
	if r == nil {
		return Repo{}, false
	}
	repo, ok := r.byID[id]
	return repo, ok
}

// Find resolves a filesystem path to its owning repo: the registered repo whose
// path equals or is an ancestor of the given (already symlink-resolved) path.
// It supports clients that still address a repo by raw path.
func (r *Registry) Find(path string) (Repo, bool) {
	if r == nil {
		return Repo{}, false
	}
	clean := filepath.Clean(path)
	for _, id := range r.order {
		repo := r.byID[id]
		if clean == repo.Path || pathWithin(clean, repo.Path) {
			return repo, true
		}
	}
	return Repo{}, false
}

// List returns the registered repos in registration order.
func (r *Registry) List() []Repo {
	if r == nil {
		return nil
	}
	repos := make([]Repo, 0, len(r.order))
	for _, id := range r.order {
		repos = append(repos, r.byID[id])
	}
	return repos
}

// Paths returns the registered repo paths in registration order.
func (r *Registry) Paths() []string {
	if r == nil {
		return nil
	}
	paths := make([]string, 0, len(r.order))
	for _, id := range r.order {
		paths = append(paths, r.byID[id].Path)
	}
	return paths
}

// Discover resolves each input path to a git work-tree root and returns the
// corresponding repos. A path that is not inside a git repository is an error:
// worktree isolation requires git. Paths that resolve to the same repo are
// deduplicated. Ids are derived from the repo name and stay stable across runs.
func Discover(ctx context.Context, paths []string) ([]Repo, error) {
	repos := make([]Repo, 0, len(paths))
	usedIDs := make(map[string]struct{}, len(paths))
	seenRoots := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return nil, fmt.Errorf("repo root %q: %w", p, err)
		}
		info, err := os.Stat(real)
		if err != nil {
			return nil, fmt.Errorf("repo root %q: %w", p, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("repo root %q is not a directory", p)
		}
		top, err := gitOutput(ctx, real, "rev-parse", "--show-toplevel")
		if err != nil {
			return nil, fmt.Errorf("repo root %q is not a git repository", p)
		}
		root, err := filepath.EvalSymlinks(filepath.Clean(top))
		if err != nil {
			return nil, fmt.Errorf("repo root %q: %w", p, err)
		}
		if _, ok := seenRoots[root]; ok {
			continue
		}
		seenRoots[root] = struct{}{}

		name := filepath.Base(root)
		id := slug(name)
		if _, ok := usedIDs[id]; ok {
			id = id + "-" + shortHash(root)
		}
		usedIDs[id] = struct{}{}

		branch, _ := gitOutput(ctx, root, "symbolic-ref", "--short", "HEAD")
		repos = append(repos, Repo{
			ID:            id,
			Name:          name,
			Path:          root,
			DefaultBranch: strings.TrimSpace(branch),
		})
	}
	return repos, nil
}

func gitOutput(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("%s", msg)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(s)
	s = nonSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "repo"
	}
	return s
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:6]
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}
