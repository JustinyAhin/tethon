package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestHubRelaysOpaqueCodexRequestKeyAndNativeResult(t *testing.T) {
	received := make(chan protocol.CodexRespondRequest, 1)
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)
	registerHost(t, hub.URL, protocol.RegisterHostRequest{HostID: "mac", URL: "http://127.0.0.1:1"})
	connectReverseHost(t, hub.URL, "mac", "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions/native/requests/respond" {
			t.Errorf("host path = %q", r.URL.Path)
		}
		var request protocol.CodexRespondRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		received <- request
		writeJSON(w, http.StatusAccepted, protocol.ActionEventsResponse{Events: []protocol.Event{}})
	}))
	// Clients send an opaque string key. They never parse a native numeric ID.
	want := protocol.CodexRespondRequest{
		RequestKey: "9007199254740993", ConnectionID: "connection-2",
		Result: json.RawMessage(`{"answers":{"q1":{"answers":["Keep"]}},"futureField":{"value":1}}`),
	}
	var response protocol.ActionEventsResponse
	postJSONInto(t, hub.URL+"/sessions/mac/native/requests/respond", want, &response)
	got := <-received
	if got.RequestKey != want.RequestKey || got.ConnectionID != want.ConnectionID || string(got.Result) != string(want.Result) {
		t.Fatalf("native request changed in relay: %+v", got)
	}
	if len(got.RequestID) != 0 {
		t.Fatalf("relay invented a numeric request ID: %s", got.RequestID)
	}
}

func TestCodexHistoryDeletionReturnsHostStatus(t *testing.T) {
	hub := httptest.NewServer(New(time.Second).Handler())
	t.Cleanup(hub.Close)

	registerHost(t, hub.URL, protocol.RegisterHostRequest{
		HostID: "mac",
		URL:    "http://127.0.0.1:1",
	})
	connectReverseHost(t, hub.URL, "mac", "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/sessions/native/conversation" {
			t.Errorf("unexpected native deletion: %s %s", r.Method, r.URL.Path)
		}

		writeJSON(w, http.StatusAccepted, protocol.ActionEventsResponse{Events: []protocol.Event{}})
	}))

	request, err := http.NewRequest(http.MethodDelete, hub.URL+"/sessions/mac/native/conversation", nil)
	if err != nil {
		t.Fatal(err)
	}

	response, err := hub.Client().Do(request)
	if err != nil {
		t.Fatalf("native deletion response failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusAccepted {
		t.Fatalf("native deletion: HTTP %d", response.StatusCode)
	}
}
