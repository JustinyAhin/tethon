package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/repo"
	"tethon/internal/worktree"
)

// Two sessions started against the same registered repo must each run in their
// own worktree directory — the heart of the Phase 3 exit check.
func TestCreateSessionRunsAgentInIsolatedWorktree(t *testing.T) {
	repoPath := gitRepo(t)
	repos, err := repo.Discover(context.Background(), []string{repoPath})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		t.Fatal(err)
	}
	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	mgr := worktree.NewManager(wtRoot)

	runner := &serverFakeCommandRunner{output: `{"type":"system","subtype":"init","session_id":"c1"}` + "\n" +
		`{"type":"result","is_error":false,"session_id":"c1"}` + "\n"}
	ts := newTestServerWithConfig(t, Config{
		Agents: agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{
			Binary: "claude",
			Runner: runner,
		})),
		Repos:     registry,
		Worktrees: mgr,
	})

	for i := 0; i < 2; i++ {
		var resp protocol.CreateSessionResponse
		postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
			RepoID: repos[0].ID,
			Agent:  "claude-cli",
			Prompt: "hi",
		}, &resp)
	}

	if len(runner.specs) != 2 {
		t.Fatalf("expected 2 agent runs, got %d", len(runner.specs))
	}
	dirA, dirB := runner.specs[0].Dir, runner.specs[1].Dir
	if dirA == dirB {
		t.Fatalf("both sessions ran in the same dir %q", dirA)
	}
	for _, dir := range []string{dirA, dirB} {
		if !within(dir, wtRoot) {
			t.Fatalf("agent dir %q is not under the worktree root %q", dir, wtRoot)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Fatalf("expected a git worktree at %q: %v", dir, err)
		}
	}
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !filepath.IsAbs(rel) && rel[0] != '.'
}

func gitRepo(t *testing.T) string {
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
