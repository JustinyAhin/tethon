package repo

import (
	"context"
	"testing"
)

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
