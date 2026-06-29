package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"tethon/internal/repo"
)

func TestDefaultDBPath(t *testing.T) {
	got := defaultDBPath()
	if filepath.Base(got) != "hostd.db" {
		t.Fatalf("defaultDBPath() = %q, want filename hostd.db", got)
	}
	if filepath.Base(filepath.Dir(got)) != "tethon" {
		t.Fatalf("defaultDBPath() = %q, want parent dir tethon", got)
	}
}

func TestDefaultStateDirXDG(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("XDG_DATA_HOME is not consulted on macOS")
	}
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	got, err := defaultStateDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/custom/data/tethon"; got != want {
		t.Fatalf("defaultStateDir() = %q, want %q", got, want)
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
