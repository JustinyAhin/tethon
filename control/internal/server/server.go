package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/sessionlog"
)

type Server struct {
	store     *sessionlog.Store
	broker    *broker
	agents    *agent.Registry
	repoRoots []string
	running   map[string]agent.RunningSession
	runningM  sync.RWMutex
}

type Config struct {
	Agents    *agent.Registry
	RepoRoots []string
}

func New(store *sessionlog.Store) *Server {
	return NewWithAgents(store, agent.DefaultRegistry())
}

func NewWithAgents(store *sessionlog.Store, agents *agent.Registry) *Server {
	return NewWithConfig(store, Config{Agents: agents})
}

func NewWithConfig(store *sessionlog.Store, config Config) *Server {
	agents := config.Agents
	if agents == nil {
		agents = agent.DefaultRegistry()
	}
	repoRoots := make([]string, 0, len(config.RepoRoots))
	for _, root := range config.RepoRoots {
		abs, err := filepath.Abs(root)
		if err != nil {
			repoRoots = append(repoRoots, filepath.Clean(root))
			continue
		}
		realPath, err := filepath.EvalSymlinks(abs)
		if err != nil {
			repoRoots = append(repoRoots, filepath.Clean(abs))
			continue
		}
		repoRoots = append(repoRoots, filepath.Clean(realPath))
	}
	return &Server{
		store:     store,
		broker:    newBroker(),
		agents:    agents,
		repoRoots: repoRoots,
		running:   make(map[string]agent.RunningSession),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/", s.handleSession)
	return mux
}

func (s *Server) normalizeCreateRequest(req protocol.CreateSessionRequest) (protocol.CreateSessionRequest, error) {
	repo, err := normalizeExistingDir(req.Repo)
	if err != nil {
		return protocol.CreateSessionRequest{}, fmt.Errorf("repo must be an existing directory")
	}
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return protocol.CreateSessionRequest{}, fmt.Errorf("repo must be an existing directory")
	}
	if len(s.repoRoots) > 0 && !pathWithinAny(realRepo, s.repoRoots) {
		return protocol.CreateSessionRequest{}, fmt.Errorf("repo must be inside an advertised repo root")
	}

	cwd, err := normalizeCWD(realRepo, req.CWD)
	if err != nil {
		return protocol.CreateSessionRequest{}, err
	}
	req.Repo = realRepo
	req.CWD = cwd
	return req, nil
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
	adapter, ok := s.agents.Get(req.Agent)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown agent")
		return
	}

	normalizedReq, err := s.normalizeCreateRequest(req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := s.store.CreateSession(r.Context(), sessionlog.CreateSessionParams{
		Repo:   normalizedReq.Repo,
		CWD:    normalizedReq.CWD,
		Branch: normalizedReq.Branch,
		Agent:  normalizedReq.Agent,
		Status: "running",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	running, err := adapter.Start(r.Context(), agent.StartRequest{Session: session, Create: normalizedReq}, s.sink(session.ID))
	if err != nil {
		_ = s.store.SetSessionStatus(r.Context(), session.ID, "error")
		_, _ = s.appendEvent(r.Context(), session.ID, protocol.EventError, protocol.ErrorPayload{Message: err.Error()})
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setRunning(session.ID, running)

	writeJSON(w, http.StatusCreated, protocol.CreateSessionResponse{ID: session.ID, Status: session.Status})
}

func normalizeExistingDir(path string) (string, error) {
	if path == "" {
		return "", errors.New("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	clean := filepath.Clean(abs)
	info, err := os.Stat(clean)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return clean, nil
}

func normalizeCWD(repo, cwd string) (string, error) {
	if cwd == "" || cwd == "." {
		return "", nil
	}
	if filepath.IsAbs(cwd) {
		return "", fmt.Errorf("cwd must be relative to repo")
	}
	clean := filepath.Clean(cwd)
	if clean == "." {
		return "", nil
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("cwd must stay inside repo")
	}
	finalPath := filepath.Join(repo, clean)
	info, err := os.Stat(finalPath)
	if err != nil {
		return "", fmt.Errorf("cwd must be an existing directory")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("cwd must be an existing directory")
	}
	realRepo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		return "", fmt.Errorf("repo must be an existing directory")
	}
	realCWD, err := filepath.EvalSymlinks(finalPath)
	if err != nil {
		return "", fmt.Errorf("cwd must be an existing directory")
	}
	if !pathWithin(realCWD, realRepo) {
		return "", fmt.Errorf("cwd must stay inside repo")
	}
	rel, err := filepath.Rel(repo, finalPath)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return "", nil
	}
	return filepath.Clean(rel), nil
}

func pathWithinAny(path string, roots []string) bool {
	for _, root := range roots {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if pathWithin(path, realRoot) {
			return true
		}
	}
	return false
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)))
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
	if len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost {
		s.handleCancel(w, r, sessionID)
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
	running := s.getRunning(sessionID)
	if running == nil {
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.Input(r.Context(), req.Text)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)

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
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	running := s.getRunning(sessionID)
	if running == nil {
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.ResolveApproval(r.Context(), requestID, req.Action, req.Text)
	if err != nil {
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)
	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		writeStoreError(w, err)
		return
	}
	running := s.getRunning(sessionID)
	if running == nil {
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.Cancel(r.Context())
	if err != nil {
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)
	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) appendEvent(ctx context.Context, sessionID, eventType string, payload any) (protocol.Event, error) {
	event, err := s.store.AppendEvent(ctx, sessionID, eventType, payload)
	if err != nil {
		return protocol.Event{}, err
	}
	s.broker.publish(event)
	return event, nil
}

func (s *Server) setRunning(sessionID string, running agent.RunningSession) {
	s.runningM.Lock()
	s.running[sessionID] = running
	s.runningM.Unlock()
}

func (s *Server) getRunning(sessionID string) agent.RunningSession {
	s.runningM.RLock()
	defer s.runningM.RUnlock()
	return s.running[sessionID]
}

func (s *Server) clearRunningIfTerminal(sessionID string, running agent.RunningSession) {
	if !running.Terminal() {
		return
	}
	s.runningM.Lock()
	if s.running[sessionID] == running {
		delete(s.running, sessionID)
	}
	s.runningM.Unlock()
}

func (s *Server) sink(sessionID string) agent.Sink {
	return sessionSink{s: s, sessionID: sessionID}
}

type sessionSink struct {
	s         *Server
	sessionID string
}

func (s sessionSink) AppendEvent(ctx context.Context, eventType string, payload any) (protocol.Event, error) {
	return s.s.appendEvent(ctx, s.sessionID, eventType, payload)
}

func (s sessionSink) CreatePendingApproval(ctx context.Context, requestID string, payload any) error {
	return s.s.store.CreatePendingApproval(ctx, s.sessionID, requestID, payload)
}

func (s sessionSink) ResolveApproval(ctx context.Context, requestID, action string) error {
	return s.s.store.ResolveApproval(ctx, s.sessionID, requestID, action)
}

func (s sessionSink) SetSessionStatus(ctx context.Context, status string) error {
	return s.s.store.SetSessionStatus(ctx, s.sessionID, status)
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

func writeAgentError(w http.ResponseWriter, err error) {
	if errors.Is(err, sessionlog.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if errors.Is(err, agent.ErrBusy) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if errors.Is(err, agent.ErrSessionDone) {
		writeError(w, http.StatusConflict, err.Error())
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
