package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"tethon/internal/protocol"
	"tethon/internal/repo"
)

func TestDefaultDBPath(t *testing.T) {
	got := defaultDBPath()
	if filepath.Base(got) != "hostd.db" {
		t.Fatalf("defaultDBPath() = %q, want filename hostd.db", got)
	}
	if filepath.Base(filepath.Dir(got)) != "hostd" {
		t.Fatalf("defaultDBPath() = %q, want parent dir hostd", got)
	}
}

func TestDefaultStateDirForDarwin(t *testing.T) {
	got := defaultStateDirFor("darwin", "/Users/alice", "/custom/data")
	want := filepath.Join("/Users/alice", "Library", "Application Support", "Tethon", "hostd")
	if got != want {
		t.Fatalf("defaultStateDirFor(darwin) = %q, want %q", got, want)
	}
}

func TestDefaultStateDirForXDG(t *testing.T) {
	got := defaultStateDirFor("linux", "/home/alice", "/custom/data")
	want := filepath.Join("/custom/data", "tethon", "hostd")
	if got != want {
		t.Fatalf("defaultStateDirFor(linux) = %q, want %q", got, want)
	}
}

func TestDefaultStateDirForLocalShare(t *testing.T) {
	got := defaultStateDirFor("linux", "/home/alice", "")
	want := filepath.Join("/home/alice", ".local", "share", "tethon", "hostd")
	if got != want {
		t.Fatalf("defaultStateDirFor(linux) = %q, want %q", got, want)
	}
}

func TestCacheEnv(t *testing.T) {
	env := cacheEnv("/state/cache")
	want := map[string]string{
		"GOCACHE":          "/state/cache/go-build",
		"GOMODCACHE":       "/state/cache/go-mod",
		"npm_config_cache": "/state/cache/npm",
		"PIP_CACHE_DIR":    "/state/cache/pip",
		"CARGO_HOME":       "/state/cache/cargo",
	}
	got := map[string]string{}
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		if !ok {
			t.Fatalf("malformed env entry %q", kv)
		}
		got[key] = val
	}
	for key, val := range want {
		if got[key] != val {
			t.Fatalf("cacheEnv[%q] = %q, want %q", key, got[key], val)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("cacheEnv returned %d entries, want %d: %#v", len(got), len(want), env)
	}
}

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

func TestPostJSONSendsHubAuthToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q, want Bearer secret", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	if err := postJSON(ts.URL, map[string]string{"ok": "true"}, "secret"); err != nil {
		t.Fatal(err)
	}
}

func TestHubClientUsesConfiguredTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ts.Close)

	client := newHubClient(ts.URL, "", &http.Client{Timeout: time.Millisecond})
	err := client.registerHost(context.Background(), protocol.RegisterHostRequest{HostID: "mac", URL: "http://127.0.0.1:39181"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestHubConnectionLoopRegistersImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), testHubConnectionConfig())

	if got := hub.waitCall(t); got != "register" {
		t.Fatalf("first call = %q, want register", got)
	}
}

func TestHubConnectionLoopRetriesRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.registerErrs = []error{errors.New("hub down")}
	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), testHubConnectionConfig())

	if got := hub.waitCall(t); got != "register" {
		t.Fatalf("first call = %q, want register", got)
	}
	if got := hub.waitCall(t); got != "register" {
		t.Fatalf("second call = %q, want register", got)
	}
}

func TestHubConnectionLoopReregistersWhenHubForgetsHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.heartbeatErrs = []error{&httpStatusError{statusCode: http.StatusNotFound, status: "404 Not Found"}}
	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), testHubConnectionConfig())

	for _, want := range []string{"register", "heartbeat", "register"} {
		if got := hub.waitCall(t); got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	}
}

func TestHubConnectionLoopRetriesHeartbeatWithoutReregistering(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.heartbeatErrs = []error{errors.New("temporary network failure")}
	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), testHubConnectionConfig())

	for _, want := range []string{"register", "heartbeat", "heartbeat"} {
		if got := hub.waitCall(t); got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	}
}

func TestNextBackoffCapsAtMax(t *testing.T) {
	if got := nextBackoff(time.Second, 5*time.Second); got != 2*time.Second {
		t.Fatalf("nextBackoff = %s, want 2s", got)
	}
	if got := nextBackoff(4*time.Second, 5*time.Second); got != 5*time.Second {
		t.Fatalf("nextBackoff = %s, want 5s", got)
	}
	if got := nextBackoff(5*time.Second, 5*time.Second); got != 5*time.Second {
		t.Fatalf("nextBackoff = %s, want 5s", got)
	}
}

func TestIsHTTPStatus(t *testing.T) {
	err := &httpStatusError{statusCode: http.StatusNotFound, status: "404 Not Found"}
	if !isHTTPStatus(err, http.StatusNotFound) {
		t.Fatal("expected not found status match")
	}
	if isHTTPStatus(err, http.StatusInternalServerError) {
		t.Fatal("did not expect internal server error status match")
	}
	if isHTTPStatus(errors.New("network"), http.StatusNotFound) {
		t.Fatal("did not expect plain error status match")
	}
}

type fakeHub struct {
	mu            sync.Mutex
	registerErrs  []error
	heartbeatErrs []error
	calls         chan string
}

func newFakeHub() *fakeHub {
	return &fakeHub{calls: make(chan string, 16)}
}

func (h *fakeHub) registerHost(_ context.Context, _ protocol.RegisterHostRequest) error {
	h.calls <- "register"
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.registerErrs) == 0 {
		return nil
	}
	err := h.registerErrs[0]
	h.registerErrs = h.registerErrs[1:]
	return err
}

func (h *fakeHub) heartbeat(_ context.Context, _ string) error {
	h.calls <- "heartbeat"
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.heartbeatErrs) == 0 {
		return nil
	}
	err := h.heartbeatErrs[0]
	h.heartbeatErrs = h.heartbeatErrs[1:]
	return err
}

func (h *fakeHub) waitCall(t *testing.T) string {
	t.Helper()
	select {
	case call := <-h.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hub call")
		return ""
	}
}

func testRegisterRequest() protocol.RegisterHostRequest {
	return protocol.RegisterHostRequest{HostID: "mac", URL: "http://127.0.0.1:39181"}
}

func testHubConnectionConfig() hubConnectionConfig {
	return hubConnectionConfig{
		heartbeatInterval: time.Millisecond,
		minBackoff:        time.Millisecond,
		maxBackoff:        2 * time.Millisecond,
		jitter:            func(time.Duration) time.Duration { return 0 },
		logf:              func(string, ...any) {},
	}
}
