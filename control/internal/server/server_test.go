package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

func TestCreateSessionAndReplayStream(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	events := streamEvents(t, ts.URL, session.ID, 0, 3)

	assertSeqs(t, events, []int64{1, 2, 3})
	assertTypes(t, events, []string{
		protocol.EventSessionStarted,
		protocol.EventOutput,
		protocol.EventAwaitingApproval,
	})
}

func TestStreamReconnectFromCursorReturnsOnlyLaterEvents(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "continue"})

	events := streamEvents(t, ts.URL, session.ID, 3, 2)
	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventInputReceived, protocol.EventOutput})
}

func TestApprovalLifecycleEmitsResolvedEvent(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/approvals/approval_bootstrap", protocol.ResolveApprovalRequest{Action: "approve"})

	events := streamEvents(t, ts.URL, session.ID, 3, 2)
	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventApprovalResolved, protocol.EventOutput})
}

func TestCreateSessionRejectsUnknownAgent(t *testing.T) {
	ts := newTestServer(t)

	body, err := json.Marshal(protocol.CreateSessionRequest{Repo: ".", Agent: "missing"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %s", resp.Status)
	}
}

func TestFakeAgentInputDoneEmitsDone(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "done"})

	events := streamEvents(t, ts.URL, session.ID, 3, 3)
	assertSeqs(t, events, []int64{4, 5, 6})
	assertTypes(t, events, []string{protocol.EventInputReceived, protocol.EventOutput, protocol.EventDone})
}

func TestInputForInactiveSessionReturnsConflict(t *testing.T) {
	store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "tethon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	session, err := store.CreateSession(context.Background(), sessionlog.CreateSessionParams{
		Repo:   ".",
		Agent:  "fake",
		Status: "running",
	})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(store).Handler())
	t.Cleanup(ts.Close)

	body, err := json.Marshal(protocol.InputRequest{Text: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/sessions/"+session.ID+"/input", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409, got %s", resp.Status)
	}
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "tethon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	ts := httptest.NewServer(New(store).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func postSession(t *testing.T, baseURL string) protocol.CreateSessionResponse {
	t.Helper()
	var resp protocol.CreateSessionResponse
	postJSONInto(t, baseURL+"/sessions", protocol.CreateSessionRequest{Repo: ".", Agent: "fake"}, &resp)
	return resp
}

func postJSON(t *testing.T, url string, value any) {
	t.Helper()
	var out any
	postJSONInto(t, url, value, &out)
}

func postJSONInto(t *testing.T, url string, value any, out any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s returned %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func streamEvents(t *testing.T, baseURL, sessionID string, from int64, count int) []protocol.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sessions/"+sessionID+"/stream?from="+strconvInt(from), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream returned %s", resp.Status)
	}

	var events []protocol.Event
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event protocol.Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
		if len(events) == count {
			cancel()
			return events
		}
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
	t.Fatalf("expected %d stream events, got %d", count, len(events))
	return nil
}

func assertSeqs(t *testing.T, events []protocol.Event, want []int64) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d", len(want), len(events))
	}
	for i := range want {
		if events[i].Seq != want[i] {
			t.Fatalf("event %d seq = %d, want %d", i, events[i].Seq, want[i])
		}
	}
}

func assertTypes(t *testing.T, events []protocol.Event, want []string) {
	t.Helper()
	if len(events) != len(want) {
		t.Fatalf("expected %d events, got %d", len(want), len(events))
	}
	for i := range want {
		if events[i].Type != want[i] {
			t.Fatalf("event %d type = %s, want %s", i, events[i].Type, want[i])
		}
	}
}

func strconvInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
