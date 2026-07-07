package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"tethon/internal/protocol"
	"tethon/internal/reverse"
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
	connectReverseHost(t, ts.URL, "mac", "", http.NotFoundHandler())

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

func TestHubRegistersDeviceWithoutEchoingPushToken(t *testing.T) {
	hub := New(time.Second)
	store := newTestDeviceStore(t)
	hub.SetDeviceStore(store)
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	req := protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[secret]",
		Platform:    "ios",
		AppID:       "dev.tethon.mobile",
		AppVersion:  "1.0.0",
	}
	var resp protocol.RegisterDeviceResponse
	postJSONInto(t, ts.URL+"/devices/register", req, &resp)
	if resp.DeviceID != req.DeviceID || resp.PushService != "expo" || resp.Platform != "ios" || resp.UpdatedAt.IsZero() {
		t.Fatalf("device response = %+v", resp)
	}
	raw := postJSONRaw(t, ts.URL+"/devices/register", req, "")
	if strings.Contains(raw, req.PushToken) {
		t.Fatalf("registration response echoed token: %s", raw)
	}

	tokens, err := store.ExpoPushTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != req.PushToken {
		t.Fatalf("stored tokens = %#v", tokens)
	}
}

func TestHubDeviceRoutesValidateAndRequireAuth(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(newTestDeviceStore(t))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodPost, ts.URL+"/devices/register", "", http.StatusUnauthorized)
	assertPostStatus(t, ts.URL+"/devices/register", "secret", protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		Platform:    "ios",
	}, http.StatusBadRequest)

	postJSONAuth(t, ts.URL+"/devices/register", "secret", protocol.RegisterDeviceRequest{
		DeviceID:    "phone-1",
		PushService: "expo",
		PushToken:   "ExpoPushToken[secret]",
		Platform:    "android",
	})
	assertStatus(t, http.MethodDelete, ts.URL+"/devices/phone-1", "secret", http.StatusNoContent)
	assertStatus(t, http.MethodDelete, ts.URL+"/devices/phone-1", "secret", http.StatusNotFound)
}

func TestHubSendsTestPushAndDisablesUnregisteredDevice(t *testing.T) {
	hub := New(time.Second)
	store := newTestDeviceStore(t)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(store)
	hub.SetPushSender(fakePushSender{
		response: protocol.PushTestResponse{
			Registered: 2,
			Attempted:  2,
			Accepted:   1,
			Rejected:   1,
			Rejections: []protocol.PushTestRejection{{
				DeviceID: "phone-2",
				Code:     expoDeviceNotRegister,
				Message:  "not registered",
			}},
		},
	})
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	for _, req := range []protocol.RegisterDeviceRequest{
		{DeviceID: "phone-1", PushService: "expo", PushToken: "ExpoPushToken[1]", Platform: "ios"},
		{DeviceID: "phone-2", PushService: "expo", PushToken: "ExpoPushToken[2]", Platform: "android"},
	} {
		postJSONAuth(t, ts.URL+"/devices/register", "secret", req)
	}
	raw := postJSONRaw(t, ts.URL+"/push/test", protocol.PushTestRequest{Title: "Hello", Body: "World"}, "secret")
	if strings.Contains(raw, "ExpoPushToken") {
		t.Fatalf("test push response leaked token: %s", raw)
	}
	var resp protocol.PushTestResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Registered != 2 || resp.Accepted != 1 || resp.Rejected != 1 {
		t.Fatalf("push response = %+v", resp)
	}

	devices, err := store.ActiveExpoDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "phone-1" {
		t.Fatalf("active devices after rejection = %#v", devices)
	}
}

func TestHubSendsApprovalTestPushAndDisablesUnregisteredDevice(t *testing.T) {
	hub := New(time.Second)
	store := newTestDeviceStore(t)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(store)
	hub.SetPushSender(fakePushSender{
		approvalResponse: protocol.PushTestResponse{
			Registered: 2,
			Attempted:  2,
			Accepted:   1,
			Rejected:   1,
			Rejections: []protocol.PushTestRejection{{
				DeviceID: "phone-2",
				Code:     expoDeviceNotRegister,
				Message:  "not registered",
			}},
		},
	})
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	for _, req := range []protocol.RegisterDeviceRequest{
		{DeviceID: "phone-1", PushService: "expo", PushToken: "ExpoPushToken[1]", Platform: "ios"},
		{DeviceID: "phone-2", PushService: "expo", PushToken: "ExpoPushToken[2]", Platform: "android"},
	} {
		postJSONAuth(t, ts.URL+"/devices/register", "secret", req)
	}
	raw := postJSONRaw(t, ts.URL+"/push/approvals/test", protocol.PushApprovalTestRequest{
		HostID:    "mac",
		SessionID: "ses_1",
		RequestID: "approval_1",
		Agent:     "claude-cli",
		ToolName:  "Bash",
	}, "secret")
	if strings.Contains(raw, "ExpoPushToken") {
		t.Fatalf("approval test push response leaked token: %s", raw)
	}
	var resp protocol.PushTestResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Registered != 2 || resp.Accepted != 1 || resp.Rejected != 1 {
		t.Fatalf("push response = %+v", resp)
	}

	devices, err := store.ActiveExpoDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "phone-1" {
		t.Fatalf("active devices after rejection = %#v", devices)
	}
}

func TestHubApprovalNotifySendsPushDedupesAndDisablesUnregisteredDevice(t *testing.T) {
	hub := New(time.Second)
	store := newTestDeviceStore(t)
	sender := &recordingPushSender{
		approvalResponse: protocol.PushTestResponse{
			Registered: 2,
			Attempted:  2,
			Accepted:   1,
			Rejected:   1,
			Rejections: []protocol.PushTestRejection{{
				DeviceID: "phone-2",
				Code:     expoDeviceNotRegister,
				Message:  "not registered",
			}},
		},
	}
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(store)
	hub.SetPushSender(sender)
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	for _, req := range []protocol.RegisterDeviceRequest{
		{DeviceID: "phone-1", PushService: "expo", PushToken: "ExpoPushToken[1]", Platform: "ios"},
		{DeviceID: "phone-2", PushService: "expo", PushToken: "ExpoPushToken[2]", Platform: "android"},
	} {
		postJSONAuth(t, ts.URL+"/devices/register", "secret", req)
	}

	req := protocol.PushApprovalTestRequest{
		SessionID: "ses_1",
		RequestID: "approval_1",
		Agent:     "claude-cli",
		Repo:      "tethon",
		ToolName:  "Bash",
		Command:   "rm -rf /tmp/example",
	}
	var resp protocol.PushTestResponse
	postJSONIntoAuth(t, ts.URL+"/hosts/mac/notify/approval", "secret", req, &resp)
	if resp.Registered != 2 || resp.Accepted != 1 || resp.Rejected != 1 {
		t.Fatalf("notify push response = %+v", resp)
	}
	if sender.approvalCalls != 1 {
		t.Fatalf("approval calls = %d, want 1", sender.approvalCalls)
	}
	if got := sender.approvalRequests[0].HostID; got != "mac" {
		t.Fatalf("host_id = %q, want path host", got)
	}

	assertPostStatus(t, ts.URL+"/hosts/mac/notify/approval", "secret", req, http.StatusOK)
	if sender.approvalCalls != 1 {
		t.Fatalf("duplicate approval calls = %d, want 1", sender.approvalCalls)
	}

	req.RequestID = "approval_2"
	assertPostStatus(t, ts.URL+"/hosts/mac/notify/approval", "secret", req, http.StatusAccepted)
	if sender.approvalCalls != 2 {
		t.Fatalf("new request approval calls = %d, want 2", sender.approvalCalls)
	}

	devices, err := store.ActiveExpoDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DeviceID != "phone-1" {
		t.Fatalf("active devices after rejection = %#v", devices)
	}
}

func TestHubApprovalNotifyRequiresAuthValidContextAndPushConfig(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(newTestDeviceStore(t))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	req := protocol.PushApprovalTestRequest{
		SessionID: "ses_1",
		RequestID: "approval_1",
	}
	assertStatus(t, http.MethodPost, ts.URL+"/hosts/mac/notify/approval", "", http.StatusUnauthorized)
	assertPostStatus(t, ts.URL+"/hosts/mac/notify/approval", "secret", protocol.PushApprovalTestRequest{}, http.StatusBadRequest)
	assertPostStatus(t, ts.URL+"/hosts/mac/notify/approval", "secret", req, http.StatusServiceUnavailable)
}

func TestHubTestPushRequiresAuthAndConfiguredSender(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(newTestDeviceStore(t))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodPost, ts.URL+"/push/test", "", http.StatusUnauthorized)
	assertPostStatus(t, ts.URL+"/push/test", "secret", protocol.PushTestRequest{}, http.StatusServiceUnavailable)
}

func TestHubApprovalTestPushRequiresAuthAndValidContext(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(newTestDeviceStore(t))
	hub.SetPushSender(fakePushSender{})
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodPost, ts.URL+"/push/approvals/test", "", http.StatusUnauthorized)
	assertPostStatus(t, ts.URL+"/push/approvals/test", "secret", protocol.PushApprovalTestRequest{}, http.StatusBadRequest)
	assertPostStatus(t, ts.URL+"/push/approvals/test", "secret", protocol.PushApprovalTestRequest{
		HostID:    "mac",
		SessionID: "ses_1",
		RequestID: "approval_1",
	}, http.StatusOK)
}

func TestHubLogsHostRegistrationWithoutBearerToken(t *testing.T) {
	var logs bytes.Buffer
	hub := New(time.Second)
	hub.SetAuthToken("secret-token")
	hub.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	registerHostAuth(t, ts.URL, "secret-token", protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    "http://100.64.0.10:39181",
		Agents: []string{"fake", "claude-cli"},
		Repos: []protocol.RepoInfo{
			{ID: "one", Path: "/repos/one"},
			{ID: "two", Path: "/repos/two"},
		},
	})

	raw := logs.String()
	for _, want := range []string{
		`"msg":"hub_host_registered"`,
		`"host_id":"mac"`,
		`"name":"Mac"`,
		`"url":"http://100.64.0.10:39181"`,
		`"repo_count":2`,
		`"agent_count":2`,
		`"online":false`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
	if strings.Contains(raw, "secret-token") || strings.Contains(raw, "Authorization") || strings.Contains(raw, "Bearer") {
		t.Fatalf("logs contain bearer token: %s", raw)
	}
}

func TestHubLogsHeartbeatStatus(t *testing.T) {
	var logs bytes.Buffer
	hub := New(time.Second)
	hub.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	registerHost(t, ts.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    "http://100.64.0.10:39181",
	})
	postJSON(t, ts.URL+"/hosts/mac/heartbeat", map[string]string{})
	assertStatus(t, http.MethodPost, ts.URL+"/hosts/missing/heartbeat", "", http.StatusNotFound)

	raw := logs.String()
	for _, want := range []string{
		`"msg":"hub_host_heartbeat"`,
		`"host_id":"mac"`,
		`"url":"http://100.64.0.10:39181"`,
		`"online":false`,
		`"msg":"hub_host_heartbeat_unknown"`,
		`"host_id":"missing"`,
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("logs missing %q: %s", want, raw)
		}
	}
}

func TestHubAcceptsAdditionalBearerTokens(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthTokens("primary", "old, new\nalso-valid")
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "primary", http.StatusOK)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "old", http.StatusOK)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "new", http.StatusOK)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "also-valid", http.StatusOK)
	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "wrong", http.StatusUnauthorized)
}

func TestHubIgnoresBlankBearerTokens(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthTokens(" \n, ")
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodGet, ts.URL+"/hosts", "", http.StatusOK)
}

func TestParseAuthTokens(t *testing.T) {
	got := ParseAuthTokens(" primary ", "old,new\n also-valid \n\n")
	want := []string{"primary", "old", "new", "also-valid"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("ParseAuthTokens = %#v, want %#v", got, want)
	}
}

func TestHealthzDoesNotRequireBearerToken(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	assertStatus(t, http.MethodGet, ts.URL+"/healthz", "", http.StatusOK)
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
	connectReverseHost(t, hubTS.URL, "mac", "secret", hostTS.Config.Handler)

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
	connectReverseHost(t, hubTS.URL, "mac", "", hostTS.Config.Handler)

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
	connectReverseHost(t, hub.URL, "mac", "", mac.Config.Handler)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{
		HostID: "vps",
		Name:   "VPS",
		URL:    vps.URL,
		Agents: []string{"fake"},
	})
	connectReverseHost(t, hub.URL, "vps", "", vps.Config.Handler)

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
	connectReverseHost(t, hubTS.URL, "mac", "", host.Config.Handler)
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
	connectReverseHost(t, hub.URL, "bad", "", badHost.Config.Handler)

	resp, err := http.Get(hub.URL + "/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %s", resp.Status)
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
	connectReverseHost(t, hubTS.URL, "mac", "", hostTS.Config.Handler)

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
	connectReverseHost(t, hubTS.URL, "mac", "", hostTS.Config.Handler)

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	postJSON(t, hubTS.URL+"/sessions/"+session.ID+"/cancel", map[string]string{})
	events := streamEvents(t, hubTS.URL, session.ID, 3, 1)

	assertSeqs(t, events, []int64{4})
	assertTypes(t, events, []string{protocol.EventDone})
}

func TestHubProxiesSessionDeletion(t *testing.T) {
	hubTS, hostTS := newHubAndHost(t, time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    hostTS.URL,
		Agents: []string{"fake"},
	})
	connectReverseHost(t, hubTS.URL, "mac", "", hostTS.Config.Handler)

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})
	postJSON(t, hubTS.URL+"/sessions/"+session.ID+"/input", protocol.InputRequest{Text: "done"})

	assertStatus(t, http.MethodDelete, hubTS.URL+"/sessions/"+session.ID, "", http.StatusNoContent)

	var resp protocol.ListSessionsResponse
	getJSON(t, hubTS.URL+"/sessions", &resp)
	for _, summary := range resp.Sessions {
		if summary.ID == session.ID {
			t.Fatalf("deleted session still listed: %+v", summary)
		}
	}
}

func TestHubDeletePropagatesHostConflict(t *testing.T) {
	hubTS, hostTS := newHubAndHost(t, time.Second)
	registerHost(t, hubTS.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		Name:   "Mac",
		URL:    hostTS.URL,
		Agents: []string{"fake"},
	})
	connectReverseHost(t, hubTS.URL, "mac", "", hostTS.Config.Handler)

	session := createSession(t, hubTS.URL, protocol.CreateSessionRequest{HostID: "mac", Repo: ".", Agent: "fake"})

	assertStatus(t, http.MethodDelete, hubTS.URL+"/sessions/"+session.ID, "", http.StatusConflict)
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

func connectReverseHost(t *testing.T, hubURL, hostID, authToken string, handler http.Handler) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	target := reverseTestURL(t, hubURL, hostID)
	headers := http.Header{}
	if authToken != "" {
		headers.Set("Authorization", "Bearer "+authToken)
	}
	conn, _, err := websocket.Dial(ctx, target, &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = conn.Close(websocket.StatusNormalClosure, "test done")
	})
	go func() {
		if err := reverse.ServeRequests(ctx, conn, handler); err != nil && ctx.Err() == nil {
			t.Logf("reverse test bridge exited: %v", err)
		}
	}()
	waitForHostOnline(t, hubURL, hostID, authToken)
}

func reverseTestURL(t *testing.T, hubURL, hostID string) string {
	t.Helper()
	switch {
	case strings.HasPrefix(hubURL, "http://"):
		return "ws://" + strings.TrimPrefix(hubURL, "http://") + "/hosts/" + hostID + "/connect"
	case strings.HasPrefix(hubURL, "https://"):
		return "wss://" + strings.TrimPrefix(hubURL, "https://") + "/hosts/" + hostID + "/connect"
	default:
		t.Fatalf("unsupported hub URL %q", hubURL)
		return ""
	}
}

func waitForHostOnline(t *testing.T, hubURL, hostID, authToken string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var resp protocol.ListHostsResponse
		getJSONAuth(t, hubURL+"/hosts", authToken, &resp)
		for _, host := range resp.Hosts {
			if host.HostID == hostID && host.Online {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for host %q online", hostID)
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

func postJSONRaw(t *testing.T, url string, value any, authToken string) string {
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
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode >= 300 {
		t.Fatalf("POST %s returned %s: %s", url, resp.Status, strings.TrimSpace(string(raw)))
	}
	return string(raw)
}

func assertPostStatus(t *testing.T, url, authToken string, value any, want int) {
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
	if resp.StatusCode != want {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s returned %s, want %d: %s", url, resp.Status, want, strings.TrimSpace(string(raw)))
	}
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
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s returned %s: %s", url, resp.Status, strings.TrimSpace(string(body)))
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
	getJSONAuth(t, url, "", out)
}

func getJSONAuth(t *testing.T, url, authToken string, out any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
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

type fakePushSender struct {
	response         protocol.PushTestResponse
	approvalResponse protocol.PushTestResponse
	err              error
}

func (f fakePushSender) SendTest(context.Context, []ExpoDevice, protocol.PushTestRequest) (protocol.PushTestResponse, error) {
	return f.response, f.err
}

func (f fakePushSender) SendApproval(context.Context, []ExpoDevice, protocol.PushApprovalTestRequest) (protocol.PushTestResponse, error) {
	return f.approvalResponse, f.err
}

type recordingPushSender struct {
	response         protocol.PushTestResponse
	approvalResponse protocol.PushTestResponse
	err              error
	approvalCalls    int
	approvalRequests []protocol.PushApprovalTestRequest
}

func (f *recordingPushSender) SendTest(context.Context, []ExpoDevice, protocol.PushTestRequest) (protocol.PushTestResponse, error) {
	return f.response, f.err
}

func (f *recordingPushSender) SendApproval(_ context.Context, _ []ExpoDevice, req protocol.PushApprovalTestRequest) (protocol.PushTestResponse, error) {
	f.approvalCalls++
	f.approvalRequests = append(f.approvalRequests, req)
	return f.approvalResponse, f.err
}
