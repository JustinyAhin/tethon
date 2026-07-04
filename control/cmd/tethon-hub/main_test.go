package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredAuthTokens(t *testing.T) {
	got := configuredAuthTokens(" primary ", "old,new\n also-valid ")
	want := []string{"primary", "old", "new", "also-valid"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("configuredAuthTokens = %#v, want %#v", got, want)
	}
}

func TestConfiguredAuthTokensIgnoresBlanks(t *testing.T) {
	got := configuredAuthTokens("", " \n, ")
	if len(got) != 0 {
		t.Fatalf("configuredAuthTokens returned %#v, want empty", got)
	}
}

func TestDefaultDBPath(t *testing.T) {
	got := defaultDBPath()
	if filepath.Base(got) != "hub.db" {
		t.Fatalf("defaultDBPath() = %q, want filename hub.db", got)
	}
	if filepath.Base(filepath.Dir(got)) != "hub" {
		t.Fatalf("defaultDBPath() = %q, want parent dir hub", got)
	}
}

func TestDefaultStateDirForDarwin(t *testing.T) {
	got := defaultStateDirFor("darwin", "/Users/alice", "/custom/data")
	want := filepath.Join("/Users/alice", "Library", "Application Support", "Tethon", "hub")
	if got != want {
		t.Fatalf("defaultStateDirFor(darwin) = %q, want %q", got, want)
	}
}

func TestDefaultStateDirForXDG(t *testing.T) {
	got := defaultStateDirFor("linux", "/home/alice", "/custom/data")
	want := filepath.Join("/custom/data", "tethon", "hub")
	if got != want {
		t.Fatalf("defaultStateDirFor(linux) = %q, want %q", got, want)
	}
}
