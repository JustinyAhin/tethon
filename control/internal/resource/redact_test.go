package resource

import (
	"strings"
	"testing"
)

func TestRedactCommandLineRedactsRepresentativeSecrets(t *testing.T) {
	got := RedactCommandLine(
		`DEBUG=plain OPENAI_API_KEY=sk-env claude --api-key sk-flag --password=hunter2 --header "Authorization: Bearer abc" /Users/alice/projects/app/.env`,
		RedactionOptions{
			MaxBytes: 1000,
			PathRoots: []PathRoot{
				{Label: "repo", Path: "/Users/alice/projects/app"},
			},
		},
	)

	if !got.Redacted {
		t.Fatal("Redacted = false, want true")
	}
	for _, leaked := range []string{"plain", "sk-env", "sk-flag", "hunter2", "Bearer abc", "/Users/alice"} {
		if strings.Contains(got.Value, leaked) {
			t.Fatalf("redacted command leaked %q in %q", leaked, got.Value)
		}
	}
	for _, want := range []string{"[env]", "--api-key [redacted]", "--password=[redacted]", "[redacted]", "[redacted path]"} {
		if !strings.Contains(got.Value, want) {
			t.Fatalf("redacted command = %q, want it to contain %q", got.Value, want)
		}
	}
}

func TestRedactCommandLineBoundsOutput(t *testing.T) {
	got := RedactCommandLine("claude "+strings.Repeat("x", 500), RedactionOptions{MaxBytes: 48})
	if !got.Truncated {
		t.Fatal("Truncated = false, want true")
	}
	if len(got.Value) > 48 {
		t.Fatalf("len(Value) = %d, want <= 48: %q", len(got.Value), got.Value)
	}
}

func TestRedactPathLabelsOwnedPaths(t *testing.T) {
	got := RedactPath("/Users/alice/projects/app/src/main.go", []PathRoot{
		{Label: "worktree", Path: "/Users/alice/projects/app"},
	}, 1000)

	if got.Value != "$worktree/src/main.go" {
		t.Fatalf("Value = %q, want labeled path", got.Value)
	}
	if !got.Redacted {
		t.Fatal("Redacted = false, want true because raw host path changed")
	}
}

func TestRedactPathHidesExternalAndSensitivePaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "external home path", path: "/Users/alice/private/client-a/notes.txt", want: "[external]/notes.txt"},
		{name: "env file", path: "/Users/alice/projects/app/.env.local", want: "[redacted path]"},
		{name: "ssh key", path: "/Users/alice/.ssh/id_ed25519", want: "[redacted path]"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactPath(tc.path, []PathRoot{{Label: "worktree", Path: "/Users/alice/projects/app"}}, 1000)
			if got.Value != tc.want {
				t.Fatalf("Value = %q, want %q", got.Value, tc.want)
			}
			if !got.Redacted {
				t.Fatal("Redacted = false, want true")
			}
			if strings.Contains(got.Value, "/Users/alice") {
				t.Fatalf("redacted path leaked home prefix: %q", got.Value)
			}
		})
	}
}
