package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"tethon/internal/sessionlog"
	"tethon/internal/worktree"
)

func TestLegacyDeletePreservesFailedNativeWorkspace(t *testing.T) {
	ctx := context.Background()
	registry, repos := fixtureRepo(t)
	manager := worktree.NewManager(filepath.Join(t.TempDir(), "worktrees"))

	store, err := sessionlog.Open(ctx, filepath.Join(t.TempDir(), "sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	session, err := store.CreateSession(ctx, sessionlog.CreateSessionParams{
		RepoID: repos[0].ID,
		Repo:   repos[0].Path,
		Agent:  "codex-cli",
		Status: "error",
	})
	if err != nil {
		t.Fatal(err)
	}

	workspace, err := manager.Create(ctx, worktree.CreateOptions{
		ID:       session.ID,
		RepoID:   repos[0].ID,
		RepoPath: repos[0].Path,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionWorktree(ctx, session.ID, workspace.Path, workspace.Branch); err != nil {
		t.Fatal(err)
	}

	marker := filepath.Join(workspace.Path, "untracked-work.txt")
	if err := os.WriteFile(marker, []byte("Keep this work.\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	host := httptest.NewServer(NewWithConfig(store, Config{Repos: registry, Worktrees: manager}).Handler())
	defer host.Close()

	if status := deleteStatus(t, host.URL+"/sessions/"+session.ID); status != http.StatusConflict {
		t.Fatalf("legacy deletion accepted native cleanup: HTTP %d", status)
	}

	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "Keep this work.\n" {
		t.Fatalf("legacy deletion changed native work: %q, %v", content, err)
	}
	if _, err := store.GetSession(ctx, session.ID); err != nil {
		t.Fatalf("legacy deletion removed the native delivery record: %v", err)
	}
}
