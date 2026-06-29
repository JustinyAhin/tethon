package sessionlog

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"tethon/internal/protocol"
)

func TestAppendReplayOrdersEventsAfterCursor(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.AppendEvent(ctx, session.ID, protocol.EventOutput, protocol.OutputPayload{Text: "one"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.AppendEvent(ctx, session.ID, protocol.EventOutput, protocol.OutputPayload{Text: "two"})
	if err != nil {
		t.Fatal(err)
	}
	third, err := store.AppendEvent(ctx, session.ID, protocol.EventDone, protocol.DonePayload{Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}

	if first.Seq != 1 || second.Seq != 2 || third.Seq != 3 {
		t.Fatalf("unexpected seqs: %d %d %d", first.Seq, second.Seq, third.Seq)
	}

	events, err := store.Replay(ctx, session.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].Seq != 2 || events[1].Seq != 3 {
		t.Fatalf("unexpected replay seqs: %d %d", events[0].Seq, events[1].Seq)
	}
}

func TestReplayPersistsAcrossReopen(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tethon.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, session.ID, protocol.EventOutput, protocol.OutputPayload{Text: "persisted"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	events, err := reopened.Replay(ctx, session.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after reopen, got %d", len(events))
	}
	if events[0].Seq != 1 || events[0].Type != protocol.EventOutput {
		t.Fatalf("unexpected event after reopen: %+v", events[0])
	}
}

func TestResolveApprovalRequiresPendingApproval(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	payload := protocol.ApprovalPayload{RequestID: "req_1", Command: "test", Reason: "test"}
	if err := store.CreatePendingApproval(ctx, session.ID, "req_1", payload); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveApproval(ctx, session.ID, "req_1", "approve"); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveApproval(ctx, session.ID, "req_1", "deny"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for already resolved approval, got %v", err)
	}
}

func TestSessionMetadataPersistsAndModelUpdates(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	session, err := store.CreateSession(ctx, CreateSessionParams{
		Repo:    ".",
		Agent:   "claude-cli",
		Backend: "anthropic",
		CLI:     "claude",
		Model:   "claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if session.Backend != "anthropic" || session.CLI != "claude" || session.Model != "claude-sonnet-4-5" {
		t.Fatalf("unexpected session metadata: %+v", session)
	}
	if err := store.SetSessionModel(ctx, session.ID, "claude-opus-4-8"); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "anthropic" || got.CLI != "claude" || got.Model != "claude-opus-4-8" {
		t.Fatalf("unexpected stored metadata: %+v", got)
	}
}

func TestOpenMigratesSessionMetadataColumns(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "tethon.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		repo TEXT NOT NULL,
		cwd TEXT NOT NULL,
		branch TEXT NOT NULL,
		agent TEXT NOT NULL,
		status TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "claude-cli", Backend: "anthropic", CLI: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Backend != "anthropic" || got.CLI != "claude" {
		t.Fatalf("metadata columns were not migrated: %+v", got)
	}
}

func TestListSessionsByStatus(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	running, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	awaiting, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake", Status: "awaiting_approval"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "fake", Status: "done"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListSessionsByStatus(ctx, "running", "awaiting_approval")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 active sessions, got %d", len(got))
	}
	ids := map[string]bool{got[0].ID: true, got[1].ID: true}
	if !ids[running.ID] || !ids[awaiting.ID] {
		t.Fatalf("unexpected sessions returned: %+v", got)
	}

	none, err := store.ListSessionsByStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("expected no rows for empty status set, got %d", len(none))
	}
}

func TestListSessionSummariesReturnsAllSessionsWithLastSeq(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	done, err := store.CreateSession(ctx, CreateSessionParams{
		RepoID:  "repo_1",
		Repo:    "/repos/one",
		CWD:     "control",
		Branch:  "feature",
		BaseRef: "main",
		Agent:   "claude-cli",
		Backend: "anthropic",
		CLI:     "claude",
		Model:   "claude-sonnet-4-5",
		Status:  "done",
	})
	if err != nil {
		t.Fatal(err)
	}
	running, err := store.CreateSession(ctx, CreateSessionParams{Repo: "/repos/two", Agent: "fake", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, done.ID, protocol.EventOutput, protocol.OutputPayload{Text: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, done.ID, protocol.EventDone, protocol.DonePayload{Reason: "test"}); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListSessionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(got))
	}
	byID := map[string]protocol.SessionSummary{got[0].ID: got[0], got[1].ID: got[1]}
	if byID[done.ID].LastSeq != 2 {
		t.Fatalf("done last_seq = %d, want 2", byID[done.ID].LastSeq)
	}
	if byID[running.ID].LastSeq != 0 {
		t.Fatalf("running last_seq = %d, want 0", byID[running.ID].LastSeq)
	}
	if byID[done.ID].RepoID != "repo_1" || byID[done.ID].CWD != "control" || byID[done.ID].BaseRef != "main" || byID[done.ID].Model != "claude-sonnet-4-5" {
		t.Fatalf("metadata was not preserved: %+v", byID[done.ID])
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "tethon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	return store
}
