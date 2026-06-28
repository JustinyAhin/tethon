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

func TestManagerAdoptRejectsPathOutsideRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "worktrees")
	mgr := NewManager(root)
	outside := Worktree{ID: "out", Path: filepath.Join(t.TempDir(), "elsewhere"), Branch: "tethon/out"}
	if err := mgr.Adopt(outside, "/some/repo"); err != ErrNotOwned {
		t.Fatalf("expected ErrNotOwned, got %v", err)
	}
	if _, ok := mgr.Get(outside.ID); ok {
		t.Fatal("out-of-root worktree must not be tracked")
	}
	// A path inside the root is accepted.
	inside := Worktree{ID: "in", Path: filepath.Join(root, "r", "ses"), Branch: "tethon/in"}
	if err := mgr.Adopt(inside, "/some/repo"); err != nil {
		t.Fatalf("adopt inside root: %v", err)
	}
	if _, ok := mgr.Get(inside.ID); !ok {
		t.Fatal("in-root worktree should be tracked")
	}
}

func TestManagerWithinRoot(t *testing.T) {
	mgr := NewManager("/var/lib/tethon/worktrees")
	cases := map[string]bool{
		"/var/lib/tethon/worktrees/repo/ses":  true,
		"/var/lib/tethon/worktrees":           true,
		"/var/lib/tethon/other/ses":           false,
		"/etc/passwd":                         false,
		"/var/lib/tethon/worktrees/../escape": false,
	}
	for path, want := range cases {
		if got := mgr.withinRoot(path); got != want {
			t.Errorf("withinRoot(%q) = %v, want %v", path, got, want)
		}
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

func TestManagerRepeatedCreateRemoveLeavesNoResidue(t *testing.T) {
	ctx := context.Background()
	repo := initRepo(t)
	root := filepath.Join(t.TempDir(), "worktrees")
	mgr := NewManager(root)

	for i := 0; i < 5; i++ {
		id := "ses_" + string(rune('a'+i))
		if _, err := mgr.Create(ctx, CreateOptions{ID: id, RepoID: "r", RepoPath: repo}); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := mgr.Remove(ctx, id); err != nil {
			t.Fatalf("remove %s: %v", id, err)
		}
	}
	if len(mgr.List()) != 0 {
		t.Fatalf("expected no tracked worktrees, got %d", len(mgr.List()))
	}
	// The per-repo dir may remain but must hold no leftover session worktrees.
	entries, err := os.ReadDir(filepath.Join(root, "r"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no residual worktrees under root, got %v", entries)
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
	if err := fresh.Adopt(created, repo); err != nil {
		t.Fatalf("adopt: %v", err)
	}
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
