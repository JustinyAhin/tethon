package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os/exec"
	"testing"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/repo"
)

func TestRepoRefsEndpoint(t *testing.T) {
	repoPath := gitRepo(t)
	gitExec(t, repoPath, "branch", "feature/x")
	gitExec(t, repoPath, "tag", "v1.0.0")

	repos, err := repo.Discover(context.Background(), []string{repoPath})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWithConfig(t, Config{Agents: agent.DefaultRegistry(), Repos: registry})

	resp, err := http.Get(ts.URL + "/repos/" + repos[0].ID + "/refs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var refs protocol.RepoRefsResponse
	if err := json.NewDecoder(resp.Body).Decode(&refs); err != nil {
		t.Fatal(err)
	}
	if len(refs.Branches) == 0 || refs.Branches[0] != repos[0].DefaultBranch {
		t.Fatalf("expected default branch %q first, got %v", repos[0].DefaultBranch, refs.Branches)
	}
	if !containsStr(refs.Branches, "feature/x") {
		t.Fatalf("expected feature/x in branches, got %v", refs.Branches)
	}
	if !containsStr(refs.Tags, "v1.0.0") {
		t.Fatalf("expected v1.0.0 in tags, got %v", refs.Tags)
	}
}

func TestRepoRefsUnknownRepoReturns404(t *testing.T) {
	repos, err := repo.Discover(context.Background(), []string{gitRepo(t)})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWithConfig(t, Config{Agents: agent.DefaultRegistry(), Repos: registry})

	resp, err := http.Get(ts.URL + "/repos/nope/refs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestRepoRefsRejectsNonGet(t *testing.T) {
	repos, err := repo.Discover(context.Background(), []string{gitRepo(t)})
	if err != nil {
		t.Fatal(err)
	}
	registry, err := repo.NewRegistry(repos...)
	if err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWithConfig(t, Config{Agents: agent.DefaultRegistry(), Repos: registry})

	resp, err := http.Post(ts.URL+"/repos/"+repos[0].ID+"/refs", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func gitExec(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func containsStr(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
