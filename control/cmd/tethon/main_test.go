package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
