package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/repo"
	"tethon/internal/sessionlog"
	"tethon/internal/worktree"
)

// fixtureRepo discovers a one-commit git repo and returns it as a registry.
func fixtureRepo(t *testing.T) (*repo.Registry, []repo.Repo) {
	t.Helper()
	repos, err := repo.Discover(context.Background(), []string{gitRepo(t)})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		t.Fatal(err)
	}
	return registry, repos
}

func claudeRunnerResult(isError bool) *serverFakeCommandRunner {
	result := `{"type":"result","is_error":false,"session_id":"c1"}`
	if isError {
		result = `{"type":"result","is_error":true,"session_id":"c1","result":"boom"}`
	}
	return &serverFakeCommandRunner{output: `{"type":"system","subtype":"init","session_id":"c1"}` + "\n" + result + "\n"}
}

// TestExplicitBranchCollisionReturnsConflict covers zqe.4: a second session that
// requests a branch already checked out by another session is rejected with 409.
func TestExplicitBranchCollisionReturnsConflict(t *testing.T) {
	registry, repos := fixtureRepo(t)
	mgr := worktree.NewManager(filepath.Join(t.TempDir(), "worktrees"))
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{Binary: "claude", Runner: claudeRunnerResult(false)})),
		Repos:     registry,
		Worktrees: mgr,
	})

	var first protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
		RepoID: repos[0].ID, Branch: "shared", Agent: "claude-cli", Prompt: "hi",
	}, &first)

	status := postJSONStatus(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
		RepoID: repos[0].ID, Branch: "shared", Agent: "claude-cli", Prompt: "hi",
	})
	if status != http.StatusConflict {
		t.Fatalf("expected 409 for reused branch, got %d", status)
	}
}

// TestDoneRemovesWorktree covers zqe.5: a session that finishes normally has its
// worktree and branch reclaimed.
func TestDoneRemovesWorktree(t *testing.T) {
	registry, repos := fixtureRepo(t)
	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	mgr := worktree.NewManager(wtRoot)
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewFake()),
		Repos:     registry,
		Worktrees: mgr,
	})

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{RepoID: repos[0].ID, Agent: "fake"}, &session)
	wt, ok := mgr.Get(session.ID)
	if !ok {
		t.Fatal("expected a worktree to be tracked after create")
	}

	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "done"})

	if _, ok := mgr.Get(session.ID); ok {
		t.Fatal("worktree should be untracked after done")
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be removed after done: %v", err)
	}
}

// TestErrorRetainsWorktree covers zqe.5: a session that fails asynchronously
// keeps its worktree for inspection and is removed from the running set.
func TestErrorRetainsWorktree(t *testing.T) {
	registry, repos := fixtureRepo(t)
	mgr := worktree.NewManager(filepath.Join(t.TempDir(), "worktrees"))
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{Binary: "claude", Runner: claudeRunnerResult(true)})),
		Repos:     registry,
		Worktrees: mgr,
	})

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{RepoID: repos[0].ID, Agent: "claude-cli", Prompt: "hi"}, &session)

	// Wait for the async error to land by reading the stream to the error event.
	events := streamEvents(t, ts.URL, session.ID, 0, 2)
	if events[len(events)-1].Type != protocol.EventError {
		t.Fatalf("expected terminal error event, got %q", events[len(events)-1].Type)
	}

	wt, ok := mgr.Get(session.ID)
	if !ok {
		t.Fatal("failed session worktree should be retained (still tracked)")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("failed session worktree should remain on disk: %v", err)
	}
	if status := postJSONStatus(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "more"}); status != http.StatusConflict {
		t.Fatalf("expected 409 after terminal error, got %d", status)
	}
}

func TestDeleteErrorSessionRemovesRetainedWorktree(t *testing.T) {
	registry, repos := fixtureRepo(t)
	mgr := worktree.NewManager(filepath.Join(t.TempDir(), "worktrees"))
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{Binary: "claude", Runner: claudeRunnerResult(true)})),
		Repos:     registry,
		Worktrees: mgr,
	})

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{RepoID: repos[0].ID, Agent: "claude-cli", Prompt: "hi"}, &session)
	events := streamEvents(t, ts.URL, session.ID, 0, 2)
	if events[len(events)-1].Type != protocol.EventError {
		t.Fatalf("expected terminal error event, got %q", events[len(events)-1].Type)
	}

	wt, ok := mgr.Get(session.ID)
	if !ok {
		t.Fatal("failed session worktree should be retained before delete")
	}
	if status := deleteStatus(t, ts.URL+"/sessions/"+session.ID); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	if _, ok := mgr.Get(session.ID); ok {
		t.Fatal("worktree should be untracked after delete")
	}
	if _, err := os.Stat(wt.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir should be removed after delete: %v", err)
	}
	if gitBranchExists(t, repos[0].Path, wt.Branch) {
		t.Fatalf("worktree branch %q should be deleted", wt.Branch)
	}
}

func TestDeleteErrorSessionToleratesMissingWorktreeDir(t *testing.T) {
	registry, repos := fixtureRepo(t)
	mgr := worktree.NewManager(filepath.Join(t.TempDir(), "worktrees"))
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{Binary: "claude", Runner: claudeRunnerResult(true)})),
		Repos:     registry,
		Worktrees: mgr,
	})

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{RepoID: repos[0].ID, Agent: "claude-cli", Prompt: "hi"}, &session)
	events := streamEvents(t, ts.URL, session.ID, 0, 2)
	if events[len(events)-1].Type != protocol.EventError {
		t.Fatalf("expected terminal error event, got %q", events[len(events)-1].Type)
	}

	wt, ok := mgr.Get(session.ID)
	if !ok {
		t.Fatal("failed session worktree should be retained before delete")
	}
	if err := os.RemoveAll(wt.Path); err != nil {
		t.Fatal(err)
	}
	if status := deleteStatus(t, ts.URL+"/sessions/"+session.ID); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}
	if gitBranchExists(t, repos[0].Path, wt.Branch) {
		t.Fatalf("worktree branch %q should be deleted", wt.Branch)
	}
}

// TestRecoverInterruptsAndAdopts covers zqe.5 restart recovery: a session left
// running is marked errored and its worktree re-adopted, without deletion.
func TestRecoverInterruptsAndAdopts(t *testing.T) {
	ctx := context.Background()
	registry, repos := fixtureRepo(t)

	store, err := sessionlog.Open(ctx, filepath.Join(t.TempDir(), "tethon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// Simulate a session that was running with a live worktree when the daemon
	// stopped. A restart reuses the same worktree root.
	wtRoot := filepath.Join(t.TempDir(), "worktrees")
	mgr1 := worktree.NewManager(wtRoot)
	wt, err := mgr1.Create(ctx, worktree.CreateOptions{ID: "ses_x", RepoID: repos[0].ID, RepoPath: repos[0].Path})
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, sessionlog.CreateSessionParams{RepoID: repos[0].ID, Repo: repos[0].Path, Agent: "claude-cli", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSessionWorktree(ctx, session.ID, wt.Path, wt.Branch); err != nil {
		t.Fatal(err)
	}

	// Restart: a fresh manager (empty live map) on the same root, then recover.
	mgr2 := worktree.NewManager(wtRoot)
	srv := NewWithConfig(store, Config{Agents: agent.NewRegistry(agent.NewFake()), Repos: registry, Worktrees: mgr2})
	if err := srv.Recover(ctx); err != nil {
		t.Fatal(err)
	}

	recovered, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "error" {
		t.Fatalf("interrupted session status = %q, want error", recovered.Status)
	}
	if _, ok := mgr2.Get(session.ID); !ok {
		t.Fatal("worktree should be adopted into the fresh manager")
	}
	if _, err := os.Stat(wt.Path); err != nil {
		t.Fatalf("recovered worktree should be retained on disk: %v", err)
	}
}

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

func gitBranchExists(t *testing.T, repoPath, branch string) bool {
	t.Helper()
	cmd := exec.Command("git", "-C", repoPath, "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	return cmd.Run() == nil
}
