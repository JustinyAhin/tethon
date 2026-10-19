package sessionlog

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"tethon/internal/protocol"
)

func TestCodexStateRequestsAndUnknownEventsSurviveReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "sessions.db")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "codex", RequestedModel: "provider-alias"})
	if err != nil {
		t.Fatal(err)
	}
	state := protocol.CodexSessionState{
		ThreadID: "native-thread", SocketPath: "/tmp/codex.sock", ServerVersion: "0.163.0", ConnectionID: "old",
		ConnectionState: protocol.CodexConnected, ConversationState: protocol.CodexConversationReady,
		TurnState: protocol.CodexTurnCompleted, TurnID: "turn-1",
	}
	if err := store.SetCodexState(ctx, session.ID, state); err != nil {
		t.Fatal(err)
	}
	request := protocol.CodexRequestPayload{
		ConnectionID: "old", RequestID: json.RawMessage(`9007199254740993`), Method: "item/tool/requestUserInput", Kind: protocol.CodexUserInput,
		ThreadID: state.ThreadID, TurnID: state.TurnID, ItemID: "item-1", Params: json.RawMessage(`{"questions":[{"id":"q1","options":[{"label":"Keep"}]}]}`),
	}
	if err := store.CreatePendingCodexRequest(ctx, session.ID, request); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, session.ID, protocol.EventCodexRequest, request); err != nil {
		t.Fatal(err)
	}
	unknown := protocol.CodexEventPayload{
		Method: "future/itemChanged", ThreadID: state.ThreadID, TurnID: state.TurnID, ItemID: "item-1",
		Params: json.RawMessage(`{"future":{"version":3,"value":"retained"}}`),
	}
	if _, err := store.AppendEvent(ctx, session.ID, protocol.EventCodexEvent, unknown); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetSession(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Codex == nil || *got.Codex != state || got.Status != "running" {
		t.Fatalf("completed turn changed conversation lifetime or lost native state: %+v", got)
	}
	summaries, err := store.ListSessionSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].Codex == nil || *summaries[0].Codex != state {
		t.Fatalf("summary native state = %+v", summaries)
	}
	events, err := store.Replay(ctx, session.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("cursor replay = %+v", events)
	}
	var preserved protocol.CodexEventPayload
	if err := json.Unmarshal(events[0].Payload, &preserved); err != nil {
		t.Fatal(err)
	}
	if preserved.Method != unknown.Method || string(preserved.Params) != string(unknown.Params) || preserved.ItemID != "item-1" {
		t.Fatalf("unknown native event changed: %+v", preserved)
	}
	pending, err := store.ListPendingCodexRequests(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].RequestKey != "9007199254740993" || string(pending[0].RequestID) != "9007199254740993" || string(pending[0].Params) != string(request.Params) {
		t.Fatalf("native request changed: %+v", pending)
	}
	// Native IDs can repeat after reconnect. Expire only the old generation.
	request.ConnectionID = "new"
	if err := store.CreatePendingCodexRequest(ctx, session.ID, request); err != nil {
		t.Fatal(err)
	}
	if err := store.InvalidateCodexRequests(ctx, session.ID, "old"); err != nil {
		t.Fatal(err)
	}
	result := json.RawMessage(`{"answers":{"q1":{"answers":["Keep"]}}}`)
	if err := store.ResolveCodexRequest(ctx, session.ID, "old", request.RequestID, result); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale response = %v", err)
	}
	if err := store.ResolveCodexRequest(ctx, session.ID, "new", request.RequestID, result); err != nil {
		t.Fatal(err)
	}
	if err := store.ResolveCodexRequest(ctx, session.ID, "new", request.RequestID, result); !errors.Is(err, ErrNotFound) {
		t.Fatalf("duplicate response = %v", err)
	}
	var storedResult string
	if err := store.db.QueryRowContext(ctx, `SELECT result_json FROM codex_requests WHERE session_id = ? AND connection_id = 'new'`, session.ID).Scan(&storedResult); err != nil {
		t.Fatal(err)
	}
	if storedResult != string(result) {
		t.Fatalf("native result changed: %s", storedResult)
	}
	pending, err = store.ListPendingCodexRequests(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending == nil || len(pending) != 0 {
		t.Fatalf("resolved requests still pending: %+v", pending)
	}
}

func TestOversizedCodexRequestCannotBecomeActionable(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	session, err := store.CreateSession(ctx, CreateSessionParams{Repo: ".", Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(map[string]string{"command": strings.Repeat("x", protocol.MaxCodexPayloadBytes)})
	if err != nil {
		t.Fatal(err)
	}
	request := protocol.CodexRequestPayload{
		ConnectionID: "connection", RequestID: json.RawMessage(`"1"`), Method: "item/commandExecution/requestApproval",
		Kind: protocol.CodexToolApproval, Params: params,
	}
	if err := store.CreatePendingCodexRequest(ctx, session.ID, request); !errors.Is(err, protocol.ErrCodexPayloadTooLarge) {
		t.Fatalf("oversized pending request = %v", err)
	}
	event, err := store.AppendEvent(ctx, session.ID, protocol.EventCodexRequest, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(event.Payload) > protocol.MaxCodexPayloadBytes {
		t.Fatalf("payload still exceeds limit: %d", len(event.Payload))
	}
	var bounded protocol.CodexRequestPayload
	if err := json.Unmarshal(event.Payload, &bounded); err != nil {
		t.Fatal(err)
	}
	if !bounded.Unsupported || !bounded.Truncated || bounded.OriginalBytes <= protocol.MaxCodexPayloadBytes || len(bounded.Params) != 0 {
		t.Fatalf("oversized request is not disabled: %+v", bounded)
	}
	if string(bounded.RequestID) != `"1"` || bounded.Method != request.Method {
		t.Fatalf("request identity changed: %+v", bounded)
	}
	if err := store.CreatePendingCodexRequest(ctx, session.ID, bounded); !errors.Is(err, ErrUnsupportedCodexRequest) {
		t.Fatalf("truncated request became actionable: %v", err)
	}
	unsupported := request
	unsupported.Kind = protocol.CodexUnknownRequest
	unsupported.Params = json.RawMessage(`{}`)
	if err := store.CreatePendingCodexRequest(ctx, session.ID, unsupported); !errors.Is(err, ErrUnsupportedCodexRequest) {
		t.Fatalf("unknown request became actionable: %v", err)
	}
	unknown, err := store.AppendEvent(ctx, session.ID, protocol.EventCodexEvent, protocol.CodexEventPayload{Method: "future/large", Params: params})
	if err != nil {
		t.Fatal(err)
	}
	var notification protocol.CodexEventPayload
	if err := json.Unmarshal(unknown.Payload, &notification); err != nil {
		t.Fatal(err)
	}
	if notification.Method != "future/large" || !notification.Truncated || len(notification.Params) != 0 {
		t.Fatalf("unknown event bounds = %+v", notification)
	}
}
