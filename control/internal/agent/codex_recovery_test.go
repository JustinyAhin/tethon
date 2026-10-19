package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"

	"tethon/internal/agent"
	"tethon/internal/codex"
	"tethon/internal/protocol"
	"tethon/internal/server"
	"tethon/internal/sessionlog"
)

func TestCodexNewThreadSendsFirstInputBeforeReadingHistory(t *testing.T) {
	engine := newRecoveryEngine(t)
	engine.requireMaterialization = true
	host, sessionID, _ := newRecoveryHost(t, engine, protocol.CreateSessionRequest{
		Agent:     "codex-cli",
		Repo:      engine.root,
		RequestID: uuid.NewString(),
		Prompt:    "first native input",
	})
	snapshot := recoverySnapshot(t, host, sessionID)
	if snapshot.State.ConnectionState != protocol.CodexConnected {
		t.Fatalf("new native thread did not become usable: %+v", snapshot.State)
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.inputs != 1 || engine.historyReadsBeforeInput != 0 {
		t.Fatalf("unmaterialized native history blocked initial input: inputs=%d early-history=%d", engine.inputs, engine.historyReadsBeforeInput)
	}
}

func TestCodexLostInputAcknowledgementDoesNotSendTwice(t *testing.T) {
	engine := newRecoveryEngine(t)
	engine.loseAcknowledgement = true
	host, sessionID, _ := newRecoveryHost(t, engine)
	before := recoverySnapshot(t, host, sessionID)
	request := protocol.InputRequest{
		Text:      "one input",
		Mode:      "start",
		RequestID: uuid.NewString(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	requestData, _ := json.Marshal(request)
	post, _ := http.NewRequestWithContext(ctx, http.MethodPost, host+"/sessions/"+sessionID+"/input", bytes.NewReader(requestData))
	response, err := http.DefaultClient.Do(post)
	if response != nil {
		response.Body.Close()
	}
	if err == nil {
		t.Fatal("the lost native acknowledgement did not reach the HTTP timeout")
	}

	if status := recoveryAction(t, host, "/sessions/"+sessionID+"/input", request); status != http.StatusGatewayTimeout {
		t.Fatalf("unconfirmed input became retryable: status=%d", status)
	}
	engine.disconnect()
	waitRecoverySnapshot(t, host, sessionID, func(snapshot protocol.CodexSessionResponse) bool {
		return snapshot.State.ConnectionState == protocol.CodexConnected && snapshot.State.ConnectionID != before.State.ConnectionID
	})

	if status := recoveryAction(t, host, "/sessions/"+sessionID+"/input", request); status != http.StatusAccepted {
		t.Fatalf("native history did not reconcile the action receipt: status=%d", status)
	}
	request.Text = "different input"
	if status := recoveryAction(t, host, "/sessions/"+sessionID+"/input", request); status != http.StatusConflict {
		t.Fatalf("changed input reused a transport ID: status=%d", status)
	}

	steer := protocol.InputRequest{
		Text:      "one follow-up",
		Mode:      "steer",
		TurnID:    "native-turn",
		RequestID: uuid.NewString(),
	}
	for range 2 {
		if status := recoveryAction(t, host, "/sessions/"+sessionID+"/input", steer); status != http.StatusAccepted {
			t.Fatalf("confirmed steer receipt was not reusable: status=%d", status)
		}
	}

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.inputs != 2 {
		t.Fatalf("native writes=%d, want one original input and one steer", engine.inputs)
	}
}

func TestCodexReconnectReissuesOnlyNativePendingRequests(t *testing.T) {
	engine := newRecoveryEngine(t)
	engine.pending = true
	host, sessionID, store := newRecoveryHost(t, engine)
	before := recoverySnapshot(t, host, sessionID)
	if len(before.PendingRequests) != 1 {
		t.Fatalf("initial native request was not hydrated before controls: %+v", before)
	}

	engine.disconnect()
	after := waitRecoverySnapshot(t, host, sessionID, func(snapshot protocol.CodexSessionResponse) bool {
		return snapshot.State.ConnectionState == protocol.CodexConnected &&
			snapshot.State.ConnectionID != before.State.ConnectionID &&
			len(snapshot.PendingRequests) == 1
	})

	stored, err := store.ListPendingCodexRequests(context.Background(), sessionID)
	if err != nil || len(stored) != 1 || stored[0].ConnectionID != after.State.ConnectionID {
		t.Fatalf("old native generation stayed actionable: pending=%+v error=%v", stored, err)
	}

	answer := protocol.CodexRespondRequest{
		RequestKey:   before.PendingRequests[0].RequestKey,
		ConnectionID: before.State.ConnectionID,
		Result:       json.RawMessage(`{"decision":"decline"}`),
	}
	if status := recoveryAction(t, host, "/sessions/"+sessionID+"/requests/respond", answer); status != http.StatusConflict {
		t.Fatalf("an old-generation approval reached the native engine: status=%d", status)
	}
	answer.ConnectionID = after.State.ConnectionID
	if status := recoveryAction(t, host, "/sessions/"+sessionID+"/requests/respond", answer); status != http.StatusAccepted {
		t.Fatalf("the real native reissue was not answerable: status=%d", status)
	}
	waitRecoverySnapshot(t, host, sessionID, func(snapshot protocol.CodexSessionResponse) bool {
		return len(snapshot.PendingRequests) == 0 && snapshot.Seq > after.Seq
	})

	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.answers != 1 || engine.inputs != 0 {
		t.Fatalf("recovery created extra effects: answers=%d inputs=%d", engine.answers, engine.inputs)
	}
}

func TestCodexArchivedConversationSurvivesHostRecoveryWithoutResume(t *testing.T) {
	engine := newRecoveryEngine(t)
	store, err := sessionlog.Open(context.Background(), filepath.Join(engine.root, "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.CreateSession(context.Background(), sessionlog.CreateSessionParams{Agent: "codex-cli", Repo: engine.root, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetCodexState(context.Background(), session.ID, protocol.CodexSessionState{
		ThreadID:          "native-thread",
		ConnectionID:      "previous-host",
		ConnectionState:   protocol.CodexDisconnected,
		ConversationState: protocol.CodexConversationArchived,
		TurnState:         protocol.CodexTurnCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	host := server.NewWithConfig(store, server.Config{
		Agents:    agent.NewRegistry(agent.NewCodex(agent.CodexConfig{Binary: engine.binary})),
		RepoRoots: []string{engine.root},
	})
	if err := host.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(host.Handler())
	t.Cleanup(httpServer.Close)

	snapshot := recoverySnapshot(t, httpServer.URL, session.ID)
	if snapshot.State.ConnectionState != protocol.CodexConnected || snapshot.State.ConversationState != protocol.CodexConversationArchived {
		t.Fatalf("host recovery lost the archived conversation: %+v", snapshot.State)
	}
	engine.mu.Lock()
	defer engine.mu.Unlock()
	if engine.resumes != 0 || engine.inputs != 0 {
		t.Fatalf("archived recovery resumed native work: resumes=%d inputs=%d", engine.resumes, engine.inputs)
	}
}

type recoveryEngine struct {
	mu                      sync.Mutex
	connections             []*websocket.Conn
	inputs                  int
	answers                 int
	resumes                 int
	lastClientID            string
	pending                 bool
	loseAcknowledgement     bool
	requireMaterialization  bool
	historyReadsBeforeInput int
	root                    string
	binary                  string
}

func newRecoveryEngine(t *testing.T) *recoveryEngine {
	t.Helper()
	root, err := os.MkdirTemp("/tmp", "tethon-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(root, "native.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}

	engine := &recoveryEngine{root: root, binary: filepath.Join(root, "codex")}
	version, _ := json.Marshal(map[string]string{
		"status":           "running",
		"socketPath":       socket,
		"cliVersion":       "0.163.0",
		"appServerVersion": "0.163.0",
	})
	script := "#!/bin/sh\nprintf '%s\\n' '" + string(version) + "'\n"
	if err := os.WriteFile(engine.binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	httpServer := &http.Server{Handler: http.HandlerFunc(engine.serve)}
	go func() { _ = httpServer.Serve(listener) }()
	t.Cleanup(func() {
		engine.disconnect()
		_ = httpServer.Close()
		_ = os.RemoveAll(root)
	})
	return engine
}

func (engine *recoveryEngine) disconnect() {
	engine.mu.Lock()
	connections := append([]*websocket.Conn{}, engine.connections...)
	engine.mu.Unlock()

	for _, connection := range connections {
		_ = connection.CloseNow()
	}
}

func (engine *recoveryEngine) serve(w http.ResponseWriter, r *http.Request) {
	connection, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer connection.CloseNow()

	engine.mu.Lock()
	engine.connections = append(engine.connections, connection)
	engine.mu.Unlock()

	for {
		var request codex.Message
		if err := wsjson.Read(r.Context(), connection, &request); err != nil {
			return
		}
		if request.Method == "initialized" {
			continue
		}

		engine.mu.Lock()
		if request.Method == "" {
			engine.answers++
			engine.pending = false
			engine.mu.Unlock()
			_ = wsjson.Write(r.Context(), connection, map[string]any{
				"method": "serverRequest/resolved",
				"params": map[string]any{
					"threadId":  "native-thread",
					"requestId": 1,
				},
			})
			continue
		}

		var result any = map[string]any{}
		switch request.Method {
		case "thread/read":
			result = map[string]any{
				"thread": map[string]any{
					"id":     "native-thread",
					"cwd":    engine.root,
					"status": map[string]string{"type": "idle"},
				},
			}
		case "thread/start", "thread/resume":
			if request.Method == "thread/resume" {
				engine.resumes++
			}

			result = map[string]any{
				"thread": map[string]any{
					"id":     "native-thread",
					"status": map[string]string{"type": "active"},
				},
				"model": "native-model",
			}
			if engine.pending {
				_ = wsjson.Write(r.Context(), connection, map[string]any{
					"method": "item/commandExecution/requestApproval",
					"id":     1,
					"params": map[string]any{
						"threadId":           "native-thread",
						"turnId":             "native-turn",
						"itemId":             "native-tool",
						"availableDecisions": []string{"accept", "decline"},
					},
				})
			}
		case "turn/start", "turn/steer":
			var input struct {
				ClientID string `json:"clientUserMessageId"`
			}
			_ = json.Unmarshal(request.Params, &input)
			engine.lastClientID = input.ClientID
			engine.inputs++
			if engine.loseAcknowledgement {
				engine.loseAcknowledgement = false
				engine.mu.Unlock()
				continue
			}
			result = map[string]any{"turn": map[string]string{"id": "native-turn", "status": "inProgress"}}
		case "thread/turns/list":
			if engine.requireMaterialization && engine.inputs == 0 {
				engine.historyReadsBeforeInput++
				engine.mu.Unlock()
				_ = wsjson.Write(r.Context(), connection, map[string]any{
					"id": request.ID,
					"error": map[string]any{
						"code":    -32600,
						"message": "thread not materialized before first input",
					},
				})
				continue
			}

			turns := []any{}
			if engine.lastClientID != "" || engine.pending {
				turns = append(turns, map[string]any{
					"id":     "native-turn",
					"status": "inProgress",
					"items": []any{map[string]any{
						"type":     "userMessage",
						"id":       "native-user-item",
						"clientId": engine.lastClientID,
						"content":  []any{},
					}},
				})
			}
			result = map[string]any{"data": turns, "nextCursor": nil}
		}
		engine.mu.Unlock()

		if err := wsjson.Write(r.Context(), connection, map[string]any{"id": request.ID, "result": result}); err != nil {
			return
		}
	}
}

func newRecoveryHost(t *testing.T, engine *recoveryEngine, createRequests ...protocol.CreateSessionRequest) (string, string, *sessionlog.Store) {
	t.Helper()
	store, err := sessionlog.Open(context.Background(), filepath.Join(engine.root, "delivery.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	host := server.NewWithConfig(store, server.Config{
		Agents:    agent.NewRegistry(agent.NewCodex(agent.CodexConfig{Binary: engine.binary})),
		RepoRoots: []string{engine.root},
	})
	httpServer := httptest.NewServer(host.Handler())
	t.Cleanup(httpServer.Close)

	request := protocol.CreateSessionRequest{
		Agent:          "codex-cli",
		Repo:           engine.root,
		NativeThreadID: "native-thread",
		RequestID:      uuid.NewString(),
	}
	if len(createRequests) > 0 {
		request = createRequests[0]
	}

	data, _ := json.Marshal(request)
	response, err := http.Post(httpServer.URL+"/sessions", "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("create native fixture: %d %s", response.StatusCode, body)
	}

	var created protocol.CreateSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	return httpServer.URL, created.ID, store
}

func recoveryAction(t *testing.T, host, path string, value any) int {
	t.Helper()
	data, _ := json.Marshal(value)
	response, err := http.Post(host+path, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

func recoverySnapshot(t *testing.T, host, sessionID string) protocol.CodexSessionResponse {
	t.Helper()
	response, err := http.Get(host + "/sessions/" + sessionID + "/native")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()

	var snapshot protocol.CodexSessionResponse
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func waitRecoverySnapshot(t *testing.T, host, sessionID string, ready func(protocol.CodexSessionResponse) bool) protocol.CodexSessionResponse {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot := recoverySnapshot(t, host, sessionID)
		if ready(snapshot) {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("native fixture did not reconcile: %+v", recoverySnapshot(t, host, sessionID)))
	return protocol.CodexSessionResponse{}
}
