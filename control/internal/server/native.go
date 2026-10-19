package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"tethon/internal/agent"
	"tethon/internal/codex"
	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

type nativeRunningSession interface {
	agent.RunningSession
	NativeSnapshot(context.Context) (protocol.CodexSessionResponse, error)
	NativeHistory(context.Context, string, int) (protocol.CodexHistoryResponse, error)
	InputNative(context.Context, protocol.InputRequest, string) ([]protocol.Event, error)
	StopNative(context.Context, string) ([]protocol.Event, error)
	RespondNative(context.Context, protocol.CodexRespondRequest) error
}

func (s *Server) recoverCodexSession(ctx context.Context, session protocol.Session) error {
	state := *session.Codex

	if err := s.store.InvalidateCodexRequests(ctx, session.ID, state.ConnectionID); err != nil {
		return err
	}

	state.ConnectionState = protocol.CodexReconnecting
	if err := s.store.SetCodexState(ctx, session.ID, state); err != nil {
		return err
	}

	adapter, ok := s.agents.Get("codex-cli")
	if !ok {
		state.ConnectionState = protocol.CodexDisconnected
		_ = s.store.SetCodexState(ctx, session.ID, state)
		_, _ = s.appendEvent(ctx, session.ID, protocol.EventCodexState, state)
		return errors.New("Codex is unavailable on this host")
	}

	workDir := joinCWD(session.Repo, session.CWD)
	if session.WorktreePath != "" {
		workDir = joinCWD(session.WorktreePath, session.CWD)
	}

	create := protocol.CreateSessionRequest{
		Agent:          "codex-cli",
		NativeThreadID: state.ThreadID,
		Repo:           session.Repo,
		CWD:            session.CWD,
		WorkspaceMode:  protocol.WorkspaceExisting,
	}
	start := agent.StartRequest{
		Session: session,
		Create:  create,
		WorkDir: workDir,
	}

	running, err := adapter.Start(ctx, start, s.sink(session.ID))
	if err != nil {
		state.ConnectionState = protocol.CodexDisconnected
		_ = s.store.SetCodexState(ctx, session.ID, state)
		_, _ = s.appendEvent(ctx, session.ID, protocol.EventCodexState, state)
		return err
	}

	s.setRunning(session.ID, running)

	return nil
}

// prepareCodexResume resolves the original native working folder. An imported
// thread never refreshes, resets, or switches the user's checkout.
func (s *Server) prepareCodexResume(ctx context.Context, req *protocol.CreateSessionRequest) (string, error) {
	adapter, ok := s.agents.Get("codex-cli")
	if !ok {
		return "", errors.New("Codex is unavailable on this host")
	}

	native, ok := adapter.(*agent.Codex)
	if !ok {
		return "", errors.New("the host does not support native Codex conversations")
	}

	if req.WorkspaceMode == protocol.WorkspaceIsolated {
		return "", errors.New("an existing native conversation keeps its working folder")
	}

	sessions, err := s.store.ListSessionSummaries(ctx)
	if err != nil {
		return "", err
	}

	for _, session := range sessions {
		if session.Codex != nil && session.Codex.ThreadID == req.NativeThreadID {
			return session.ID, nil
		}
	}

	thread, err := native.ReadThread(ctx, req.NativeThreadID)
	if err != nil {
		return "", err
	}

	cwd, err := resolveRealDir(thread.CWD)
	if err != nil {
		return "", errors.New("the native working folder is unavailable on the host")
	}

	req.RepoID, req.CWD, req.Repo = "", "", cwd
	if s.repos != nil {
		rp, ok := s.repos.Find(cwd)
		if !ok {
			return "", errors.New("the native working folder is outside the registered repositories")
		}

		rel, err := filepath.Rel(rp.Path, cwd)
		if err != nil {
			return "", err
		}

		req.RepoID, req.Repo = rp.ID, rp.Path
		if rel != "." {
			req.CWD = rel
		}
	}

	req.WorkspaceMode = protocol.WorkspaceExisting

	return "", nil
}

func (s *Server) handleCodexConversations(w http.ResponseWriter, r *http.Request) {
	adapter, ok := s.agents.Get("codex-cli")
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Codex is unavailable on this host")
		return
	}

	native, ok := adapter.(*agent.Codex)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "native Codex conversations are unavailable")
		return
	}

	page, err := native.Conversations(r.Context(), r.URL.Query().Get("cursor"), r.URL.Query().Get("search"))
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	roots := append([]string{}, s.repoRoots...)
	if s.repos != nil {
		roots = append(roots, s.repos.Paths()...)
	}
	if len(roots) > 0 {
		visible := make([]protocol.CodexConversation, 0, len(page.Conversations))
		for _, conversation := range page.Conversations {
			cwd, err := resolveRealDir(conversation.CWD)
			if err == nil && pathWithinAny(cwd, roots) {
				visible = append(visible, conversation)
			}
		}

		page.Conversations = visible
	}

	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleNativeSnapshot(w http.ResponseWriter, r *http.Request, sessionID string) {
	session, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if running, ok := s.getRunning(sessionID).(nativeRunningSession); ok {
		response, err := running.NativeSnapshot(r.Context())
		if err != nil {
			writeNativeError(w, err)
			return
		}

		response.WorkspaceOwned = s.ownsNativeWorkspace(session)

		writeJSON(w, http.StatusOK, response)
		return
	}

	if session.Codex == nil {
		writeError(w, http.StatusBadRequest, "session is not a native Codex conversation")
		return
	}

	state := *session.Codex
	state.ConnectionState = protocol.CodexDisconnected

	seq, err := s.store.LastEventSeq(r.Context(), sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, protocol.CodexSessionResponse{
		Seq:             seq,
		State:           state,
		PendingRequests: []protocol.CodexRequestPayload{},
		Queue:           []json.RawMessage{},
		WorkspaceOwned:  s.ownsNativeWorkspace(session),
	})
}

func (s *Server) ownsNativeWorkspace(session protocol.Session) bool {
	if s.worktrees == nil || session.WorktreePath == "" {
		return false
	}

	owned, ok := s.worktrees.Get(session.ID)

	return ok && owned.Path == session.WorktreePath
}

func (s *Server) handleNativeHistory(w http.ResponseWriter, r *http.Request, sessionID string) {
	running, ok := s.getRunning(sessionID).(nativeRunningSession)
	if !ok {
		writeError(w, http.StatusConflict, "reconnect the native Codex conversation before reading history")
		return
	}

	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "history limit must be between 1 and 100")
			return
		}

		limit = parsed
	}

	history, err := running.NativeHistory(r.Context(), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeNativeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, history)
}

func (s *Server) handleNativeResponse(w http.ResponseWriter, r *http.Request, sessionID string) {
	var request protocol.CodexRespondRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, protocol.MaxCodexPayloadBytes)).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid native response")
		return
	}

	if request.RequestKey == "" || request.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "request_key and connection_id are required")
		return
	}

	running, ok := s.getRunning(sessionID).(nativeRunningSession)
	if !ok {
		writeError(w, http.StatusConflict, "the native request is disconnected or stale")
		return
	}

	if err := running.RespondNative(r.Context(), request); err != nil {
		writeNativeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, protocol.ActionEventsResponse{Events: []protocol.Event{}})
}

func writeNativeError(w http.ResponseWriter, err error) {
	if errors.Is(err, sessionlog.ErrCreateRequestUncertain) {
		writeError(w, http.StatusGatewayTimeout, "resume delivery is unconfirmed; check this conversation before starting another request")
		return
	}

	var uncertain *codex.DeliveryError
	if errors.As(err, &uncertain) {
		writeError(w, http.StatusGatewayTimeout, uncertain.Error())
		return
	}

	writeError(w, http.StatusConflict, err.Error())
}

func (s sessionSink) SetCodexState(ctx context.Context, state protocol.CodexSessionState) error {
	return s.s.store.SetCodexState(ctx, s.sessionID, state)
}

func (s sessionSink) LastEventSeq(ctx context.Context) (int64, error) {
	return s.s.store.LastEventSeq(ctx, s.sessionID)
}

func (s sessionSink) CreatePendingCodexRequest(ctx context.Context, request protocol.CodexRequestPayload) error {
	return s.s.store.CreatePendingCodexRequest(ctx, s.sessionID, request)
}

func (s sessionSink) MarkCodexRequestResolved(ctx context.Context, connectionID string, requestID, result json.RawMessage) error {
	return s.s.store.MarkCodexRequestResolved(ctx, s.sessionID, connectionID, requestID, result)
}

func (s sessionSink) InvalidateCodexRequests(ctx context.Context, connectionID string) error {
	return s.s.store.InvalidateCodexRequests(ctx, s.sessionID, connectionID)
}

func (s sessionSink) BeginCodexAction(ctx context.Context, requestID, fingerprint string) (string, json.RawMessage, error) {
	return s.s.store.BeginCodexAction(ctx, s.sessionID, requestID, fingerprint)
}

func (s sessionSink) CompleteCodexAction(ctx context.Context, requestID, fingerprint, status string, response json.RawMessage) error {
	return s.s.store.CompleteCodexAction(ctx, s.sessionID, requestID, fingerprint, status, response)
}

func (s sessionSink) ConfirmCodexAction(ctx context.Context, requestID string) error {
	return s.s.store.ConfirmCodexAction(ctx, s.sessionID, requestID)
}
