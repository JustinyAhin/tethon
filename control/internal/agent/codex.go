package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"

	"tethon/internal/codex"
	"tethon/internal/protocol"
)

type CodexConfig = codex.Config

type Codex struct {
	config CodexConfig
}

// CodexSink records delivery state. It does not own the native conversation.
type CodexSink interface {
	Sink
	SetCodexState(context.Context, protocol.CodexSessionState) error
	CreatePendingCodexRequest(context.Context, protocol.CodexRequestPayload) error
	MarkCodexRequestResolved(context.Context, string, json.RawMessage, json.RawMessage) error
	InvalidateCodexRequests(context.Context, string) error
	LastEventSeq(context.Context) (int64, error)
	BeginCodexAction(context.Context, string, string) (string, json.RawMessage, error)
	CompleteCodexAction(context.Context, string, string, string, json.RawMessage) error
	ConfirmCodexAction(context.Context, string) error
}

type NativeThread struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Preview   string `json:"preview"`
	CWD       string `json:"cwd"`
	Model     string `json:"model"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
	Status    struct {
		Type string `json:"type"`
	} `json:"status"`
	Turns []protocol.CodexHistoryTurn `json:"turns"`
}

type codexSession struct {
	adapter   *Codex
	sink      CodexSink
	client    *codex.Client
	mu        sync.Mutex
	actions   sync.Mutex
	state     protocol.CodexSessionState
	pending   map[string]protocol.CodexRequestPayload
	responded map[string]bool
}

func NewCodex(config CodexConfig) *Codex {
	return &Codex{config: config}
}

func (a *Codex) Name() string {
	return "codex-cli"
}

func (a *Codex) Capabilities() Capabilities {
	return Capabilities{
		Stream:           true,
		InteractiveInput: true,
		Approvals:        true,
		Resume:           true,
	}
}

func (a *Codex) connect(ctx context.Context) (*codex.Client, codex.DaemonInfo, error) {
	return codex.Connect(ctx, a.config)
}

func (a *Codex) Check(ctx context.Context) error {
	client, _, err := a.connect(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	var account struct {
		Account            json.RawMessage `json:"account"`
		RequiresOpenaiAuth bool            `json:"requiresOpenaiAuth"`
	}

	if err := client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		return err
	}

	if account.RequiresOpenaiAuth && (len(account.Account) == 0 || string(account.Account) == "null") {
		return errors.New("Codex is not signed in; run codex login on the host")
	}

	return nil
}

func (a *Codex) Start(ctx context.Context, req StartRequest, sink Sink) (RunningSession, error) {
	nativeSink, ok := sink.(CodexSink)
	if !ok {
		return nil, errors.New("Codex requires native delivery storage")
	}
	if req.Session.Codex != nil && req.Session.Codex.ConversationState == protocol.CodexConversationUnavailable {
		return nil, errors.New("the native Codex conversation is unavailable")
	}

	client, info, err := a.connect(ctx)
	if err != nil {
		return nil, err
	}

	keep := false
	defer func() {
		if !keep {
			client.Close()
		}
	}()

	params := map[string]any{}
	method := "thread/start"
	conversationState := protocol.CodexConversationReady
	if req.Create.NativeThreadID != "" {
		method = "thread/resume"
		params["threadId"] = req.Create.NativeThreadID
		params["excludeTurns"] = true
		if req.Session.Codex != nil && req.Session.Codex.ConversationState == protocol.CodexConversationArchived {
			method = "thread/read"
			conversationState = protocol.CodexConversationArchived
			delete(params, "excludeTurns")
		}
	} else {
		params["cwd"] = req.WorkDir
	}

	if req.Create.RequestedModel != "" {
		params["model"] = req.Create.RequestedModel
	}
	if req.Create.CodexPermissionProfile != "" && method == "thread/start" {
		if !a.config.Experimental {
			return nil, errors.New("native permission profiles are unavailable on this connection")
		}

		params["permissions"] = req.Create.CodexPermissionProfile
	}

	var started struct {
		Thread NativeThread `json:"thread"`
		Model  string       `json:"model"`
		CWD    string       `json:"cwd"`
	}

	if err := client.Call(ctx, method, params, &started); err != nil {
		return nil, err
	}

	if started.Thread.ID == "" {
		return nil, errors.New("Codex returned no native thread identity")
	}
	if started.CWD == "" {
		started.CWD = started.Thread.CWD
	}

	running := &codexSession{
		adapter:   a,
		sink:      nativeSink,
		client:    client,
		pending:   make(map[string]protocol.CodexRequestPayload),
		responded: make(map[string]bool),
		state: protocol.CodexSessionState{
			ThreadID:          started.Thread.ID,
			CWD:               started.CWD,
			SocketPath:        info.SocketPath,
			ServerVersion:     info.AppServerVersion,
			ConnectionID:      uuid.NewString(),
			ConnectionState:   protocol.CodexConnecting,
			ConversationState: conversationState,
			TurnState:         protocol.CodexTurnIdle,
		},
	}

	if req.Session.Codex != nil {
		if err := nativeSink.InvalidateCodexRequests(ctx, req.Session.Codex.ConnectionID); err != nil {
			return nil, err
		}
	}

	if err := nativeSink.SetCodexState(ctx, running.state); err != nil {
		return nil, err
	}
	if _, err := sink.AppendEvent(ctx, protocol.EventSessionStarted, req.Create); err != nil {
		return nil, err
	}
	if method == "thread/start" || conversationState == protocol.CodexConversationArchived {
		model := started.Model
		if model == "" {
			model = started.Thread.Model
		}
		if err := sink.SetSessionModel(ctx, model); err != nil {
			return nil, err
		}

		running.state.ConnectionState = protocol.CodexConnected
		if err := running.saveStateLocked(); err != nil {
			return nil, err
		}
	} else {
		if err := running.reconcileConnection(ctx, client, started.Thread.ID, started.Model); err != nil {
			return nil, err
		}
	}

	keep = true
	go running.consume()

	if req.Create.Prompt != "" {
		// The connection and native thread now exist. A lost acknowledgement must
		// not turn a potentially executed prompt into a retryable create error.
		requestID := req.Create.RequestID
		if requestID == "" {
			requestID = uuid.NewString()
		}

		input := protocol.InputRequest{
			Text:                   req.Create.Prompt,
			Mode:                   "start",
			RequestID:              requestID,
			RequestedModel:         req.Create.RequestedModel,
			ReasoningEffort:        req.Create.ReasoningEffort,
			CodexPermissionProfile: req.Create.CodexPermissionProfile,
		}
		_, err := running.InputNative(ctx, input, req.Create.ReasoningEffort)
		if err != nil {
			_, _ = sink.AppendEvent(context.Background(), protocol.EventCodexEvent, protocol.CodexEventPayload{
				Method:   "tethon/inputError",
				ThreadID: started.Thread.ID,
				Params:   mustJSON(map[string]string{"message": err.Error()}),
			})
		}
	}

	return running, nil
}

func (s *codexSession) NativeState() protocol.CodexSessionState {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state
}

func (s *codexSession) NativeSnapshot(ctx context.Context) (protocol.CodexSessionResponse, error) {
	s.mu.Lock()
	seq, err := s.sink.LastEventSeq(ctx)
	if err != nil {
		s.mu.Unlock()
		return protocol.CodexSessionResponse{}, err
	}

	requests := make([]protocol.CodexRequestPayload, 0, len(s.pending))
	for _, request := range s.pending {
		requests = append(requests, request)
	}

	sort.Slice(requests, func(i, j int) bool {
		return requests[i].RequestKey < requests[j].RequestKey
	})
	response := protocol.CodexSessionResponse{
		Seq:             seq,
		State:           s.state,
		PendingRequests: requests,
		Queue:           []json.RawMessage{},
	}
	client := s.client
	s.mu.Unlock()

	queueAvailable := s.adapter.config.Experimental &&
		response.State.ConnectionState == protocol.CodexConnected &&
		response.State.ConversationState == protocol.CodexConversationReady
	if queueAvailable {
		var queue struct {
			Data       []json.RawMessage `json:"data"`
			NextCursor string            `json:"nextCursor"`
		}

		if err := client.Call(ctx, "thread/queue/list", map[string]any{"threadId": response.State.ThreadID, "limit": 100}, &queue); err != nil {
			return response, err
		}

		response.QueueAvailable = true
		response.QueueNextCursor = queue.NextCursor
		if queue.Data != nil {
			response.Queue = queue.Data
		}

		if err := s.confirmQueueActions(ctx, response.Queue); err != nil {
			return response, err
		}
	}

	return response, nil
}

func (s *codexSession) saveStateLocked() error {
	ctx := context.Background()
	if err := s.sink.SetCodexState(ctx, s.state); err != nil {
		return err
	}

	_, err := s.sink.AppendEvent(ctx, protocol.EventCodexState, s.state)
	return err
}

func (s *codexSession) consume() {
	for {
		s.mu.Lock()
		client := s.client
		s.mu.Unlock()

		for message := range client.Messages() {
			if err := s.handleMessage(message); err != nil {
				client.Close()
				break
			}
		}

		if !s.reconnect() {
			return
		}
	}
}

func (s *codexSession) handleMessage(message codex.Message) error {
	var params struct {
		ThreadID       string          `json:"threadId"`
		TurnID         string          `json:"turnId"`
		ItemID         string          `json:"itemId"`
		RequestID      json.RawMessage `json:"requestId"`
		ToModel        string          `json:"toModel"`
		ThreadSettings struct {
			Model string `json:"model"`
			CWD   string `json:"cwd"`
		} `json:"threadSettings"`
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
		Item struct {
			ID string `json:"id"`
		} `json:"item"`
	}

	if len(message.Params) != 0 {
		if err := json.Unmarshal(message.Params, &params); err != nil {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if params.ThreadID != "" && params.ThreadID != s.state.ThreadID {
		return nil
	}

	if params.TurnID == "" {
		params.TurnID = params.Turn.ID
	}
	if params.ItemID == "" {
		params.ItemID = params.Item.ID
	}

	ctx := context.Background()
	if len(message.ID) != 0 {
		request := protocol.CodexRequestPayload{
			RequestID:    message.ID,
			ConnectionID: s.state.ConnectionID,
			Method:       message.Method,
			Kind:         nativeRequestKind(message.Method),
			ThreadID:     s.state.ThreadID,
			TurnID:       params.TurnID,
			ItemID:       params.ItemID,
			Params:       message.Params,
		}
		request, err := protocol.BoundCodexRequest(request)
		if err != nil {
			return err
		}

		if !request.Unsupported {
			if err := s.sink.CreatePendingCodexRequest(ctx, request); err != nil {
				return err
			}
			s.pending[request.RequestKey] = request
		}

		_, err = s.sink.AppendEvent(ctx, protocol.EventCodexRequest, request)
		return err
	}

	if message.Method == "serverRequest/resolved" {
		key, err := protocol.CodexRequestID(params.RequestID)
		if err != nil {
			return err
		}

		delete(s.pending, key)
		delete(s.responded, key)

		// The request can have been answered in the terminal or another client.
		_ = s.sink.MarkCodexRequestResolved(ctx, s.state.ConnectionID, params.RequestID, nil)
		_, err = s.sink.AppendEvent(ctx, protocol.EventCodexRequestResolved, protocol.CodexRequestResolvedPayload{
			RequestID:    params.RequestID,
			RequestKey:   key,
			ConnectionID: s.state.ConnectionID,
		})
		return err
	}

	changed := false
	switch message.Method {
	case "thread/settings/updated":
		if params.ThreadSettings.CWD != "" && s.state.CWD != params.ThreadSettings.CWD {
			s.state.CWD = params.ThreadSettings.CWD
			changed = true
		}

		if params.ThreadSettings.Model != "" {
			if err := s.sink.SetSessionModel(ctx, params.ThreadSettings.Model); err != nil {
				return err
			}
		}
	case "model/rerouted":
		if params.ToModel != "" {
			if err := s.sink.SetSessionModel(ctx, params.ToModel); err != nil {
				return err
			}
		}
	case "turn/started":
		s.state.TurnID, s.state.TurnState = params.Turn.ID, protocol.CodexTurnInProgress
		changed = true
	case "turn/completed":
		if s.state.TurnID == "" || s.state.TurnID == params.Turn.ID {
			s.state.TurnID = params.Turn.ID
			s.state.TurnState = protocol.CodexTurnCompleted
			if params.Turn.Status == "interrupted" {
				s.state.TurnState = protocol.CodexTurnInterrupted
			} else if params.Turn.Status == "failed" {
				s.state.TurnState = protocol.CodexTurnFailed
			}
			changed = true
		}
	case "thread/archived":
		s.state.ConversationState = protocol.CodexConversationArchived
		changed = true
	case "thread/unarchived":
		s.state.ConversationState = protocol.CodexConversationReady
		changed = true
	case "thread/deleted":
		s.state.ConversationState = protocol.CodexConversationUnavailable
		changed = true
	}

	payload := protocol.CodexEventPayload{
		Method:       message.Method,
		ThreadID:     s.state.ThreadID,
		TurnID:       params.TurnID,
		ItemID:       params.ItemID,
		ConnectionID: s.state.ConnectionID,
		Params:       message.Params,
	}
	if _, err := s.sink.AppendEvent(ctx, protocol.EventCodexEvent, payload); err != nil {
		return err
	}
	if changed {
		return s.saveStateLocked()
	}

	return nil
}

func nativeRequestKind(method string) protocol.CodexRequestKind {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval", "item/permissions/requestApproval":
		return protocol.CodexToolApproval
	case "item/tool/requestUserInput":
		return protocol.CodexUserInput
	case "mcpServer/elicitation/request":
		return protocol.CodexMCPRequest
	default:
		return protocol.CodexUnknownRequest
	}
}

func (s *codexSession) Input(ctx context.Context, text string) ([]protocol.Event, error) {
	return s.InputNative(ctx, protocol.InputRequest{Text: text, RequestID: uuid.NewString()}, "")
}

func (s *codexSession) sendInput(ctx context.Context, request protocol.InputRequest, effort string) ([]protocol.Event, error) {
	state := s.NativeState()
	if state.ConnectionState != protocol.CodexConnected || state.ConversationState != protocol.CodexConversationReady {
		return nil, errors.New("reconnect the native Codex conversation before sending input")
	}

	mode := request.Mode
	if mode == "" {
		mode = "start"
		if state.TurnState == protocol.CodexTurnInProgress {
			mode = "steer"
		}
	}

	if request.ReasoningEffort != "" {
		effort = request.ReasoningEffort
	}
	overrides := request.RequestedModel != "" || effort != "" || request.CodexPermissionProfile != ""
	if overrides && mode != "start" {
		return nil, errors.New("native model, reasoning, and permission changes require a new turn")
	}
	if request.CodexPermissionProfile != "" && !s.adapter.config.Experimental {
		return nil, errors.New("native permission profiles are unavailable on this connection")
	}

	params := map[string]any{
		"threadId":            state.ThreadID,
		"clientUserMessageId": request.RequestID,
		"input":               []any{map[string]any{"type": "text", "text": request.Text}},
	}
	method := "turn/start"
	switch mode {
	case "start":
		if state.TurnState == protocol.CodexTurnInProgress {
			return nil, ErrBusy
		}
		if effort != "" {
			params["effort"] = effort
		}
		if request.RequestedModel != "" {
			params["model"] = request.RequestedModel
		}
		if request.CodexPermissionProfile != "" {
			params["permissions"] = request.CodexPermissionProfile
		}
	case "steer":
		if state.TurnState != protocol.CodexTurnInProgress || (request.TurnID != "" && request.TurnID != state.TurnID) {
			return nil, errors.New("the target Codex turn is stale; refresh the conversation")
		}
		params["expectedTurnId"] = state.TurnID
		method = "turn/steer"
	case "queue":
		if !s.adapter.config.Experimental {
			return nil, errors.New("native queued input is unavailable on this connection")
		}
		method = "thread/queue/add"
	default:
		return nil, errors.New("unsupported native input mode")
	}

	var reply struct {
		Turn struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"turn"`
	}

	if err := s.client.Call(ctx, method, params, &reply); err != nil {
		var uncertain *codex.DeliveryError
		if errors.As(err, &uncertain) {
			return nil, err
		}

		var rejected *codex.RPCError
		if errors.As(err, &rejected) {
			return nil, err
		}

		return nil, &codex.DeliveryError{Err: err}
	}
	if mode == "start" && reply.Turn.ID == "" {
		s.client.Close()
		return nil, &codex.DeliveryError{Err: errors.New("native input acknowledgement has no turn identity")}
	}

	if mode == "start" && reply.Turn.ID != "" {
		s.mu.Lock()
		if s.state.TurnID != reply.Turn.ID {
			s.state.TurnID, s.state.TurnState = reply.Turn.ID, protocol.CodexTurnInProgress
			if err := s.saveStateLocked(); err != nil {
				s.mu.Unlock()
				return nil, &codex.DeliveryError{Err: err}
			}
		}
		s.mu.Unlock()
	}

	s.mu.Lock()
	event, err := s.sink.AppendEvent(context.Background(), protocol.EventInputReceived, map[string]any{
		"text":       request.Text,
		"mode":       mode,
		"thread_id":  state.ThreadID,
		"request_id": request.RequestID,
	})
	s.mu.Unlock()
	if err != nil {
		return nil, &codex.DeliveryError{Err: err}
	}

	return []protocol.Event{event}, err
}

func (s *codexSession) Cancel(ctx context.Context) ([]protocol.Event, error) {
	return s.StopNative(ctx, "")
}

func (s *codexSession) StopNative(ctx context.Context, turnID string) ([]protocol.Event, error) {
	s.actions.Lock()
	defer s.actions.Unlock()

	state := s.NativeState()
	if state.ConnectionState != protocol.CodexConnected {
		return nil, errors.New("Codex is disconnected; reconnect before stopping a turn")
	}
	if turnID != "" && turnID != state.TurnID {
		return nil, errors.New("the target Codex turn is stale; refresh the conversation")
	}
	if state.TurnState != protocol.CodexTurnInProgress {
		return []protocol.Event{}, nil
	}

	err := s.client.Call(ctx, "turn/interrupt", map[string]string{"threadId": state.ThreadID, "turnId": state.TurnID}, nil)
	return []protocol.Event{}, err
}

func (s *codexSession) RespondNative(ctx context.Context, request protocol.CodexRespondRequest) error {
	s.actions.Lock()
	defer s.actions.Unlock()

	s.mu.Lock()
	pending, ok := s.pending[request.RequestKey]
	valid := ok &&
		request.ConnectionID == s.state.ConnectionID &&
		s.state.ConnectionState == protocol.CodexConnected &&
		!s.responded[request.RequestKey]
	if valid {
		s.responded[request.RequestKey] = true
	}
	s.mu.Unlock()

	if !valid {
		return errors.New("the native request is resolved, stale, or disconnected")
	}

	if len(request.Result) == 0 || !json.Valid(request.Result) || len(request.Result) > protocol.MaxCodexPayloadBytes {
		s.mu.Lock()
		delete(s.responded, request.RequestKey)
		s.mu.Unlock()
		return errors.New("the native answer must contain bounded valid JSON")
	}

	// A successful write is not a resolution. Only serverRequest/resolved may
	// clear the request in delivery storage and all clients.
	if err := s.client.Respond(ctx, pending.RequestID, request.Result); err != nil {
		return &codex.DeliveryError{Err: err}
	}

	return nil
}

func (s *codexSession) ResolveApproval(context.Context, string, string, string) ([]protocol.Event, error) {
	return nil, errors.New("Codex requests require their native response choices")
}

func (s *codexSession) Terminal() bool {
	return false
}

func (s *codexSession) ProcessIdentity() ProcessIdentity {
	return ProcessIdentity{}
}

func (s *codexSession) history(ctx context.Context, cursor string, limit int, out *protocol.CodexHistoryResponse) error {
	s.mu.Lock()
	client, threadID := s.client, s.state.ThreadID
	s.mu.Unlock()

	return s.historyWithClient(ctx, client, threadID, cursor, limit, out)
}

func (s *codexSession) historyWithClient(ctx context.Context, client *codex.Client, threadID, cursor string, limit int, out *protocol.CodexHistoryResponse) error {
	var page struct {
		Data       []protocol.CodexHistoryTurn `json:"data"`
		NextCursor string                      `json:"nextCursor"`
	}

	params := map[string]any{
		"threadId":      threadID,
		"limit":         limit,
		"itemsView":     "full",
		"sortDirection": "desc",
	}
	if cursor != "" {
		params["cursor"] = cursor
	}

	if err := client.Call(ctx, "thread/turns/list", params, &page); err != nil {
		return err
	}

	if page.Data == nil {
		page.Data = []protocol.CodexHistoryTurn{}
	}

	*out = protocol.CodexHistoryResponse{
		ThreadID:   threadID,
		Turns:      page.Data,
		NextCursor: page.NextCursor,
	}
	return s.confirmHistoryActions(ctx, *out)
}

func (s *codexSession) NativeHistory(ctx context.Context, cursor string, limit int) (protocol.CodexHistoryResponse, error) {
	var history protocol.CodexHistoryResponse
	err := s.history(ctx, cursor, limit, &history)
	return history, err
}

func (a *Codex) ReadThread(ctx context.Context, threadID string) (NativeThread, error) {
	client, _, err := a.connect(ctx)
	if err != nil {
		return NativeThread{}, err
	}
	defer client.Close()

	var response struct {
		Thread NativeThread `json:"thread"`
	}

	err = client.Call(ctx, "thread/read", map[string]string{"threadId": threadID}, &response)
	return response.Thread, err
}

func (a *Codex) Conversations(ctx context.Context, cursor, search string) (protocol.CodexConversationsResponse, error) {
	client, _, err := a.connect(ctx)
	if err != nil {
		return protocol.CodexConversationsResponse{}, err
	}
	defer client.Close()

	params := map[string]any{"limit": 30}
	if cursor != "" {
		params["cursor"] = cursor
	}
	if search != "" {
		params["searchTerm"] = search
	}

	var page struct {
		Data       []NativeThread `json:"data"`
		NextCursor string         `json:"nextCursor"`
	}

	if err := client.Call(ctx, "thread/list", params, &page); err != nil {
		return protocol.CodexConversationsResponse{}, err
	}

	result := protocol.CodexConversationsResponse{
		Conversations: []protocol.CodexConversation{},
		NextCursor:    page.NextCursor,
	}
	for _, thread := range page.Data {
		result.Conversations = append(result.Conversations, protocol.CodexConversation{
			ThreadID:  thread.ID,
			Name:      thread.Name,
			Preview:   thread.Preview,
			CWD:       thread.CWD,
			Model:     thread.Model,
			Status:    thread.Status.Type,
			CreatedAt: thread.CreatedAt,
			UpdatedAt: thread.UpdatedAt,
		})
	}

	return result, nil
}

func (a *Codex) ListModels(ctx context.Context) (protocol.ModelCatalog, error) {
	client, _, err := a.connect(ctx)
	if err != nil {
		return protocol.ModelCatalog{}, err
	}
	defer client.Close()

	var page struct {
		Data []struct {
			ID                        string   `json:"id"`
			Model                     string   `json:"model"`
			DisplayName               string   `json:"displayName"`
			Description               string   `json:"description"`
			IsDefault                 bool     `json:"isDefault"`
			Hidden                    bool     `json:"hidden"`
			InputModalities           []string `json:"inputModalities"`
			DefaultReasoningEffort    string   `json:"defaultReasoningEffort"`
			SupportedReasoningEfforts []struct {
				ReasoningEffort string `json:"reasoningEffort"`
				Description     string `json:"description"`
			} `json:"supportedReasoningEfforts"`
		} `json:"data"`
		NextCursor string `json:"nextCursor"`
	}

	catalog := protocol.ModelCatalog{
		Agent:  a.Name(),
		Status: "ready",
		Source: "codex:model/list",
		Models: []protocol.ModelInfo{},
	}
	params := map[string]any{}

	for {
		if err := client.Call(ctx, "model/list", params, &page); err != nil {
			return protocol.ModelCatalog{}, err
		}

		for _, model := range page.Data {
			if model.Hidden {
				continue
			}

			id := model.Model
			if id == "" {
				id = model.ID
			}

			descriptor := protocol.ModelInfo{
				ID:                     id,
				Label:                  model.DisplayName,
				Provider:               "openai",
				Description:            model.Description,
				IsDefault:              model.IsDefault,
				InputModalities:        model.InputModalities,
				DefaultReasoningEffort: model.DefaultReasoningEffort,
			}
			for _, effort := range model.SupportedReasoningEfforts {
				descriptor.ReasoningEfforts = append(descriptor.ReasoningEfforts, protocol.ModelReasoningEffort{
					ID:          effort.ReasoningEffort,
					Description: effort.Description,
				})
			}
			catalog.Models = append(catalog.Models, descriptor)
		}

		if page.NextCursor == "" {
			break
		}

		params["cursor"] = page.NextCursor
	}

	return catalog, nil
}

func mustJSON(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("encode native event: %v", err))
	}

	return data
}

var _ Adapter = (*Codex)(nil)
var _ ModelLister = (*Codex)(nil)
