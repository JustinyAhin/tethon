// Package worktree manages per-session git worktrees so that concurrent agents
// on the same repository run in isolation: each session gets its own work-tree
// directory and branch, and removing one never touches another.
package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNotFound is returned when an operation references an unknown worktree.
var ErrNotFound = errors.New("worktree not found")

// ErrWorktreeExists is returned when an id is already tracked.
var ErrWorktreeExists = errors.New("worktree already exists")

// ErrBranchExists is returned when the requested branch is already present in
// the repository (so it cannot be created for a new session).
var ErrBranchExists = errors.New("branch already exists")

// Worktree describes a single checked-out work-tree owned by a session.
type Worktree struct {
	ID      string // owning session id
	RepoID  string
	Path    string // absolute path of the work-tree directory
	Branch  string // branch created for the session
	BaseRef string // commit-ish the work-tree was created from ("" = repo HEAD)
}

// Manager creates and tears down worktrees under a single root directory.
type Manager struct {
	root string
	mu   sync.Mutex
	live map[string]record
}

type record struct {
	wt       Worktree
	repoPath string
}

// CreateOptions parameterizes a new worktree.
type CreateOptions struct {
	ID       string // unique owner id (the session id)
	RepoID   string
	RepoPath string // git work-tree root the new worktree branches from
	BaseRef  string // optional commit-ish; defaults to the repo's HEAD
	Branch   string // optional branch name; defaults to tethon/<ID>
}

// NewManager returns a Manager that places worktrees under root.
func NewManager(root string) *Manager {
	return &Manager{root: root, live: make(map[string]record)}
}

// Create adds a git worktree on a fresh branch and tracks it.
func (m *Manager) Create(ctx context.Context, opts CreateOptions) (Worktree, error) {
	if opts.ID == "" || opts.RepoPath == "" {
		return Worktree{}, errors.New("worktree id and repo path are required")
	}
	m.mu.Lock()
	_, exists := m.live[opts.ID]
	m.mu.Unlock()
	if exists {
		return Worktree{}, fmt.Errorf("%q: %w", opts.ID, ErrWorktreeExists)
	}

	branch := opts.Branch
	if branch == "" {
		branch = "tethon/" + opts.ID
	}
	// A session always branches onto a fresh ref. Reject an explicit branch that
	// already exists rather than letting `git worktree add -b` fail opaquely —
	// this is what enforces one session per branch/worktree.
	if branchExists(ctx, opts.RepoPath, branch) {
		return Worktree{}, fmt.Errorf("%q: %w", branch, ErrBranchExists)
	}
	dir := filepath.Join(m.root, opts.RepoID, opts.ID)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return Worktree{}, err
	}

	args := []string{"-C", opts.RepoPath, "worktree", "add", "-b", branch, dir}
	if opts.BaseRef != "" {
		args = append(args, opts.BaseRef)
	}
	if err := runGit(ctx, args...); err != nil {
		return Worktree{}, err
	}

	wt := Worktree{ID: opts.ID, RepoID: opts.RepoID, Path: dir, Branch: branch, BaseRef: opts.BaseRef}
	m.mu.Lock()
	m.live[opts.ID] = record{wt: wt, repoPath: opts.RepoPath}
	m.mu.Unlock()
	return wt, nil
}

// Remove deletes the worktree and its branch. Branch deletion is best-effort so
// a partially-removed worktree still gets cleaned up.
func (m *Manager) Remove(ctx context.Context, id string) error {
	m.mu.Lock()
	rec, ok := m.live[id]
	if ok {
		delete(m.live, id)
	}
	m.mu.Unlock()
	if !ok {
		return ErrNotFound
	}
	if err := runGit(ctx, "-C", rec.repoPath, "worktree", "remove", "--force", rec.wt.Path); err != nil {
		return err
	}
	_ = runGit(ctx, "-C", rec.repoPath, "branch", "-D", rec.wt.Branch)
	return nil
}

// Adopt records a worktree the daemon created in a previous run (recovered from
// the session log) so it is tracked and can be cleaned up later. Adopting only
// DB-recorded worktrees keeps removal limited to worktrees the daemon owns.
func (m *Manager) Adopt(wt Worktree, repoPath string) {
	m.mu.Lock()
	m.live[wt.ID] = record{wt: wt, repoPath: repoPath}
	m.mu.Unlock()
}

// Get returns the tracked worktree for an owner id.
func (m *Manager) Get(id string) (Worktree, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.live[id]
	return rec.wt, ok
}

// List returns the currently tracked worktrees.
func (m *Manager) List() []Worktree {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Worktree, 0, len(m.live))
	for _, rec := range m.live {
		out = append(out, rec.wt)
	}
	return out
}

// branchExists reports whether refs/heads/<branch> is present in the repo.
func branchExists(ctx context.Context, repoPath, branch string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}

func runGit(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
