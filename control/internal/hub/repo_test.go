package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestHubRelaysRepoRefs(t *testing.T) {
	var gotPath string
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		writeJSON(w, http.StatusOK, protocol.RepoRefsResponse{
			Branches: []string{"main", "feature/x"},
			Tags:     []string{"v1.0.0"},
		})
	}))
	t.Cleanup(host.Close)

	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{HostID: "mac", URL: host.URL})
	connectReverseHost(t, hub.URL, "mac", "", host.Config.Handler)

	var refs protocol.RepoRefsResponse
	getJSON(t, hub.URL+"/repos/mac/myrepo/refs", &refs)

	if gotPath != "/repos/myrepo/refs" {
		t.Fatalf("host received %q, want /repos/myrepo/refs", gotPath)
	}
	if len(refs.Branches) != 2 || refs.Branches[0] != "main" {
		t.Fatalf("unexpected branches: %v", refs.Branches)
	}
	if len(refs.Tags) != 1 || refs.Tags[0] != "v1.0.0" {
		t.Fatalf("unexpected tags: %v", refs.Tags)
	}
}

func TestHubRelaysRepoRefsNotFound(t *testing.T) {
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "unknown repo_id")
	}))
	t.Cleanup(host.Close)

	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{HostID: "mac", URL: host.URL})
	connectReverseHost(t, hub.URL, "mac", "", host.Config.Handler)

	var resp *http.Response
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		resp, err = http.Get(hub.URL + "/repos/mac/nope/refs")
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusServiceUnavailable {
			break
		}
		resp.Body.Close()
		time.Sleep(time.Millisecond)
	}
	if resp == nil {
		t.Fatal("timed out waiting for repo refs response")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %s", resp.Status)
	}
	var errResp protocol.ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		t.Fatal(err)
	}
	if errResp.Error != "unknown repo_id" {
		t.Fatalf("expected relayed error message, got %q", errResp.Error)
	}
}

func TestHubRepoRefsOfflineHostReturns503(t *testing.T) {
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)

	resp, err := http.Get(hub.URL + "/repos/ghost/myrepo/refs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %s", resp.Status)
	}
}

func TestHubReverseConnectionReplacementUsesNewestConnection(t *testing.T) {
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{HostID: "mac", URL: "http://127.0.0.1:1"})

	connectReverseHost(t, hub.URL, "mac", "", repoRefsHandler("old"))
	connectReverseHost(t, hub.URL, "mac", "", repoRefsHandler("new"))

	var refs protocol.RepoRefsResponse
	getJSON(t, hub.URL+"/repos/mac/myrepo/refs", &refs)
	if len(refs.Branches) != 1 || refs.Branches[0] != "new" {
		t.Fatalf("branches = %v, want newest connection branch", refs.Branches)
	}
}

func repoRefsHandler(branch string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, protocol.RepoRefsResponse{Branches: []string{branch}, Tags: []string{}})
	})
}
