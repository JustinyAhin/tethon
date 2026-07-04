package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"tethon/internal/protocol"
)

func TestExpoPushSenderSendsChunksWithOptionalAccessToken(t *testing.T) {
	var sizes []int
	var authHeaders []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		var messages []expoPushMessage
		if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
			t.Fatal(err)
		}
		sizes = append(sizes, len(messages))
		tickets := make([]expoPushTicket, len(messages))
		for i := range tickets {
			tickets[i] = expoPushTicket{Status: "ok", ID: "ticket-" + strconv.Itoa(i)}
		}
		writeJSON(w, http.StatusOK, expoPushResponse{Data: tickets})
	}))
	t.Cleanup(ts.Close)

	devices := make([]ExpoDevice, 101)
	for i := range devices {
		devices[i] = ExpoDevice{
			DeviceID:  "phone-" + strconv.Itoa(i),
			PushToken: "ExpoPushToken[" + strconv.Itoa(i) + "]",
		}
	}
	sender := NewExpoPushSender(ExpoPushConfig{
		Endpoint:    ts.URL,
		AccessToken: "expo-secret",
		Client:      ts.Client(),
	})

	resp, err := sender.SendTest(context.Background(), devices, protocol.PushTestRequest{Title: "Hello", Body: "World"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Registered != 101 || resp.Attempted != 101 || resp.Accepted != 101 || resp.Rejected != 0 {
		t.Fatalf("summary = %+v", resp)
	}
	if len(sizes) != 2 || sizes[0] != 100 || sizes[1] != 1 {
		t.Fatalf("chunk sizes = %#v, want 100 and 1", sizes)
	}
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer expo-secret" || authHeaders[1] != "Bearer expo-secret" {
		t.Fatalf("auth headers = %#v", authHeaders)
	}
}

func TestExpoPushSenderMapsTicketRejections(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, expoPushResponse{Data: []expoPushTicket{
			{Status: "ok", ID: "ticket-1"},
			{
				Status:  "error",
				Message: "not registered",
				Details: expoPushErrorInfo{
					Error: expoDeviceNotRegister,
				},
			},
		}})
	}))
	t.Cleanup(ts.Close)

	sender := NewExpoPushSender(ExpoPushConfig{Endpoint: ts.URL, Client: ts.Client()})
	resp, err := sender.SendTest(context.Background(), []ExpoDevice{
		{DeviceID: "phone-1", PushToken: "ExpoPushToken[1]"},
		{DeviceID: "phone-2", PushToken: "ExpoPushToken[2]"},
	}, protocol.PushTestRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 1 || resp.Rejected != 1 {
		t.Fatalf("summary = %+v", resp)
	}
	if len(resp.Rejections) != 1 || resp.Rejections[0].DeviceID != "phone-2" || resp.Rejections[0].Code != expoDeviceNotRegister {
		t.Fatalf("rejections = %+v", resp.Rejections)
	}
}

func TestApprovalPushMessageCarriesSafeDecisionContext(t *testing.T) {
	message := approvalPushMessage("ExpoPushToken[secret]", protocol.PushApprovalTestRequest{
		HostID:    "mac",
		SessionID: "ses_1",
		RequestID: "approval_1",
		Agent:     "claude-cli",
		Repo:      "/Users/alice/client/.env.local",
		ToolName:  "Bash",
		Command:   `OPENAI_API_KEY=sk-test bash -lc "curl -H 'Authorization: Bearer abc' https://example.com"`,
	})

	visiblePayload := struct {
		Title      string         `json:"title"`
		Body       string         `json:"body"`
		Data       map[string]any `json:"data"`
		CategoryID string         `json:"categoryId"`
		ChannelID  string         `json:"channelId"`
	}{Title: message.Title, Body: message.Body, Data: message.Data, CategoryID: message.CategoryID, ChannelID: message.ChannelID}
	raw, err := json.Marshal(visiblePayload)
	if err != nil {
		t.Fatal(err)
	}
	for _, leaked := range []string{"sk-test", "Bearer abc", "ExpoPushToken[secret]", "/Users/alice/client/.env.local"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("approval push payload leaked %q in %s", leaked, raw)
		}
	}
	if message.Title != "Approval needed" {
		t.Fatalf("title = %q, want Approval needed", message.Title)
	}
	if message.CategoryID != approvalNotificationCategoryID {
		t.Fatalf("categoryId = %q, want %q", message.CategoryID, approvalNotificationCategoryID)
	}
	if message.ChannelID != approvalNotificationChannelID {
		t.Fatalf("channelId = %q, want %q", message.ChannelID, approvalNotificationChannelID)
	}
	if !strings.Contains(message.Body, "claude-cli/Bash needs approval") {
		t.Fatalf("body = %q, want safe agent/tool summary", message.Body)
	}
	if message.Data["kind"] != "approval_requested" || message.Data["host_id"] != "mac" || message.Data["session_id"] != "ses_1" || message.Data["request_id"] != "approval_1" {
		t.Fatalf("data = %#v, want stable approval identifiers", message.Data)
	}
	if message.Data["command_redacted"] != true {
		t.Fatalf("data = %#v, want command_redacted true", message.Data)
	}
}

func TestExpoPushSenderSendsApprovalPayload(t *testing.T) {
	var messages []expoPushMessage
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&messages); err != nil {
			t.Fatal(err)
		}
		writeJSON(w, http.StatusOK, expoPushResponse{Data: []expoPushTicket{{Status: "ok", ID: "ticket-1"}}})
	}))
	t.Cleanup(ts.Close)

	sender := NewExpoPushSender(ExpoPushConfig{Endpoint: ts.URL, Client: ts.Client()})
	resp, err := sender.SendApproval(context.Background(), []ExpoDevice{{DeviceID: "phone-1", PushToken: "ExpoPushToken[1]"}}, protocol.PushApprovalTestRequest{
		HostID:    "mac",
		SessionID: "ses_1",
		RequestID: "approval_1",
		Agent:     "claude-cli",
		ToolName:  "Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Accepted != 1 || len(messages) != 1 {
		t.Fatalf("resp = %+v messages = %#v", resp, messages)
	}
	if messages[0].Data["kind"] != "approval_requested" {
		t.Fatalf("message data = %#v, want approval_requested kind", messages[0].Data)
	}
	if messages[0].CategoryID != approvalNotificationCategoryID || messages[0].ChannelID != approvalNotificationChannelID {
		t.Fatalf("message category/channel = %q/%q, want %q/%q", messages[0].CategoryID, messages[0].ChannelID, approvalNotificationCategoryID, approvalNotificationChannelID)
	}
}

func TestExpoPushSenderReturnsExpoRequestError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []map[string]string{{"message": "bad payload"}},
		})
	}))
	t.Cleanup(ts.Close)

	sender := NewExpoPushSender(ExpoPushConfig{Endpoint: ts.URL, Client: ts.Client()})
	_, err := sender.SendTest(context.Background(), []ExpoDevice{{DeviceID: "phone-1", PushToken: "ExpoPushToken[1]"}}, protocol.PushTestRequest{})
	if err == nil {
		t.Fatal("SendTest succeeded, want error")
	}
}
