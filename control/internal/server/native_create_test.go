package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"

	"tethon/internal/agent"
	"tethon/internal/codex"
	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

type nativeCreateCounter struct {
	agent.Adapter
	starts int
}

type nativeResumeCounter struct {
	nativeRunningSession
	inputs    atomic.Int32
	uncertain atomic.Bool
}

func (a *nativeCreateCounter) Name() string {
	return "codex-cli"
}

func (a *nativeCreateCounter) Start(ctx context.Context, request agent.StartRequest, sink agent.Sink) (agent.RunningSession, error) {
	a.starts++

	return a.Adapter.Start(ctx, request, sink)
}

func (s *nativeResumeCounter) InputNative(context.Context, protocol.InputRequest, string) ([]protocol.Event, error) {
	s.inputs.Add(1)
	if s.uncertain.Load() {
		return nil, &codex.DeliveryError{Err: errors.New("native acknowledgement was lost")}
	}

	return []protocol.Event{}, nil
}

func TestNativeResumeIntentCannotMoveThreadsOrRedispatchUncertainInput(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store, err := sessionlog.Open(ctx, filepath.Join(root, "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	host := NewWithConfig(store, Config{
		Agents:    agent.NewRegistry(agent.NewCodex(agent.CodexConfig{Binary: filepath.Join(root, "missing-codex")})),
		RepoRoots: []string{root},
	})
	threads := []string{uuid.NewString(), uuid.NewString()}
	runners := []*nativeResumeCounter{{}, {}}
	for index, threadID := range threads {
		session, err := store.CreateSession(ctx, sessionlog.CreateSessionParams{Agent: "codex-cli", Repo: root})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetCodexState(ctx, session.ID, protocol.CodexSessionState{
			ThreadID:          threadID,
			ConnectionID:      uuid.NewString(),
			ConnectionState:   protocol.CodexConnected,
			ConversationState: protocol.CodexConversationReady,
			TurnState:         protocol.CodexTurnCompleted,
		}); err != nil {
			t.Fatal(err)
		}

		host.setRunning(session.ID, runners[index])
	}

	endpoint := httptest.NewServer(host.Handler())
	defer endpoint.Close()

	request := protocol.CreateSessionRequest{
		Agent:          "codex-cli",
		NativeThreadID: threads[0],
		RequestID:      uuid.NewString(),
		Prompt:         "Send this resume prompt once",
	}
	for range 2 {
		if status := postJSONStatus(t, endpoint.URL+"/sessions", request); status != http.StatusOK {
			t.Fatalf("acknowledged resume retry: HTTP %d", status)
		}
	}
	request.NativeThreadID = threads[1]
	if status := postJSONStatus(t, endpoint.URL+"/sessions", request); status != http.StatusConflict {
		t.Fatalf("resume intent moved to another native thread: HTTP %d", status)
	}
	if runners[0].inputs.Load() != 1 || runners[1].inputs.Load() != 0 {
		t.Fatalf("resume retry sent duplicate work: first=%d second=%d", runners[0].inputs.Load(), runners[1].inputs.Load())
	}

	request.NativeThreadID = threads[0]
	request.RequestID = uuid.NewString()
	request.Prompt = "Do not retry uncertain native delivery"
	runners[0].uncertain.Store(true)
	for range 2 {
		if status := postJSONStatus(t, endpoint.URL+"/sessions", request); status != http.StatusGatewayTimeout {
			t.Fatalf("uncertain resume must not look successful or dispatch again: HTTP %d", status)
		}
	}
	if runners[0].inputs.Load() != 2 {
		t.Fatalf("uncertain resume dispatched more than once: total inputs=%d", runners[0].inputs.Load())
	}
}

func TestNativeCreateRetryAfterRestartDoesNotStartSecondAgent(t *testing.T) {
	ctx := context.Background()
	database := filepath.Join(t.TempDir(), "sessions.db")
	adapter := &nativeCreateCounter{Adapter: agent.NewFake()}
	request := protocol.CreateSessionRequest{
		RequestID: uuid.NewString(),
		Agent:     "codex-cli",
		Repo:      t.TempDir(),
		Prompt:    "Execute this prompt once",
	}

	store, err := sessionlog.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	host := httptest.NewServer(NewWithAgents(store, agent.NewRegistry(adapter)).Handler())

	// Ignore the first response to represent a lost create acknowledgement.
	request.RequestID = strings.ToUpper(request.RequestID)
	if status := postJSONStatus(t, host.URL+"/sessions", request); status != http.StatusCreated {
		t.Fatalf("initial creation: HTTP %d", status)
	}

	host.Close()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = sessionlog.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	host = httptest.NewServer(NewWithAgents(store, agent.NewRegistry(adapter)).Handler())
	defer host.Close()

	request.RequestID = strings.ToLower(request.RequestID)
	if status := postJSONStatus(t, host.URL+"/sessions", request); status != http.StatusOK {
		t.Fatalf("retried creation after restart: HTTP %d", status)
	}

	sessions, err := store.ListSessionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(sessions) != 1 || adapter.starts != 1 {
		t.Fatalf("create retry started duplicate work: %d sessions, %d starts", len(sessions), adapter.starts)
	}

	request.Prompt = "Different work must use a new intent"
	if status := postJSONStatus(t, host.URL+"/sessions", request); status != http.StatusConflict {
		t.Fatalf("changed input reused the old intent: HTTP %d", status)
	}
}
