package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

func TestHostDiagnosticHealthAndStatus(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	status := newHostStatus(hostStatusConfig{
		hostID:        "mac",
		advertisedURL: "http://100.64.0.10:39181",
		hubURL:        "https://hub.tethon.dev",
		now:           func() time.Time { return now },
		repos: []protocol.RepoInfo{
			{ID: "alpha", Name: "alpha", Path: "/secret/path/alpha"},
			{ID: "beta", Name: "beta", Path: "/secret/path/beta"},
		},
	})
	now = now.Add(3 * time.Second)
	status.recordHubSuccess()
	now = now.Add(2 * time.Second)
	ts := httptest.NewServer(hostDiagnosticHandler(http.NotFoundHandler(), status))
	t.Cleanup(ts.Close)

	var health map[string]string
	getJSON(t, ts.URL+"/healthz", &health)
	if health["status"] != "ok" {
		t.Fatalf("health status = %q, want ok", health["status"])
	}

	var out hostStatusResponse
	getJSON(t, ts.URL+"/status", &out)
	if out.Status != "ok" || out.HostID != "mac" || out.AdvertisedURL != "http://100.64.0.10:39181" || out.HubURL != "https://hub.tethon.dev" {
		t.Fatalf("unexpected status response: %+v", out)
	}
	if out.RepoCount != 2 || len(out.Repos) != 2 || out.Repos[0].ID != "alpha" || out.Repos[0].Name != "alpha" {
		t.Fatalf("unexpected repo summary: %+v", out)
	}
	if out.UptimeSeconds != 5 {
		t.Fatalf("uptime = %d, want 5", out.UptimeSeconds)
	}
	if !out.Registered || out.Retrying || out.LastHubSuccess != "2026-06-29T12:00:03Z" || out.LastHubFailure != nil || out.NextRetryDelay != "" {
		t.Fatalf("unexpected hub state: %+v", out)
	}
	raw := getBody(t, ts.URL+"/status")
	if strings.Contains(raw, "/secret/path") || strings.Contains(raw, "Bearer") || strings.Contains(raw, "Authorization") {
		t.Fatalf("status leaked sensitive data: %s", raw)
	}
}

func TestHostDiagnosticStatusTracksHubFailure(t *testing.T) {
	now := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	status := newHostStatus(hostStatusConfig{
		hostID:        "mac",
		advertisedURL: "http://100.64.0.10:39181",
		hubURL:        "https://hub.tethon.dev",
		now:           func() time.Time { return now },
	})
	status.recordHubFailure(errors.New("network down"), 5*time.Second, false)
	ts := httptest.NewServer(hostDiagnosticHandler(http.NotFoundHandler(), status))
	t.Cleanup(ts.Close)

	var out hostStatusResponse
	getJSON(t, ts.URL+"/status", &out)
	if out.Registered || !out.Retrying || out.NextRetryDelay != "5s" {
		t.Fatalf("unexpected retry state: %+v", out)
	}
	if out.LastHubFailure == nil || out.LastHubFailure.At != "2026-06-29T12:00:00Z" || out.LastHubFailure.Error != "network down" {
		t.Fatalf("unexpected failure state: %+v", out.LastHubFailure)
	}
}

func TestHostDiagnosticStatusUpdatesFromHubConnectionLoop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.heartbeatErrs = []error{errors.New("temporary network failure")}
	status := newHostStatus(hostStatusConfig{hostID: "mac", advertisedURL: "http://127.0.0.1:39181", hubURL: "https://hub.tethon.dev"})
	cfg := testHubConnectionConfig()
	cfg.status = status

	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), cfg)

	for _, want := range []string{"register", "heartbeat"} {
		if got := hub.waitCall(t); got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	}
	failed := status.snapshot()
	if !failed.Registered || !failed.Retrying || failed.NextRetryDelay != "1ms" || failed.LastHubFailure == nil {
		t.Fatalf("unexpected failed status: %+v", failed)
	}
	if got := hub.waitCall(t); got != "heartbeat" {
		t.Fatalf("call = %q, want heartbeat", got)
	}
	recovered := status.snapshot()
	if !recovered.Registered || recovered.Retrying || recovered.NextRetryDelay != "" || recovered.LastHubSuccess == "" {
		t.Fatalf("unexpected recovered status: %+v", recovered)
	}
}

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

func TestHubConnectionLoopLogsRegistrationSuccessMetadataWithoutToken(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	logs := newSafeBuffer()
	req := protocol.RegisterHostRequest{
		HostID: "mac",
		URL:    "http://100.64.0.10:39181",
		Agents: []string{"fake", "claude-cli"},
		Repos: []protocol.RepoInfo{
			{ID: "one", Path: "/repos/one"},
			{ID: "two", Path: "/repos/two"},
		},
	}
	cfg := testHubConnectionConfig()
	cfg.hubURL = "https://hub.tethon.dev"
	cfg.logger = slog.New(slog.NewJSONHandler(logs, nil))

	go runHubConnectionLoop(ctx, hub, req, cfg)

	if got := hub.waitCall(t); got != "register" {
		t.Fatalf("first call = %q, want register", got)
	}
	raw := logs.waitFor(t, "host_registered")
	for _, want := range []string{
		`"msg":"host_registered"`,
		`"hub_url":"https://hub.tethon.dev"`,
		`"host_id":"mac"`,
		`"advertised_url":"http://100.64.0.10:39181"`,
		`"repo_count":2`,
		`"agent_count":2`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, "secret") || strings.Contains(raw, "Authorization") || strings.Contains(raw, "Bearer") {
		t.Fatalf("logs contain secret-like value: %s", raw)
	}
}

func TestHubConnectionLoopLogsHeartbeatFailureRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.heartbeatErrs = []error{errors.New("network down")}
	logs := newSafeBuffer()
	cfg := testHubConnectionConfig()
	cfg.hubURL = "https://hub.tethon.dev"
	cfg.logger = slog.New(slog.NewJSONHandler(logs, nil))

	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), cfg)

	for _, want := range []string{"register", "heartbeat"} {
		if got := hub.waitCall(t); got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	}
	raw := logs.waitFor(t, "hub_heartbeat_failed")
	for _, want := range []string{
		`"msg":"hub_heartbeat_failed"`,
		`"host_id":"mac"`,
		`"failed_attempts":1`,
		`"next_retry_delay":"1ms"`,
		`"error":"network down"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
}

func TestHubConnectionLoopLogsHeartbeatRecoveryAfterFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.heartbeatErrs = []error{errors.New("temporary network failure")}
	logs := newSafeBuffer()
	cfg := testHubConnectionConfig()
	cfg.hubURL = "https://hub.tethon.dev"
	cfg.logger = slog.New(slog.NewJSONHandler(logs, nil))

	go runHubConnectionLoop(ctx, hub, testRegisterRequest(), cfg)

	for _, want := range []string{"register", "heartbeat", "heartbeat"} {
		if got := hub.waitCall(t); got != want {
			t.Fatalf("call = %q, want %q", got, want)
		}
	}
	raw := logs.waitFor(t, "hub_heartbeat_recovered")
	for _, want := range []string{
		`"msg":"hub_heartbeat_recovered"`,
		`"host_id":"mac"`,
		`"failed_attempts":1`,
		`"next_heartbeat_delay":"1ms"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
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

func TestReverseURL(t *testing.T) {
	got, err := reverseURL("https://hub.tethon.dev/base", "mac 1")
	if err != nil {
		t.Fatal(err)
	}
	if got != "wss://hub.tethon.dev/base/hosts/mac%201/connect" {
		t.Fatalf("reverseURL = %q", got)
	}
}

func TestHubReverseLoopRecordsConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	status := newHostStatus(hostStatusConfig{hostID: "mac"})
	cfg := testHubReverseConfig()
	cfg.status = status

	go runHubReverseLoop(ctx, hub, "mac", http.NotFoundHandler(), cfg)

	if got := hub.waitCall(t); got != "reverse" {
		t.Fatalf("call = %q, want reverse", got)
	}
	waitForStatus(t, func() bool { return status.snapshot().ReverseConnected })
}

func TestHubReverseLoopRetriesFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	hub := newFakeHub()
	hub.reverseErrs = []error{errors.New("dial failed")}
	status := newHostStatus(hostStatusConfig{hostID: "mac"})
	cfg := testHubReverseConfig()
	cfg.status = status

	go runHubReverseLoop(ctx, hub, "mac", http.NotFoundHandler(), cfg)

	if got := hub.waitCall(t); got != "reverse" {
		t.Fatalf("first call = %q, want reverse", got)
	}
	if got := hub.waitCall(t); got != "reverse" {
		t.Fatalf("second call = %q, want reverse", got)
	}
	snapshot := status.snapshot()
	if !snapshot.ReverseConnected || snapshot.LastReverseFailure == nil || snapshot.LastReverseFailure.Error != "dial failed" {
		t.Fatalf("unexpected reverse status after retry: %+v", snapshot)
	}
}

func getJSON(t *testing.T, target string, out any) {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %s", target, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func getBody(t *testing.T, target string) string {
	t.Helper()
	resp, err := http.Get(target)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s returned %s: %s", target, resp.Status, body)
	}
	return string(body)
}

type fakeHub struct {
	mu            sync.Mutex
	registerErrs  []error
	heartbeatErrs []error
	reverseErrs   []error
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

func (h *fakeHub) connectReverse(ctx context.Context, _ string, _ http.Handler, onConnected func()) error {
	h.calls <- "reverse"
	h.mu.Lock()
	if len(h.reverseErrs) > 0 {
		err := h.reverseErrs[0]
		h.reverseErrs = h.reverseErrs[1:]
		h.mu.Unlock()
		return err
	}
	h.mu.Unlock()
	if onConnected != nil {
		onConnected()
	}
	<-ctx.Done()
	return ctx.Err()
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
		logger:            slog.New(slog.NewJSONHandler(newSafeBuffer(), nil)),
	}
}

func testHubReverseConfig() hubReverseConfig {
	return hubReverseConfig{
		minBackoff: time.Millisecond,
		maxBackoff: 2 * time.Millisecond,
		jitter:     func(time.Duration) time.Duration { return 0 },
		logger:     slog.New(slog.NewJSONHandler(newSafeBuffer(), nil)),
	}
}

func waitForStatus(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if ok() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for status")
		case <-tick.C:
		}
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func newSafeBuffer() *safeBuffer {
	return &safeBuffer{}
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *safeBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

func (b *safeBuffer) waitFor(t *testing.T, want string) string {
	t.Helper()
	deadline := time.After(time.Second)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		raw := b.String()
		if strings.Contains(raw, want) {
			return raw
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for log %q; logs: %s", want, raw)
		case <-tick.C:
		}
	}
}
