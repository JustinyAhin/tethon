package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"tethon/internal/agent"
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

func TestFakeAgentUsesUniqueApprovalIDsAcrossSessions(t *testing.T) {
	ts := newTestServer(t)

	first := postSession(t, ts.URL)
	second := postSession(t, ts.URL)

	firstEvents := streamEvents(t, ts.URL, first.ID, 0, 3)
	secondEvents := streamEvents(t, ts.URL, second.ID, 0, 3)
	firstRequestID := approvalRequestID(t, firstEvents[2])
	secondRequestID := approvalRequestID(t, secondEvents[2])

	if firstRequestID == secondRequestID {
		t.Fatalf("approval request ids should be unique, both were %q", firstRequestID)
	}
}

func TestStreamReconnectFromCursorReturnsOnlyLaterEvents(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "continue"})

	events := streamEvents(t, ts.URL, session.ID, 3, 2)
	assertSeqs(t, events, []int64{4, 5})
	assertTypes(t, events, []string{protocol.EventInputReceived, protocol.EventOutput})
}

func TestListSessionsReturnsLocalSummaries(t *testing.T) {
	store, err := sessionlog.Open(context.Background(), filepath.Join(t.TempDir(), "tethon.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
	})
	done, err := store.CreateSession(context.Background(), sessionlog.CreateSessionParams{Repo: ".", Agent: "fake", Status: "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), done.ID, protocol.EventOutput, protocol.OutputPayload{Text: "done"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), done.ID, protocol.EventDone, protocol.DonePayload{Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	running, err := store.CreateSession(context.Background(), sessionlog.CreateSessionParams{Repo: ".", Agent: "fake", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(context.Background(), running.ID, protocol.EventOutput, protocol.OutputPayload{Text: "running"}); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(store).Handler())
	t.Cleanup(ts.Close)

	var resp protocol.ListSessionsResponse
	getJSON(t, ts.URL+"/sessions", &resp)
	if len(resp.Sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(resp.Sessions))
	}
	byID := map[string]protocol.SessionSummary{resp.Sessions[0].ID: resp.Sessions[0], resp.Sessions[1].ID: resp.Sessions[1]}
	if byID[done.ID].Status != "done" || byID[done.ID].LastSeq != 2 {
		t.Fatalf("done summary = %+v, want status done and last_seq 2", byID[done.ID])
	}
	if byID[running.ID].Status != "running" || byID[running.ID].LastSeq != 1 {
		t.Fatalf("running summary = %+v, want status running and last_seq 1", byID[running.ID])
	}
	if byID[running.ID].HostID != "" || strings.Contains(byID[running.ID].ID, "/") {
		t.Fatalf("host summary should use local ids without host_id: %+v", byID[running.ID])
	}
}

func TestApprovalLifecycleEmitsResolvedEvent(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	startupEvents := streamEvents(t, ts.URL, session.ID, 0, 3)
	requestID := approvalRequestID(t, startupEvents[2])

	postJSON(t, ts.URL+"/sessions/"+session.ID+"/approvals/"+requestID, protocol.ResolveApprovalRequest{Action: "approve"})

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

func TestCancelSessionEmitsDoneAndDeactivatesSession(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/cancel", map[string]string{})

	events := streamEvents(t, ts.URL, session.ID, 3, 1)
	assertSeqs(t, events, []int64{4})
	assertTypes(t, events, []string{protocol.EventDone})

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
		t.Fatalf("expected 409 after cancel, got %s", resp.Status)
	}
}

func TestDeleteTerminalSessionRemovesSessionState(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "done"})

	if status := deleteStatus(t, ts.URL+"/sessions/"+session.ID); status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	var resp protocol.ListSessionsResponse
	getJSON(t, ts.URL+"/sessions", &resp)
	for _, summary := range resp.Sessions {
		if summary.ID == session.ID {
			t.Fatalf("deleted session still listed: %+v", summary)
		}
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/sessions/"+session.ID+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	streamResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	if streamResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected stream 404 after delete, got %s", streamResp.Status)
	}
}

func TestDeleteRejectsActiveSession(t *testing.T) {
	ts := newTestServer(t)

	session := postSession(t, ts.URL)
	if status := deleteStatus(t, ts.URL+"/sessions/"+session.ID); status != http.StatusConflict {
		t.Fatalf("expected 409, got %d", status)
	}
}

func TestServerLogsSessionLifecycleWithoutInputBody(t *testing.T) {
	var logs bytes.Buffer
	ts := newTestServerWithConfig(t, Config{
		Agents: agent.NewRegistry(agent.NewFake()),
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	})

	session := postSession(t, ts.URL)
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "secret input text"})
	postJSON(t, ts.URL+"/sessions/"+session.ID+"/cancel", map[string]string{})

	raw := logs.String()
	for _, want := range []string{"session_create_accepted", "session_agent_started", "session_input_accepted", "session_cancel_accepted"} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, "secret input text") {
		t.Fatalf("logs contain input body: %s", raw)
	}
	if !strings.Contains(raw, `"text_len":17`) {
		t.Fatalf("logs missing input length: %s", raw)
	}
}

func TestClaudeAgentSessionStreamsMappedOutput(t *testing.T) {
	runner := &serverFakeCommandRunner{output: `{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
		`{"type":"assistant","message":{"content":[{"type":"text","text":"real output"}]},"session_id":"claude_1"}` + "\n" +
		`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n"}
	ts := newTestServerWithAgents(t, agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{
		Binary: "claude",
		Runner: runner,
	})))

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
		Repo:   ".",
		Agent:  "claude-cli",
		Prompt: "say hi",
	}, &session)

	events := streamEvents(t, ts.URL, session.ID, 0, 2)
	assertTypes(t, events, []string{protocol.EventSessionStarted, protocol.EventOutput})
	var payload protocol.OutputPayload
	if err := json.Unmarshal(events[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Text != "real output" {
		t.Fatalf("output = %q, want real output", payload.Text)
	}
	var started protocol.CreateSessionRequest
	if err := json.Unmarshal(events[0].Payload, &started); err != nil {
		t.Fatal(err)
	}
	if started.PermissionMode != "default" {
		t.Fatalf("permission_mode = %q, want default", started.PermissionMode)
	}
	if len(runner.specs) != 1 || runner.specs[0].Args[len(runner.specs[0].Args)-1] != "say hi" || !strings.Contains(strings.Join(runner.specs[0].Args, " "), "--permission-mode default") {
		t.Fatalf("unexpected claude command specs: %+v", runner.specs)
	}
}

func TestCreateSessionValidatesPermissionMode(t *testing.T) {
	claudeTS := newTestServerWithAgents(t, agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{
		Binary: "claude",
		Runner: &serverFakeCommandRunner{output: `{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n"},
	})))

	if status := postJSONStatus(t, claudeTS.URL+"/sessions", protocol.CreateSessionRequest{
		Repo:           ".",
		Agent:          "claude-cli",
		Prompt:         "hi",
		PermissionMode: "invalid",
	}); status != http.StatusBadRequest {
		t.Fatalf("invalid claude permission mode returned %d, want 400", status)
	}

	fakeTS := newTestServerWithAgents(t, agent.NewRegistry(agent.NewFake()))
	if status := postJSONStatus(t, fakeTS.URL+"/sessions", protocol.CreateSessionRequest{
		Repo:           ".",
		Agent:          "fake",
		PermissionMode: "default",
	}); status != http.StatusBadRequest {
		t.Fatalf("fake permission mode returned %d, want 400", status)
	}
}

func TestCreateSessionNormalizesRepoAndRelativeCWD(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	cwd := filepath.Join(repo, "control")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	realCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	runner := &serverFakeCommandRunner{output: `{"type":"system","subtype":"init","session_id":"claude_1"}` + "\n" +
		`{"type":"result","is_error":false,"session_id":"claude_1"}` + "\n"}
	ts := newTestServerWithConfig(t, Config{
		Agents: agent.NewRegistry(agent.NewClaude(agent.ClaudeConfig{
			Binary: "claude",
			Runner: runner,
		})),
		RepoRoots: []string{root},
	})

	var session protocol.CreateSessionResponse
	postJSONInto(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
		Repo:   filepath.Join(root, ".", "repo"),
		CWD:    "control",
		Agent:  "claude-cli",
		Prompt: "say hi",
	}, &session)

	events := streamEvents(t, ts.URL, session.ID, 0, 1)
	var payload protocol.CreateSessionRequest
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Repo != realRepo || payload.CWD != "control" {
		t.Fatalf("session_started payload repo/cwd = %q/%q, want %q/control", payload.Repo, payload.CWD, realRepo)
	}
	if len(runner.specs) != 1 || runner.specs[0].Dir != realCWD {
		t.Fatalf("command dir = %+v, want %q", runner.specs, realCWD)
	}
}

func TestCreateSessionRejectsInvalidRepoAndCWD(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	repoFile := filepath.Join(root, "repo-file")
	if err := os.WriteFile(repoFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	cwdFile := filepath.Join(repo, "file")
	if err := os.WriteFile(cwdFile, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewFake()),
		RepoRoots: []string{root},
	})

	cases := []protocol.CreateSessionRequest{
		{Repo: filepath.Join(root, "missing"), Agent: "fake"},
		{Repo: repoFile, Agent: "fake"},
		{Repo: outside, Agent: "fake"},
		{Repo: repo, CWD: filepath.Join(repo, "subdir"), Agent: "fake"},
		{Repo: repo, CWD: "../outside", Agent: "fake"},
		{Repo: repo, CWD: "missing", Agent: "fake"},
		{Repo: repo, CWD: "file", Agent: "fake"},
	}
	for _, req := range cases {
		resp := postJSONStatus(t, ts.URL+"/sessions", req)
		if resp != http.StatusBadRequest {
			t.Fatalf("request %+v returned %d, want 400", req, resp)
		}
	}
}

func TestCreateSessionRejectsSymlinkCWDOutsideRepo(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "repo")
	outside := filepath.Join(root, "outside")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "linked-outside")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	ts := newTestServerWithConfig(t, Config{
		Agents:    agent.NewRegistry(agent.NewFake()),
		RepoRoots: []string{root},
	})

	resp := postJSONStatus(t, ts.URL+"/sessions", protocol.CreateSessionRequest{
		Repo:  repo,
		CWD:   "linked-outside",
		Agent: "fake",
	})
	if resp != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp)
	}
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
	return newTestServerWithAgents(t, agent.DefaultRegistry())
}

func newTestServerWithAgents(t *testing.T, agents *agent.Registry) *httptest.Server {
	t.Helper()
	return newTestServerWithConfig(t, Config{Agents: agents})
}

func newTestServerWithConfig(t *testing.T, config Config) *httptest.Server {
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
	ts := httptest.NewServer(NewWithConfig(store, config).Handler())
	t.Cleanup(ts.Close)
	return ts
}

type serverFakeCommandRunner struct {
	output string
	specs  []agent.CommandSpec
}

func (r *serverFakeCommandRunner) Start(_ context.Context, spec agent.CommandSpec) (agent.CommandProcess, error) {
	r.specs = append(r.specs, spec)
	return serverFakeProcess{stdout: strings.NewReader(r.output), stderr: strings.NewReader("")}, nil
}

type serverFakeProcess struct {
	stdout io.Reader
	stderr io.Reader
}

func (p serverFakeProcess) Stdout() io.Reader {
	return p.stdout
}

func (p serverFakeProcess) Stderr() io.Reader {
	return p.stderr
}

func (serverFakeProcess) Wait() error {
	return nil
}

func (serverFakeProcess) Interrupt() error {
	return nil
}

func (serverFakeProcess) Kill() error {
	return nil
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

func postJSONStatus(t *testing.T, url string, value any) int {
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
	return resp.StatusCode
}

func deleteStatus(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
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
