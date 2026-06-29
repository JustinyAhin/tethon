package repo

import (
	"context"
	"os/exec"
	"testing"
)

func TestListRefs(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)
	gitRun(t, dir, "branch", "feature/x")
	gitRun(t, dir, "tag", "v1.0.0")

	repos, err := Discover(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	r := repos[0]

	branches, tags, err := ListRefs(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) == 0 || branches[0] != r.DefaultBranch {
		t.Fatalf("expected default branch %q first, got %v", r.DefaultBranch, branches)
	}
	if !contains(branches, "feature/x") {
		t.Fatalf("expected feature/x in branches, got %v", branches)
	}
	if !contains(tags, "v1.0.0") {
		t.Fatalf("expected v1.0.0 in tags, got %v", tags)
	}
}

func TestListRefsEmptyTagsAreNonNil(t *testing.T) {
	dir := t.TempDir()
	gitInit(t, dir)

	repos, err := Discover(context.Background(), []string{dir})
	if err != nil {
		t.Fatal(err)
	}

	_, tags, err := ListRefs(context.Background(), repos[0])
	if err != nil {
		t.Fatal(err)
	}
	if tags == nil {
		t.Fatal("expected non-nil tags slice for a repo with no tags")
	}
	if len(tags) != 0 {
		t.Fatalf("expected no tags, got %v", tags)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
