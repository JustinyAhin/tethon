package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

type modelTestAdapter struct {
	agent.Adapter
	catalog   protocol.ModelCatalog
	err       error
	listCalls int
	starts    int
}

func (a *modelTestAdapter) ListModels(context.Context) (protocol.ModelCatalog, error) {
	a.listCalls++
	return a.catalog, a.err
}

func (a *modelTestAdapter) Start(ctx context.Context, req agent.StartRequest, sink agent.Sink) (agent.RunningSession, error) {
	a.starts++
	return a.Adapter.Start(ctx, req, sink)
}

func TestCreateSessionValidatesRequestedModelBeforeStart(t *testing.T) {
	for _, tc := range []struct {
		name          string
		requested     string
		catalog       protocol.ModelCatalog
		catalogErr    error
		wantStatus    int
		wantListCalls int
		wantStarts    int
	}{
		{name: "default skips provider", catalogErr: errors.New("provider unavailable"), wantStatus: http.StatusCreated, wantStarts: 1},
		{name: "selected model", requested: "provider-alias", catalog: protocol.ModelCatalog{Models: []protocol.ModelInfo{{ID: "provider-alias"}}}, wantStatus: http.StatusCreated, wantListCalls: 1, wantStarts: 1},
		{name: "unavailable model", requested: "missing", catalog: protocol.ModelCatalog{Models: []protocol.ModelInfo{{ID: "provider-alias"}}}, wantStatus: http.StatusBadRequest, wantListCalls: 1},
		{name: "empty catalog", requested: "missing", wantStatus: http.StatusBadRequest, wantListCalls: 1},
		{name: "provider failure", requested: "provider-alias", catalogErr: errors.New("provider unavailable"), wantStatus: http.StatusBadGateway, wantListCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &modelTestAdapter{Adapter: agent.NewFake(), catalog: tc.catalog, err: tc.catalogErr}
			store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "sessions.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			ts := httptest.NewServer(NewWithAgents(store, agent.NewRegistry(adapter)).Handler())
			defer ts.Close()
			status := postJSONStatus(t, ts.URL+"/sessions", protocol.CreateSessionRequest{Repo: t.TempDir(), Agent: "fake", RequestedModel: tc.requested})
			if status != tc.wantStatus || adapter.listCalls != tc.wantListCalls || adapter.starts != tc.wantStarts {
				t.Fatalf("status/list calls/starts = %d/%d/%d, want %d/%d/%d", status, adapter.listCalls, adapter.starts, tc.wantStatus, tc.wantListCalls, tc.wantStarts)
			}
			summaries, err := store.ListSessionSummaries(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(summaries) != tc.wantStarts {
				t.Fatalf("sessions = %d, want %d", len(summaries), tc.wantStarts)
			}
			if tc.wantStarts == 0 {
				return
			}
			if summaries[0].RequestedModel != tc.requested || summaries[0].Model != "" {
				t.Fatalf("model summary = %+v", summaries[0])
			}
			events, err := store.Replay(context.Background(), summaries[0].ID, 0)
			if err != nil {
				t.Fatal(err)
			}
			var started protocol.SessionStartedPayload
			if len(events) == 0 {
				t.Fatal("missing session_started event")
			}
			if err := json.Unmarshal(events[0].Payload, &started); err != nil {
				t.Fatal(err)
			}
			if started.RequestedModel != tc.requested {
				t.Fatalf("requested model event = %q", started.RequestedModel)
			}
		})
	}
}

func TestCreateSessionRejectsSelectionForUnsupportedAdapter(t *testing.T) {
	ts := newTestServerWithAgents(t, agent.NewRegistry(agent.NewFake()))
	if status := postJSONStatus(t, ts.URL+"/sessions", protocol.CreateSessionRequest{Repo: t.TempDir(), Agent: "fake", RequestedModel: "any-model"}); status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", status)
	}
}
