package codex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func clientPair(t *testing.T) (*Client, *websocket.Conn) {
	t.Helper()

	accepted := make(chan *websocket.Conn, 1)
	stop := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()

		accepted <- conn
		<-stop
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		close(stop)
		server.Close()
		t.Fatal(err)
	}

	client := newClient(conn)
	t.Cleanup(func() {
		client.Close()
		close(stop)
		server.Close()
	})

	return client, <-accepted
}

func TestInterleavedRepliesAndNativeRequests(t *testing.T) {
	client, server := clientPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errorsCh := make(chan error, 2)
	for _, method := range []string{"first", "second"} {
		go func() {
			var result string
			err := client.Call(ctx, method, struct{}{}, &result)
			if err == nil && result != method {
				err = errors.New("reply reached the wrong request")
			}
			errorsCh <- err
		}()
	}

	var first, second Message
	if err := wsjson.Read(ctx, server, &first); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Read(ctx, server, &second); err != nil {
		t.Fatal(err)
	}

	// A server request can use the same ID as a client request. Method presence
	// distinguishes them. It must remain pending until the user responds.
	if err := wsjson.Write(ctx, server, Message{
		ID:     first.ID,
		Method: "item/tool/requestUserInput",
		Params: json.RawMessage(`{"questions":[]}`),
	}); err != nil {
		t.Fatal(err)
	}

	for _, request := range []Message{second, first} {
		result, _ := json.Marshal(request.Method)
		if err := wsjson.Write(ctx, server, Message{ID: request.ID, Result: result}); err != nil {
			t.Fatal(err)
		}
	}

	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}

	select {
	case request := <-client.Messages():
		if request.Method != "item/tool/requestUserInput" || string(request.ID) != string(first.ID) {
			t.Fatalf("native request changed: %+v", request)
		}
		if err := client.Respond(ctx, request.ID, map[string]any{"answers": map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server request was lost")
	}

	var reply Message
	if err := wsjson.Read(ctx, server, &reply); err != nil {
		t.Fatal(err)
	}
	if string(reply.Result) != `{"answers":{}}` || string(reply.ID) != string(first.ID) {
		t.Fatalf("native answer changed: %+v", reply)
	}
}

func TestUnacknowledgedActionHasUncertainDelivery(t *testing.T) {
	client, server := clientPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	actionCtx, actionCancel := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { result <- client.Call(actionCtx, "turn/start", struct{}{}, nil) }()
	var request Message
	if err := wsjson.Read(ctx, server, &request); err != nil {
		t.Fatal(err)
	}

	actionCancel()
	var uncertain *DeliveryError
	if err := <-result; !errors.As(err, &uncertain) || !errors.Is(err, context.Canceled) {
		t.Fatalf("an executed but unacknowledged action needs reconciliation: %v", err)
	}

	// A late reply must not block later request replies.
	if err := wsjson.Write(ctx, server, Message{ID: request.ID, Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}

	go func() { result <- client.Call(ctx, "thread/read", struct{}{}, nil) }()
	if err := wsjson.Read(ctx, server, &request); err != nil {
		t.Fatal(err)
	}
	if err := wsjson.Write(ctx, server, Message{ID: request.ID, Result: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestDisconnectReleasesPendingAction(t *testing.T) {
	client, server := clientPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() { result <- client.Call(ctx, "turn/start", struct{}{}, nil) }()
	var request Message
	if err := wsjson.Read(ctx, server, &request); err != nil {
		t.Fatal(err)
	}

	server.CloseNow()
	select {
	case err := <-result:
		var uncertain *DeliveryError
		if !errors.As(err, &uncertain) {
			t.Fatalf("disconnect hid uncertain delivery: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("pending call did not end after disconnection")
	}
}

func TestSlowConsumerFailsConnectionWithoutDroppingRequests(t *testing.T) {
	client, server := clientPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for range cap(client.messages) + 1 {
		if err := wsjson.Write(ctx, server, Message{Method: "item/agentMessage/delta", Params: json.RawMessage(`{"delta":"x"}`)}); err != nil {
			t.Fatal(err)
		}
	}

	select {
	case <-client.Done():
		if !strings.Contains(client.Err().Error(), "reconcile") {
			t.Fatal(client.Err())
		}
	case <-ctx.Done():
		t.Fatal("full event queue blocked the response reader")
	}
}
