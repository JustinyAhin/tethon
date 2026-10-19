package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"tethon/internal/protocol"
)

func TestHubApprovalResolutionRequiresAuthAndMatchingHost(t *testing.T) {
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(newTestDeviceStore(t))
	hub.SetPushSender(NewExpoPushSender(ExpoPushConfig{}))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	endpoint := ts.URL + "/hosts/mac/notify/approval-resolved"
	assertStatus(t, http.MethodPost, endpoint, "", http.StatusUnauthorized)
	assertPostStatus(t, endpoint, "secret", protocol.PushApprovalResolvedRequest{}, http.StatusBadRequest)
	assertPostStatus(t, endpoint, "secret", protocol.PushApprovalResolvedRequest{
		HostID:    "other",
		SessionID: "ses_1",
		RequestID: "approval_1",
	}, http.StatusBadRequest)
	assertPostStatus(t, endpoint, "secret", protocol.PushApprovalResolvedRequest{
		SessionID: "ses_1",
		RequestID: " ",
	}, http.StatusBadRequest)
}

func TestHubApprovalResolutionSendsPathIdentityAndDisablesRejectedDevice(t *testing.T) {
	var received protocol.PushApprovalResolvedRequest
	expo := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var messages []map[string]any
		if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if len(messages) != 1 {
			t.Errorf("messages = %d, want 1", len(messages))
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		message := messages[0]
		if message["_contentAvailable"] != true {
			t.Error("resolution push will not wake the iOS task")
		}
		for _, field := range []string{"title", "body", "categoryId", "channelId"} {
			if _, ok := message[field]; ok {
				t.Errorf("resolution push can show an alert: field %s", field)
			}
		}

		data, _ := json.Marshal(message["data"])
		if err := json.Unmarshal(data, &received); err != nil {
			t.Error(err)
		}
		writeJSON(w, http.StatusOK, expoPushResponse{
			Data: []expoPushTicket{{
				Status:  "error",
				Details: expoPushErrorInfo{Error: expoDeviceNotRegister},
			}},
		})
	}))
	t.Cleanup(expo.Close)

	store := newTestDeviceStore(t)
	hub := New(time.Second)
	hub.SetAuthToken("secret")
	hub.SetDeviceStore(store)
	hub.SetPushSender(NewExpoPushSender(ExpoPushConfig{Endpoint: expo.URL, Client: expo.Client()}))
	ts := httptest.NewServer(hub.Handler())
	t.Cleanup(ts.Close)

	postJSONAuth(t, ts.URL+"/devices/register", "secret", protocol.RegisterDeviceRequest{
		DeviceID:    "phone",
		PushService: "expo",
		PushToken:   "ExpoPushToken[1]",
		Platform:    "ios",
	})
	assertPostStatus(t, ts.URL+"/hosts/mac/notify/approval-resolved", "secret", protocol.PushApprovalResolvedRequest{
		SessionID: "ses_1",
		RequestID: "approval_1",
	}, http.StatusAccepted)

	if received.HostID != "mac" || received.SessionID != "ses_1" || received.RequestID != "approval_1" {
		t.Fatalf("received = %+v, want path host and request identity", received)
	}
	devices, err := store.ActiveExpoDevices(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 0 {
		t.Fatalf("active devices = %#v, want rejected device disabled", devices)
	}
}
