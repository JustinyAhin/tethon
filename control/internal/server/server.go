package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"tethon/internal/agent"
	"tethon/internal/protocol"
	"tethon/internal/repo"
	"tethon/internal/sessionlog"
	"tethon/internal/worktree"
)

type Server struct {
	store     *sessionlog.Store
	broker    *broker
	agents    *agent.Registry
	repos     *repo.Registry
	worktrees *worktree.Manager
	repoRoots []string
	logger    *slog.Logger
	running   map[string]agent.RunningSession
	runningM  sync.RWMutex
}

type Config struct {
	Agents    *agent.Registry
	Repos     *repo.Registry
	Worktrees *worktree.Manager
	RepoRoots []string
	Logger    *slog.Logger
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
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
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
		repos:     config.Repos,
		worktrees: config.Worktrees,
		repoRoots: repoRoots,
		logger:    logger,
		running:   make(map[string]agent.RunningSession),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/sessions", s.handleSessions)
	mux.HandleFunc("/sessions/", s.handleSession)
	return mux
}

// resolvedRepo is the outcome of mapping a create request to a concrete repo
// and working tree on disk.
type resolvedRepo struct {
	repoID   string
	repoPath string
	cwd      string
	baseRef  string
}

// resolveRepo maps a create request to a registered repo (by id or by path).
// When no registry is configured (legacy/tests), it falls back to validating
// the raw repo path against advertised roots and no worktree is used.
func (s *Server) resolveRepo(req protocol.CreateSessionRequest) (resolvedRepo, error) {
	if s.repos == nil {
		if req.Repo == "" {
			return resolvedRepo{}, fmt.Errorf("repo is required")
		}
		realRepo, err := resolveRealDir(req.Repo)
		if err != nil {
			return resolvedRepo{}, fmt.Errorf("repo must be an existing directory")
		}
		if len(s.repoRoots) > 0 && !pathWithinAny(realRepo, s.repoRoots) {
			return resolvedRepo{}, fmt.Errorf("repo must be inside an advertised repo root")
		}
		cwd, err := normalizeCWD(realRepo, req.CWD)
		if err != nil {
			return resolvedRepo{}, err
		}
		return resolvedRepo{repoPath: realRepo, cwd: cwd, baseRef: req.BaseRef}, nil
	}

	var rp repo.Repo
	var ok bool
	switch {
	case req.RepoID != "":
		rp, ok = s.repos.Get(req.RepoID)
		if !ok {
			return resolvedRepo{}, fmt.Errorf("unknown repo_id")
		}
	case req.Repo != "":
		realRepo, err := resolveRealDir(req.Repo)
		if err != nil {
			return resolvedRepo{}, fmt.Errorf("repo must be an existing directory")
		}
		rp, ok = s.repos.Find(realRepo)
		if !ok {
			return resolvedRepo{}, fmt.Errorf("repo must be a registered repository")
		}
	default:
		return resolvedRepo{}, fmt.Errorf("repo_id or repo is required")
	}

	cwd, err := normalizeCWD(rp.Path, req.CWD)
	if err != nil {
		return resolvedRepo{}, err
	}
	baseRef := req.BaseRef
	if baseRef == "" {
		baseRef = rp.DefaultBranch
	}
	return resolvedRepo{repoID: rp.ID, repoPath: rp.Path, cwd: cwd, baseRef: baseRef}, nil
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req protocol.CreateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Warn("session_create_rejected", "reason", "invalid json")
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Agent == "" {
		req.Agent = "fake"
	}
	adapter, ok := s.agents.Get(req.Agent)
	if !ok {
		s.logger.Warn("session_create_rejected", "agent", req.Agent, "repo_id", req.RepoID, "repo", req.Repo, "cwd", req.CWD, "reason", "unknown agent")
		writeError(w, http.StatusBadRequest, "unknown agent")
		return
	}

	resolved, err := s.resolveRepo(req)
	if err != nil {
		s.logger.Warn("session_create_rejected", "agent", req.Agent, "repo_id", req.RepoID, "repo", req.Repo, "cwd", req.CWD, "reason", err.Error())
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	session, err := s.store.CreateSession(r.Context(), sessionlog.CreateSessionParams{
		RepoID:  resolved.repoID,
		Repo:    resolved.repoPath,
		CWD:     resolved.cwd,
		Branch:  req.Branch,
		BaseRef: resolved.baseRef,
		Agent:   req.Agent,
		Backend: agentBackend(req.Agent),
		CLI:     agentCLI(req.Agent),
		Status:  "running",
	})
	if err != nil {
		s.logger.Error("session_create_failed", "agent", req.Agent, "repo", resolved.repoPath, "cwd", resolved.cwd, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logger.Info("session_create_accepted", "session_id", session.ID, "agent", session.Agent, "repo_id", resolved.repoID, "repo", session.Repo, "cwd", session.CWD, "status", session.Status)

	workDir := joinCWD(resolved.repoPath, resolved.cwd)
	if s.worktrees != nil && resolved.repoID != "" {
		wt, err := s.worktrees.Create(r.Context(), worktree.CreateOptions{
			ID:       session.ID,
			RepoID:   resolved.repoID,
			RepoPath: resolved.repoPath,
			BaseRef:  resolved.baseRef,
			Branch:   req.Branch,
		})
		if err != nil {
			_ = s.store.SetSessionStatus(r.Context(), session.ID, "error")
			_, _ = s.appendEvent(r.Context(), session.ID, protocol.EventError, protocol.ErrorPayload{Message: err.Error()})
			s.logger.Error("session_worktree_failed", "session_id", session.ID, "repo_id", resolved.repoID, "error", err.Error())
			// A branch/worktree collision means another session owns it: that is a
			// client conflict, not a server fault.
			if errors.Is(err, worktree.ErrBranchExists) || errors.Is(err, worktree.ErrWorktreeExists) {
				writeError(w, http.StatusConflict, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		if err := s.store.SetSessionWorktree(r.Context(), session.ID, wt.Path, wt.Branch); err != nil {
			s.logger.Error("session_worktree_persist_failed", "session_id", session.ID, "error", err.Error())
		}
		session.WorktreePath = wt.Path
		session.Branch = wt.Branch
		workDir = joinCWD(wt.Path, resolved.cwd)
		s.logger.Info("session_worktree_created", "session_id", session.ID, "repo_id", resolved.repoID, "worktree", wt.Path, "branch", wt.Branch)
	}

	// The session_started event records the resolved repo/cwd, not the raw
	// request, so clients see where the agent actually runs.
	createForEvent := req
	createForEvent.RepoID = resolved.repoID
	createForEvent.Repo = resolved.repoPath
	createForEvent.CWD = resolved.cwd
	createForEvent.BaseRef = resolved.baseRef

	running, err := adapter.Start(r.Context(), agent.StartRequest{Session: session, Create: createForEvent, WorkDir: workDir}, s.sink(session.ID))
	if err != nil {
		_ = s.store.SetSessionStatus(r.Context(), session.ID, "error")
		_, _ = s.appendEvent(r.Context(), session.ID, protocol.EventError, protocol.ErrorPayload{Message: err.Error()})
		if s.worktrees != nil && session.WorktreePath != "" {
			if removeErr := s.worktrees.Remove(context.Background(), session.ID); removeErr != nil {
				s.logger.Warn("session_worktree_cleanup_failed", "session_id", session.ID, "error", removeErr.Error())
			}
		}
		s.logger.Error("session_agent_start_failed", "session_id", session.ID, "agent", session.Agent, "error", err.Error())
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.setRunning(session.ID, running)
	// If the agent reached a terminal state before setRunning ran (a fast
	// goroutine), reconcile now so the map does not hold a dead session.
	s.clearRunningIfTerminal(session.ID, running)
	s.logger.Info("session_agent_started", "session_id", session.ID, "agent", session.Agent)

	writeJSON(w, http.StatusCreated, protocol.CreateSessionResponse{ID: session.ID, Status: session.Status})
}

// resolveRealDir validates that path is an existing directory and resolves it
// through any symlinks, matching how repo paths are normalized at registration.
func resolveRealDir(path string) (string, error) {
	clean, err := normalizeExistingDir(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(clean)
}

// joinCWD appends a repo-relative cwd to a base directory.
func joinCWD(base, cwd string) string {
	if cwd == "" {
		return base
	}
	return filepath.Join(base, cwd)
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
		s.logger.Warn("session_input_rejected", "session_id", sessionID, "reason", "invalid json")
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Text == "" {
		s.logger.Warn("session_input_rejected", "session_id", sessionID, "reason", "text is required")
		writeError(w, http.StatusBadRequest, "text is required")
		return
	}
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		s.logger.Warn("session_input_rejected", "session_id", sessionID, "text_len", len(req.Text), "reason", err.Error())
		writeStoreError(w, err)
		return
	}
	running := s.getRunning(sessionID)
	if running == nil {
		s.logger.Warn("session_input_rejected", "session_id", sessionID, "text_len", len(req.Text), "reason", "session is not active")
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.Input(r.Context(), req.Text)
	if err != nil {
		s.logger.Warn("session_input_rejected", "session_id", sessionID, "text_len", len(req.Text), "reason", err.Error())
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)
	s.logger.Info("session_input_accepted", "session_id", sessionID, "text_len", len(req.Text), "event_count", len(events))

	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) handleApproval(w http.ResponseWriter, r *http.Request, sessionID, requestID string) {
	var req protocol.ResolveApprovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "reason", "invalid json")
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Action != "approve" && req.Action != "deny" && req.Action != "reply" {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "action", req.Action, "reason", "invalid action")
		writeError(w, http.StatusBadRequest, "action must be approve, deny, or reply")
		return
	}
	if req.Action == "reply" && req.Text == "" {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "action", req.Action, "reason", "text is required")
		writeError(w, http.StatusBadRequest, "text is required for reply")
		return
	}
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "action", req.Action, "reason", err.Error())
		writeStoreError(w, err)
		return
	}
	running := s.getRunning(sessionID)
	if running == nil {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "action", req.Action, "reason", "session is not active")
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.ResolveApproval(r.Context(), requestID, req.Action, req.Text)
	if err != nil {
		s.logger.Warn("session_approval_rejected", "session_id", sessionID, "request_id", requestID, "action", req.Action, "reason", err.Error())
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)
	s.logger.Info("session_approval_accepted", "session_id", sessionID, "request_id", requestID, "action", req.Action, "event_count", len(events))
	writeJSON(w, http.StatusAccepted, map[string]any{"events": events})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request, sessionID string) {
	if _, err := s.store.GetSession(r.Context(), sessionID); err != nil {
		s.logger.Warn("session_cancel_rejected", "session_id", sessionID, "reason", err.Error())
		writeStoreError(w, err)
		return
	}
	running := s.getRunning(sessionID)
	if running == nil {
		s.logger.Warn("session_cancel_rejected", "session_id", sessionID, "reason", "session is not active")
		writeError(w, http.StatusConflict, "session is not active")
		return
	}

	events, err := running.Cancel(r.Context())
	if err != nil {
		s.logger.Warn("session_cancel_rejected", "session_id", sessionID, "reason", err.Error())
		writeAgentError(w, err)
		return
	}
	s.clearRunningIfTerminal(sessionID, running)
	s.logger.Info("session_cancel_accepted", "session_id", sessionID, "event_count", len(events))
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

func (s *Server) clearRunning(sessionID string) {
	s.runningM.Lock()
	delete(s.running, sessionID)
	s.runningM.Unlock()
}

// onSessionTerminal applies the cleanup policy when a session reaches a terminal
// status. It fires for every terminal transition — including those that happen
// asynchronously inside an agent goroutine — because the agent reports them
// through SetSessionStatus on the sink.
//
// Policy: done/cancelled removes the worktree and its branch; error/interrupted
// retains them so the failed work can be inspected.
func (s *Server) onSessionTerminal(ctx context.Context, sessionID, status string) {
	s.clearRunning(sessionID)
	if s.worktrees == nil {
		return
	}
	switch status {
	case "done":
		if err := s.worktrees.Remove(ctx, sessionID); err != nil && !errors.Is(err, worktree.ErrNotFound) {
			s.logger.Warn("session_worktree_remove_failed", "session_id", sessionID, "error", err.Error())
		}
	case "error":
		if wt, ok := s.worktrees.Get(sessionID); ok {
			s.logger.Info("session_worktree_retained", "session_id", sessionID, "worktree", wt.Path, "branch", wt.Branch)
		}
	}
}

// Recover reconciles persisted state after a restart. Sessions left running or
// awaiting approval when the daemon stopped have no live process, so they are
// marked errored (interrupted) and their worktrees re-adopted so they are
// tracked. It never deletes anything — interrupted work is retained for
// inspection, consistent with the failure cleanup policy.
func (s *Server) Recover(ctx context.Context) error {
	sessions, err := s.store.ListSessionsByStatus(ctx, "running", "awaiting_approval")
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if s.worktrees != nil && s.repos != nil && session.WorktreePath != "" && session.RepoID != "" {
			if repo, ok := s.repos.Get(session.RepoID); ok {
				if info, statErr := os.Stat(session.WorktreePath); statErr == nil && info.IsDir() {
					s.worktrees.Adopt(worktree.Worktree{
						ID:      session.ID,
						RepoID:  session.RepoID,
						Path:    session.WorktreePath,
						Branch:  session.Branch,
						BaseRef: session.BaseRef,
					}, repo.Path)
				}
			}
		}
		_, _ = s.appendEvent(ctx, session.ID, protocol.EventError, protocol.ErrorPayload{Message: "daemon restarted; session interrupted"})
		if err := s.store.SetSessionStatus(ctx, session.ID, "error"); err != nil {
			s.logger.Warn("session_recover_failed", "session_id", session.ID, "error", err.Error())
			continue
		}
		s.logger.Info("session_recovered_interrupted", "session_id", session.ID, "worktree", session.WorktreePath)
	}
	return nil
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
	if err := s.s.store.SetSessionStatus(ctx, s.sessionID, status); err != nil {
		return err
	}
	if status == "done" || status == "error" {
		s.s.onSessionTerminal(ctx, s.sessionID, status)
	}
	return nil
}

func (s sessionSink) SetSessionModel(ctx context.Context, model string) error {
	return s.s.store.SetSessionModel(ctx, s.sessionID, model)
}

func agentBackend(agentName string) string {
	switch agentName {
	case "claude-cli":
		return "anthropic"
	case "fake":
		return "fake"
	default:
		return ""
	}
}

func agentCLI(agentName string) string {
	switch agentName {
	case "claude-cli":
		return "claude"
	case "fake":
		return "fake"
	default:
		return ""
	}
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
