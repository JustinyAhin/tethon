package sessionlog

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestCreateIntentBindingsStayGlobalAndKeepOutcomesAcrossRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "delivery.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.CreateSession(ctx, CreateSessionParams{
		Agent:             "codex-cli",
		Repo:              ".",
		CreateRequestID:   "initial-id",
		CreateFingerprint: "initial-payload",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateSession(ctx, CreateSessionParams{Agent: "codex-cli", Repo: "."})
	if err != nil {
		t.Fatal(err)
	}

	// An upgraded database can still have only the original session columns.
	if _, err := store.db.ExecContext(ctx, `DELETE FROM create_requests WHERE request_id = 'initial-id'`); err != nil {
		t.Fatal(err)
	}
	legacy, found, err := store.FindCreatedSession(ctx, "initial-id", "initial-payload")
	if err != nil || !found || legacy.ID != first.ID {
		t.Fatalf("initial create receipt was lost during upgrade: session=%s found=%t error=%v", legacy.ID, found, err)
	}
	if err := store.BindCreatedSession(ctx, "initial-id", "initial-payload", second.ID); !errors.Is(err, ErrCodexActionConflict) {
		t.Fatalf("an initial create ID moved to another session: %v", err)
	}

	type claim struct {
		sessionID string
		err       error
	}
	start := make(chan struct{})
	results := make(chan claim, 2)
	for _, sessionID := range []string{first.ID, second.ID} {
		go func() {
			<-start
			results <- claim{sessionID: sessionID, err: store.BindCreatedSession(ctx, "raced-id", "same-payload", sessionID)}
		}()
	}
	close(start)

	winner := ""
	for range 2 {
		result := <-results
		if result.err == nil {
			if winner != "" {
				t.Fatal("the same create ID was claimed by two sessions")
			}
			winner = result.sessionID
		} else if !errors.Is(result.err, ErrCodexActionConflict) {
			t.Fatalf("concurrent cross-session claim returned %v", result.err)
		}
	}
	if winner == "" {
		t.Fatal("no session claimed the resume intent")
	}
	if err := store.BindCreatedSession(ctx, "raced-id", "same-payload", winner); !errors.Is(err, ErrCreateRequestUncertain) {
		t.Fatalf("a repeated pending intent could dispatch twice: %v", err)
	}
	if _, err := store.CreateSession(ctx, CreateSessionParams{
		Agent:             "codex-cli",
		Repo:              ".",
		CreateRequestID:   "raced-id",
		CreateFingerprint: "same-payload",
	}); !errors.Is(err, ErrCodexActionConflict) {
		t.Fatalf("new creation reused a known-resume ID: %v", err)
	}

	for _, requestID := range []string{"accepted-id", "rejected-id"} {
		if err := store.BindCreatedSession(ctx, requestID, requestID+"-payload", first.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.CompleteCreatedSession(ctx, "accepted-id", "accepted-id-payload", first.ID, "acknowledged", ""); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteCreatedSession(ctx, "rejected-id", "rejected-id-payload", first.ID, "rejected", "native turn is busy"); err != nil {
		t.Fatal(err)
	}
	if err := store.CompleteCreatedSession(ctx, "accepted-id", "accepted-id-payload", first.ID, "rejected", "late error"); !errors.Is(err, ErrCodexActionConflict) {
		t.Fatalf("a late failure replaced an acknowledged intent: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if _, found, err := store.FindCreatedSession(ctx, "raced-id", "same-payload"); !found || !errors.Is(err, ErrCreateRequestUncertain) {
		t.Fatalf("a restart made undispatched or uncertain work look successful: found=%t error=%v", found, err)
	}
	accepted, found, err := store.FindCreatedSession(ctx, "accepted-id", "accepted-id-payload")
	if err != nil || !found || accepted.ID != first.ID {
		t.Fatalf("acknowledged resume did not keep its session: session=%s found=%t error=%v", accepted.ID, found, err)
	}
	if _, found, err := store.FindCreatedSession(ctx, "rejected-id", "rejected-id-payload"); !found || err == nil || err.Error() != "native turn is busy" {
		t.Fatalf("restart hid a definite native rejection: found=%t error=%v", found, err)
	}
	if _, _, err := store.FindCreatedSession(ctx, "accepted-id", "changed-payload"); !errors.Is(err, ErrCodexActionConflict) {
		t.Fatalf("changed payload reused an acknowledged resume ID: %v", err)
	}
}
