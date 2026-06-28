package main

import (
	"testing"

	"tethon/internal/repo"
)

func TestRepoInfos(t *testing.T) {
	registry, err := repo.NewRegistry(
		repo.Repo{ID: "alpha", Name: "alpha", Path: "/repos/alpha", DefaultBranch: "main"},
		repo.Repo{ID: "beta", Name: "beta", Path: "/repos/beta"},
	)
	if err != nil {
		t.Fatal(err)
	}
	infos := repoInfos(registry)
	if len(infos) != 2 {
		t.Fatalf("expected 2 repo infos, got %d", len(infos))
	}
	if infos[0].ID != "alpha" || infos[0].Path != "/repos/alpha" || infos[0].DefaultBranch != "main" {
		t.Fatalf("unexpected first repo info: %+v", infos[0])
	}
	if infos[1].ID != "beta" {
		t.Fatalf("unexpected second repo info: %+v", infos[1])
	}
}
