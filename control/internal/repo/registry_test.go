package repo

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNewRegistryLookups(t *testing.T) {
	reg, err := NewRegistry(
		Repo{ID: "app", Name: "app", Path: "/repos/app"},
		Repo{ID: "lib", Name: "lib", Path: "/repos/lib"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := reg.Get("app"); !ok || got.Path != "/repos/app" {
		t.Fatalf("Get(app) = %+v, %v", got, ok)
	}
	if _, ok := reg.Get("missing"); ok {
		t.Fatal("expected missing id to not be found")
	}
	// Find resolves a subdirectory to its owning repo.
	if got, ok := reg.Find("/repos/app/internal/x"); !ok || got.ID != "app" {
		t.Fatalf("Find(subdir) = %+v, %v", got, ok)
	}
	if _, ok := reg.Find("/elsewhere"); ok {
		t.Fatal("expected unrelated path to not resolve")
	}
}

func TestNewRegistryRejectsDuplicateID(t *testing.T) {
	_, err := NewRegistry(
		Repo{ID: "dup", Path: "/a"},
		Repo{ID: "dup", Path: "/b"},
	)
	if err == nil {
		t.Fatal("expected duplicate id error")
	}
}

func TestDiscover(t *testing.T) {
	root := t.TempDir()
	gitInit(t, root)

	repos, err := Discover(context.Background(), []string{root, filepath.Join(root, ".")})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected duplicate paths to collapse to one repo, got %d", len(repos))
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Path != want {
		t.Fatalf("repo path = %q, want %q", repos[0].Path, want)
	}
	if repos[0].ID == "" || repos[0].Name == "" {
		t.Fatalf("expected id and name to be set, got %+v", repos[0])
	}
}

func TestDiscoverRejectsNonGitRoot(t *testing.T) {
	_, err := Discover(context.Background(), []string{t.TempDir()})
	if err == nil {
		t.Fatal("expected non-git root to be rejected")
	}
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
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
}
