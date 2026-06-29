package hub

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
	hostserver "tethon/internal/server"
	"tethon/internal/sessionlog"
)

func TestRegisterAndListHosts(t *testing.T) {
	hub := New(time.Second)
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	registerHost(t, ts.URL, protocol.RegisterHostRequest{
		HostID:    "mac",
		Name:      "Mac",
		URL:       "http://127.0.0.1:39181",
		Agents:    []string{"fake"},
		RepoRoots: []string{"."},
	})

	var resp struct {
		Hosts []protocol.HostSummary `json:"hosts"`
	}
	getJSON(t, ts.URL+"/hosts", &resp)
	if len(resp.Hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(resp.Hosts))
	}
	if resp.Hosts[0].HostID != "mac" || !resp.Hosts[0].Online {
		t.Fatalf("unexpected host summary: %+v", resp.Hosts[0])
	}
}

func TestHubRequiresBearerTokenWhenConfigured(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "", http.StatusUnauthorized)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "wrong", http.StatusUnauthorized)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "secret", http.StatusOK)
}

func TestHubBearerTokenAllowsSessionLifecycle(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hubTS := httptest.NewServer(hub.Handler())
	t.Cleanup(hubTS.Close)
	hostTS := newHost(t)

	registerHostAuth(t, hubTS.URL, "secret", protocol.RegisterHostRequest{
		HostID:    "mac",
		Name:      "Mac",
		URL:       hostTS.URL,
		Agents:    []string{"fake"},
		RepoRoots: []string{"."},
	})

	session := createSessionAuth(t, hubTS.URL, "secret", protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	postJSONAuth(t, hubTS.URL+"/sessions/"+session.ID+"/input", "secret", protocol.InputRequest{Text: "continue"})
	events := streamEventsAuth(t, hubTS.URL, session.ID, "secret", 3, 2)

	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventInputReceived, protocol.EventOutput})
}

func TestHubCreatesSessionAndStreamsReplayThroughHost(t *testing.T) {
	hubTS, hostTS := newHubAndHost(t, time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{
		HostID:    "mac",
		Name:      "Mac",
		URL:       hostTS.URL,
		Agents:    []string{"fake"},
		RepoRoots: []string{"."},
	})

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	if !strings.HasPrefix(session.ID, "mac/ses_") {
		t.Fatalf("expected global session id, got %q", session.ID)
	}

	postJSON(t, hubTS.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "continue"})
	events := streamEvents(t, hubTS.URL, session.ID, 3, 2)

	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventInputReceived, protocol.EventOutput})
}

func TestHubListsSessionsAcrossOnlineHosts(t *testing.T) {
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	mac := newHost(t)
	vps := newHost(t)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    mac.URL,
		Agents: []string{"fake"},
	})
	registerHost(t, hub.URL, protocol.RegisterHostRequest{
		HostID: "vps",
		Name:   "VPS",
		URL:    vps.URL,
		Agents: []string{"fake"},
	})

	done := createSession(t, hub.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	postJSON(t, hub.URL+"/sessions/"+done.ID+"/input", protocol.InputRequest{Text: "done"})
	running := createSession(t, hub.URL, protocol.CreateSessionRequest{HostID: "vps", Repo: ".", Agent: "fake"})

	var resp protocol.ListSessionsResponse
	getJSON(t, hub.URL+"/sessions", &resp)
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
	byID := map[string]protocol.SessionSummary{resp.Sessions[0].ID: resp.Sessions[0], resp.Sessions[1].ID: resp.Sessions[1]}
	if byID[done.ID].HostID != "mac" || byID[done.ID].Status != "done" || byID[done.ID].LastSeq != 6 {
		t.Fatalf("done summary = %+v, want mac done last_seq 6", byID[done.ID])
	}
	if byID[running.ID].HostID != "vps" || byID[running.ID].Status != "running" || byID[running.ID].LastSeq != 3 {
		t.Fatalf("running summary = %+v, want vps running last_seq 3", byID[running.ID])
	}
	if !strings.HasPrefix(done.ID, "mac/") || !strings.HasPrefix(running.ID, "vps/") {
		t.Fatalf("expected compound ids, got %q and %q", done.ID, running.ID)
	}
}

func TestHubListSessionsOmitsOfflineHosts(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	hub := New(time.Second)
	hub.now = func() time.Time { return now }
	hubTS := httptest.NewServer(hub.Handler())
	t.Cleanup(hubTS.Close)
	host := newHost(t)

	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{HostID: "old", URL: "http://127.0.0.1:1"})
	now = now.Add(2 * time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{HostID: "mac", URL: host.URL})
	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})

	var resp protocol.ListSessionsResponse
	getJSON(t, hubTS.URL+"/sessions", &resp)
	if len(resp.Sessions) != 1 {
		t.Fatalf("expected 1 online-host session, got %d", len(resp.Sessions))
	}
	if resp.Sessions[0].ID != session.ID || resp.Sessions[0].HostID != "mac" {
		t.Fatalf("unexpected session summary: %+v", resp.Sessions[0])
	}
}

func TestHubListSessionsFailsWhenOnlineHostFanoutFails(t *testing.T) {
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	badHost := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusInternalServerError, "boom")
	}))
	t.Cleanup(badHost.Close)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{HostID: "bad", URL: badHost.URL})

	resp, err := http.Get(hub.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502, got %s", resp.Status)
	}
}

func TestHubProxiesApprovalResolution(t *testing.T) {
	hubTS, hostTS := newHubAndHost(t, time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    hostTS.URL,
		Agents: []string{"fake"},
	})

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	startupEvents := streamEvents(t, hubTS.URL, session.ID, 0, 3)
	requestID := approvalRequestID(t, startupEvents[2])

	postJSON(t, hubTS.URL+"/sessions/"+session.ID+"/approvals/"+requestID, protocol.ResolveApprovalRequest{Action: "approve"})
	events := streamEvents(t, hubTS.URL, session.ID, 3, 2)

	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventApprovalResolved, protocol.EventOutput})
}

func TestHubProxiesCancellation(t *testing.T) {
	hubTS, hostTS := newHubAndHost(t, time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    hostTS.URL,
		Agents: []string{"fake"},
	})

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	postJSON(t, hubTS.URL+"/sessions/"+session.ID+"/cancel", map[string]string{})
	events := streamEvents(t, hubTS.URL, session.ID, 3, 1)

	assertSeqs(t, events, []int64{4})
	assertTypes(t, events, []string{protocol.EventDone})
}

func TestHubRejectsOfflineHost(t *testing.T) {
	now := time.Date(2026, 6, 28, 12, 0, 0, 0, time.UTC)
	hub := New(time.Second)
	hub.now = func() time.Time { return now }
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	registerHost(t, ts.URL, protocol.RegisterHostRequest{HostID: "mac", URL: "http://127.0.0.1:39181"})
	now = now.Add(2 * time.Second)

	body, err := json.Marshal(protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(ts.URL+"/sessions", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %s", resp.Status)
	}
}

func newHubAndHost(t *testing.T, timeout time.Duration) (*httptest.Server, *httptest.Server) {
	t.Helper()
	hubTS := httptest.NewServer(New(timeout).Handler())
	t.Cleanup(hubTS.Close)

	store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "hostd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	hostTS := httptest.NewServer(hostserver.New(store).Handler())
	t.Cleanup(hostTS.Close)

	return hubTS, hostTS
}

func newHost(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "hostd.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	ts := httptest.NewServer(hostserver.New(store).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func registerHost(t *testing.T, hubURL string, req protocol.RegisterHostRequest) {
	t.Helper()
	var out protocol.HostSummary
	postJSONInto(t, hubURL+"/hosts/register", req, &out)
}

func createSession(t *testing.T, hubURL string, req protocol.CreateSessionRequest) protocol.CreateSessionResponse {
	t.Helper()
	var out protocol.CreateSessionResponse
	postJSONInto(t, hubURL+"/sessions", req, &out)
	return out
}

func registerHostAuth(t *testing.T, hubURL, authToken string, req protocol.RegisterHostRequest) {
	t.Helper()
	var out protocol.HostSummary
	postJSONIntoAuth(t, hubURL+"/hosts/register", authToken, req, &out)
}

func createSessionAuth(t *testing.T, hubURL, authToken string, req protocol.CreateSessionRequest) protocol.CreateSessionResponse {
	t.Helper()
	var out protocol.CreateSessionResponse
	postJSONIntoAuth(t, hubURL+"/sessions", authToken, req, &out)
	return out
}

func postJSON(t *testing.T, url string, value any) {
	t.Helper()
	var out any
	postJSONInto(t, url, value, &out)
}

func postJSONAuth(t *testing.T, url, authToken string, value any) {
	t.Helper()
	var out any
	postJSONIntoAuth(t, url, authToken, value, &out)
}

func postJSONInto(t *testing.T, url string, value any, out any) {
	t.Helper()
	postJSONIntoAuth(t, url, "", value, out)
}

func postJSONIntoAuth(t *testing.T, url, authToken string, value any, out any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
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

func assertStatus(t *testing.T, method, target, authToken string, want int) {
	t.Helper()
	req, err := http.NewRequest(method, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		t.Fatalf("%s %s returned %s, want %d", method, target, resp.Status, want)
	}
}

func getJSON(t *testing.T, url string, out any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		t.Fatalf("GET %s returned %s", url, resp.Status)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func streamEvents(t *testing.T, baseURL, sessionID string, from int64, count int) []protocol.Event {
	t.Helper()
	return streamEventsAuth(t, baseURL, sessionID, "", from, count)
}

func streamEventsAuth(t *testing.T, baseURL, sessionID, authToken string, from int64, count int) []protocol.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/sessions/"+sessionID+"/stream?from="+strconvInt(from), nil)
	if err != nil {
		t.Fatal(err)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
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

func approvalRequestID(t *testing.T, event protocol.Event) string {
	t.Helper()
	if event.Type != protocol.EventAwaitingApproval {
		t.Fatalf("event type = %s, want %s", event.Type, protocol.EventAwaitingApproval)
	}
	var payload protocol.ApprovalPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequestID == "" {
		t.Fatal("approval request_id is empty")
	}
	return payload.RequestID
}

func strconvInt(value int64) string {
	return strconv.FormatInt(value, 10)
}
