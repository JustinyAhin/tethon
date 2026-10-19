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
