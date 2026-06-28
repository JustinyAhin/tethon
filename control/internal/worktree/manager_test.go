package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestManagerCreateIsolatesSessions(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(filepath.Join(t.TempDir(), "worktrees"))

	a, err := mgr.Create(ctx, CreateOptions{ID: "ses_a", RepoID: "r", RepoPath: repo})
	if err != nil {
		t.Fatalf("create a: %v", err)
	}
	b, err := mgr.Create(ctx, CreateOptions{ID: "ses_b", RepoID: "r", RepoPath: repo})
	if err != nil {
		t.Fatalf("create b: %v", err)
	}

	if a.Path == b.Path {
		t.Fatal("worktrees share a path")
	}
	if a.Branch == b.Branch {
		t.Fatalf("worktrees share a branch %q", a.Branch)
	}
	for _, wt := range []Worktree{a, b} {
		if _, err := os.Stat(wt.Path); err != nil {
			t.Fatalf("worktree %s missing: %v", wt.ID, err)
		}
	}

	// A change on one worktree's branch must not appear on the other.
	writeFile(t, filepath.Join(a.Path, "only_a.txt"), "a")
	if _, err := os.Stat(filepath.Join(b.Path, "only_a.txt")); !os.IsNotExist(err) {
		t.Fatalf("change leaked into sibling worktree: %v", err)
	}

	// Removing one leaves the other intact.
	if err := mgr.Remove(ctx, "ses_a"); err != nil {
		t.Fatalf("remove a: %v", err)
	}
	if _, err := os.Stat(a.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree a not removed: %v", err)
	}
	if _, ok := mgr.Get("ses_b"); !ok {
		t.Fatal("worktree b should still be tracked")
	}
	if _, err := os.Stat(b.Path); err != nil {
		t.Fatalf("worktree b should survive removal of a: %v", err)
	}
}

func TestManagerRemoveUnknown(t *testing.T) {
	if err := NewManager(t.TempDir()).Remove(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestManagerCreateRejectsDuplicateID(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	mgr := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	if _, err := mgr.Create(ctx, CreateOptions{ID: "ses", RepoID: "r", RepoPath: repo}); err != nil {
		t.Fatal(err)
	}
	_, err := mgr.Create(ctx, CreateOptions{ID: "ses", RepoID: "r", RepoPath: repo})
	if !errors.Is(err, ErrWorktreeExists) {
		t.Fatalf("expected ErrWorktreeExists, got %v", err)
	}
}

func TestManagerCreateRejectsExistingBranch(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	if out, err := exec.Command("git", "-C", repo, "branch", "feature").CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	mgr := NewManager(filepath.Join(t.TempDir(), "worktrees"))
	_, err := mgr.Create(ctx, CreateOptions{ID: "ses", RepoID: "r", RepoPath: repo, Branch: "feature"})
	if !errors.Is(err, ErrBranchExists) {
		t.Fatalf("expected ErrBranchExists, got %v", err)
	}
}

func TestManagerAdoptEnablesRemoval(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	root := filepath.Join(t.TempDir(), "worktrees")

	// Create a worktree, then simulate a daemon restart with a fresh manager
	// that only knows what the session log recorded.
	created, err := NewManager(root).Create(ctx, CreateOptions{ID: "ses", RepoID: "r", RepoPath: repo})
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewManager(root)
	if _, ok := fresh.Get("ses"); ok {
		t.Fatal("fresh manager should not know the worktree before adoption")
	}
	fresh.Adopt(created, repo)
	if _, ok := fresh.Get("ses"); !ok {
		t.Fatal("worktree should be tracked after adoption")
	}
	if err := fresh.Remove(ctx, "ses"); err != nil {
		t.Fatalf("remove adopted worktree: %v", err)
	}
	if _, err := os.Stat(created.Path); !os.IsNotExist(err) {
		t.Fatalf("adopted worktree not removed: %v", err)
	}
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
		{"commit", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
