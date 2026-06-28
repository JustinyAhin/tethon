package main

import (
	"path/filepath"
	"testing"
)

func TestNormalizeRepoRoots(t *testing.T) {
	root := t.TempDir()
	roots, err := normalizeRepoRoots([]string{root, filepath.Join(root, ".")})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("expected duplicate roots to collapse, got %v", roots)
	}
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0] != want {
		t.Fatalf("root = %q, want %q", roots[0], want)
	}
}

func TestNormalizeRepoRootsRejectsMissingRoot(t *testing.T) {
	_, err := normalizeRepoRoots([]string{filepath.Join(t.TempDir(), "missing")})
	if err == nil {
		t.Fatal("expected missing root error")
	}
}
