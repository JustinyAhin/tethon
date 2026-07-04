package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestDiscoverWorkspaceParent(t *testing.T) {
	workspace := t.TempDir()
	app := filepath.Join(workspace, "app")
	lib := filepath.Join(workspace, "lib")
	nonRepo := filepath.Join(workspace, "notes")
	for _, dir := range []string{app, lib, nonRepo} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "README.txt"), []byte("not a repo"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInit(t, app)
	gitInit(t, lib)

	repos, err := Discover(context.Background(), []string{workspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 2 {
		t.Fatalf("expected 2 repos, got %d: %+v", len(repos), repos)
	}
	if repos[0].Name != "app" || repos[1].Name != "lib" {
		t.Fatalf("repo order/names = %+v, want app then lib", repos)
	}
	wantApp, err := filepath.EvalSymlinks(app)
	if err != nil {
		t.Fatal(err)
	}
	if repos[0].Path != wantApp {
		t.Fatalf("first repo path = %q, want %q", repos[0].Path, wantApp)
	}
}

func TestDiscoverWorkspaceParentOnlyUsesImmediateChildren(t *testing.T) {
	workspace := t.TempDir()
	app := filepath.Join(workspace, "app")
	nested := filepath.Join(workspace, "group", "nested")
	for _, dir := range []string{app, nested} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	gitInit(t, app)
	gitInit(t, nested)

	repos, err := Discover(context.Background(), []string{workspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Name != "app" {
		t.Fatalf("repos = %+v, want only direct child app", repos)
	}
}

func TestDiscoverDeduplicatesParentAndExplicitChild(t *testing.T) {
	workspace := t.TempDir()
	app := filepath.Join(workspace, "app")
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	gitInit(t, app)

	repos, err := Discover(context.Background(), []string{workspace, app})
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Name != "app" {
		t.Fatalf("repos = %+v, want one app repo", repos)
	}
}

func TestDiscoverRejectsWorkspaceWithoutRepos(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := Discover(context.Background(), []string{workspace})
	if err == nil {
		t.Fatal("expected workspace without repos to be rejected")
	}
	if !strings.Contains(err.Error(), "contains no git repositories") {
		t.Fatalf("error = %q, want clear no-repos message", err)
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
