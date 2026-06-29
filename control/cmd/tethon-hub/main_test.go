package main

import (
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
