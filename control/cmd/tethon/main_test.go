package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tethon/internal/protocol"
)

func TestGetJSONSendsAuthToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(ts.Close)

	var out any
	if err := getJSON(ts.URL, "secret", &out, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestPostJSONSendsAuthToken(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("Authorization = %q, want Bearer secret", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(ts.Close)

	var out any
	if err := postJSON(ts.URL, "secret", map[string]string{"ok": "true"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestSessionStartSendsPermissionMode(t *testing.T) {
	var got protocol.CreateSessionRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Fatalf("path = %q, want /sessions", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"mac/ses_1","status":"running"}`))
	}))
	t.Cleanup(ts.Close)

	err := sessionStart([]string{
		"--hub-url", ts.URL,
		"--host", "mac",
		"--repo", ".",
		"--agent", "claude-cli",
		"--permission-mode", "plan",
		"--prompt", "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.PermissionMode != "plan" {
		t.Fatalf("permission_mode = %q, want plan", got.PermissionMode)
	}
}

func TestHubFlagsDefaultAndParse(t *testing.T) {
	hubURL, _, rest, err := hubFlags([]string{"list"})
	if err != nil {
		t.Fatal(err)
	}
	if hubURL != defaultHubURL {
		t.Fatalf("hubURL = %q, want default %q", hubURL, defaultHubURL)
	}
	if len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("rest = %v, want [list]", rest)
	}

	hubURL, authToken, rest, err := hubFlags([]string{"--hub-url", "https://hub.tethon.dev", "--auth-token", "secret", "list"})
	if err != nil {
		t.Fatal(err)
	}
	if hubURL != "https://hub.tethon.dev" {
		t.Fatalf("hubURL = %q, want https://hub.tethon.dev", hubURL)
	}
	if authToken != "secret" {
		t.Fatalf("authToken = %q, want secret", authToken)
	}
	if len(rest) != 1 || rest[0] != "list" {
		t.Fatalf("rest = %v, want [list]", rest)
	}
}

func TestHubEndpoint(t *testing.T) {
	cases := []struct {
		hubURL string
		path   string
		want   string
	}{
		{"http://127.0.0.1:39180", "/hosts", "http://127.0.0.1:39180/hosts"},
		{"https://hub.tethon.dev/", "/hosts", "https://hub.tethon.dev/hosts"},
	}
	for _, c := range cases {
		if got := hubEndpoint(c.hubURL, c.path); got != c.want {
			t.Fatalf("hubEndpoint(%q, %q) = %q, want %q", c.hubURL, c.path, got, c.want)
		}
	}
}
