package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

type Server struct {
	store  *sessionlog.Store
	broker *broker
}

func New(store *sessionlog.Store) *Server {
	return &Server{store: store, broker: newBroker()}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/", s.handleSession)
	return mux
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req protocol.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Repo == "" {
		writeError(w, http.StatusBadRequest, "repo is required")
		return
	}
	if req.Agent == "" {
		req.Agent = "fake"
	}
	if req.Agent != "fake" {
		writeError(w, http.StatusBadRequest, "only fake agent is supported")
		return
	}

	session, err := s.store.CreateSession(r.Context(), sessionlog.CreateSessionParams{
		Repo:   req.Repo,
		CWD:    req.CWD,
		Branch: req.Branch,
		Agent:  req.Agent,
		Status: "running",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.seedFakeSession(r.Context(), session.ID, req); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, protocol.CreateSessionResponse{ID: session.ID, Status: session.Status})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/sessions/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	sessionID := parts[0]

	if len(parts) == 2 && parts[1] == "stream" && r.Method == http.MethodGet {
		s.handleStream(w, r, sessionID)
		return
	}
	if len(parts) == 2 && parts[1] == "input" && r.Method == http.MethodPost {
		s.handleInput(w, r, sessionID)
		return
	}
	if len(parts) == 3 && parts[1] == "approvals" && r.Method == http.MethodPost {
		s.handleApproval(w, r, sessionID, parts[2])
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	fromSeq, err := strconv.ParseInt(r.URL.Query().Get("from"), 10, 64)
	if r.URL.Query().Get("from") == "" {
		fromSeq = 0
		err = nil
	}
	if err != nil || fromSeq < 0 {
		writeError(w, http.StatusBadRequest, "from must be a non-negative integer")
		return
	}
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		writeStoreError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming is unavailable")
		return
	}

	events, unsubscribe := s.broker.subscribe(sessionID)
	defer unsubscribe()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	lastSent := fromSeq
	replay, err := s.store.Replay(r.Context(), sessionID, fromSeq)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, event := range replay {
		if err := writeSSE(w, event); err != nil {
			return
		}
		lastSent = event.Seq
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-events:
			if event.Seq <= lastSent {
				continue
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			lastSent = event.Seq
			flusher.Flush()
		}
	}
}

func (s *Server) handleInput(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req protocol.InputRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		writeStoreError(w, err)
		return
	}

	events, err := s.appendEvents(r.Context(), sessionID, []eventDraft{
		{eventType: protocol.EventInputReceived, payload: protocol.InputReceivedPayload{Text: req.Text}},
		{eventType: protocol.EventOutput, payload: protocol.OutputPayload{Text: "fake agent received: " + req.Text}},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if strings.EqualFold(req.Text, "done") || strings.EqualFold(req.Text, "finish") {
		done, err := s.appendEvent(r.Context(), sessionID, protocol.EventDone, map[string]string{"reason": "fake agent finished"})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		events = append(events, done)
		_ = s.store.SetSessionStatus(r.Context(), sessionID, "done")
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request, sessionID, requestID string) {
	var req protocol.ResolveApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Action != "approve" && req.Action != "deny" && req.Action != "reply" {
		writeError(w, http.StatusBadRequest, "action must be approve, deny, or reply")
		return
	}
	if req.Action == "reply" && req.Text == "" {
		writeError(w, http.StatusBadRequest, "text is required for reply")
		return
	}
	if err := s.store.ResolveApproval(r.Context(), sessionID, requestID, req.Action); err != nil {
		writeStoreError(w, err)
		return
	}

	events, err := s.appendEvents(r.Context(), sessionID, []eventDraft{
		{eventType: protocol.EventApprovalResolved, payload: protocol.ApprovalResolvedPayload{RequestID: requestID, Action: req.Action, Text: req.Text}},
		{eventType: protocol.EventOutput, payload: protocol.OutputPayload{Text: "fake approval resolved: " + req.Action}},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) seedFakeSession(ctx context.Context, sessionID string, req protocol.CreateSessionRequest) error {
	requestID := "approval_bootstrap"
	approval := protocol.ApprovalPayload{
		RequestID: requestID,
		Command:   "fake risky command",
		Reason:    "proves pending approval state and replay",
	}
	if err := s.store.CreatePendingApproval(ctx, sessionID, requestID, approval); err != nil {
		return err
	}
	_, err := s.appendEvents(ctx, sessionID, []eventDraft{
		{eventType: protocol.EventSessionStarted, payload: req},
		{eventType: protocol.EventOutput, payload: protocol.OutputPayload{Text: "fake agent ready"}},
		{eventType: protocol.EventAwaitingApproval, payload: approval},
	})
	return err
}

type eventDraft struct {
	eventType string
	payload   any
}

func (s *Server) appendEvents(ctx context.Context, sessionID string, drafts []eventDraft) ([]protocol.Event, error) {
	events := make([]protocol.Event, 0, len(drafts))
	for _, draft := range drafts {
		event, err := s.appendEvent(ctx, sessionID, draft.eventType, draft.payload)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *Server) appendEvent(ctx context.Context, sessionID, eventType string, payload any) (protocol.Event, error) {
	event, err := s.store.AppendEvent(ctx, sessionID, eventType, payload)
	if err != nil {
		return protocol.Event{}, err
	}
	s.broker.publish(event)
	return event, nil
}

func writeSSE(w http.ResponseWriter, event protocol.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", event.Seq, event.Type, data)
	return err
}

func writeStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, sessionlog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
