package sessionlog

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestCodexActionClaimsStayUniqueAndSurviveRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "delivery.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	session, err := store.CreateSession(ctx, CreateSessionParams{Agent: "codex-cli", Repo: "."})
	if err != nil {
		t.Fatal(err)
	}

	var claims atomic.Int32
	var workers sync.WaitGroup
	for range 12 {
		workers.Add(1)
		go func() {
			defer workers.Done()

			status, _, err := store.BeginCodexAction(ctx, session.ID, "same-request", "same-input")
			if err != nil {
				t.Error(err)
			}
			if status == "new" {
				claims.Add(1)
			}
		}()
	}
	workers.Wait()
	if claims.Load() != 1 {
		t.Fatalf("native write claims = %d, want exactly one", claims.Load())
	}

	if _, _, err := store.BeginCodexAction(ctx, session.ID, "same-request", "changed-input"); !errors.Is(err, ErrCodexActionConflict) {
		t.Fatalf("changed input reused a transport ID: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	status, _, err := store.BeginCodexAction(ctx, session.ID, "same-request", "same-input")
	if err != nil || status != "uncertain" {
		t.Fatalf("a crash made unacknowledged input retryable: status=%q error=%v", status, err)
	}

	if err := store.ConfirmCodexAction(ctx, session.ID, "same-request"); err != nil {
		t.Fatal(err)
	}
	response := json.RawMessage(`{"events":[]}`)
	if err := store.CompleteCodexAction(ctx, session.ID, "same-request", "same-input", "acknowledged", response); err != nil {
		t.Fatalf("native history reconciliation raced the original acknowledgement: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	status, _, err = store.BeginCodexAction(ctx, session.ID, "same-request", "same-input")
	if err != nil || status != "acknowledged" {
		t.Fatalf("confirmed native input was not reusable: status=%q error=%v", status, err)
	}
}
