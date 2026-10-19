package server

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"tethon/internal/agent"
)

func TestCodexSettingsRejectsWorkingFoldersOutsideAdvertisedRoots(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	linked := filepath.Join(root, "linked-outside")
	if err := os.Symlink(outside, linked); err != nil {
		t.Fatal(err)
	}
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewCodex(agent.CodexConfig{Binary: filepath.Join(root, "missing-codex")})),
		RepoRoots: []string{root},
	})
	for _, cwd := range []string{outside, linked, "relative"} {
		t.Run(cwd, func(t *testing.T) {
			status := getStatus(t, ts.URL+"/agents/codex-cli/settings?cwd="+url.QueryEscape(cwd))
			if status != http.StatusBadRequest {
				t.Fatalf("settings read for unadvertised folder returned %d, want 400 before a native connection", status)
			}
		})
	}
}
