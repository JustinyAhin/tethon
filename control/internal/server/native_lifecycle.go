package server

import (
	"context"
	"errors"
	"net/http"

	"tethon/internal/protocol"
	"tethon/internal/worktree"
)

type nativeConversationActions interface {
	ChangeConversation(context.Context, string) error
}

func (s *Server) handleNativeConversationAction(w http.ResponseWriter, r *http.Request, sessionID, action string) {
	s.nativeLifecycleMu.Lock()
	defer s.nativeLifecycleMu.Unlock()

	running, ok := s.getRunning(sessionID).(nativeConversationActions)
	if !ok {
		writeError(w, http.StatusConflict, "reconnect the native conversation before changing it")
		return
	}

	if err := running.ChangeConversation(r.Context(), action); err != nil {
		writeNativeError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, protocol.ActionEventsResponse{Events: []protocol.Event{}})
}

func (s *Server) handleNativeWorkspaceCleanup(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Keep Tethon restore and cleanup actions from changing the same workspace.
	s.nativeLifecycleMu.Lock()
	defer s.nativeLifecycleMu.Unlock()

	session, err := s.store.GetSession(r.Context(), sessionID)
	if err != nil {
		writeStoreError(w, err)
		return
	}

	if session.Codex == nil || session.WorktreePath == "" || s.worktrees == nil {
		writeError(w, http.StatusConflict, "this conversation has no Tethon-owned isolated workspace")
		return
	}

	state := *session.Codex
	if running, ok := s.getRunning(sessionID).(nativeRunningSession); ok {
		snapshot, err := running.NativeSnapshot(r.Context())
		if err != nil {
			writeNativeError(w, err)
			return
		}

		state = snapshot.State
	}

	inactiveConversation := state.ConversationState == protocol.CodexConversationArchived || state.ConversationState == protocol.CodexConversationUnavailable
	if state.TurnState == protocol.CodexTurnInProgress || !inactiveConversation {
		writeError(w, http.StatusConflict, "archive or delete the inactive conversation before workspace cleanup")
		return
	}

	owned, ok := s.worktrees.Get(sessionID)
	if !ok || owned.Path != session.WorktreePath {
		writeError(w, http.StatusConflict, "the workspace ownership record is unavailable")
		return
	}

	if err := s.worktrees.RemoveClean(r.Context(), sessionID); err != nil {
		if errors.Is(err, worktree.ErrNotOwned) {
			writeError(w, http.StatusConflict, "the host does not own this workspace")
			return
		}

		writeError(w, http.StatusConflict, "workspace cleanup failed; check for dirty or untracked files on the host")
		return
	}

	if err := s.store.SetSessionWorktree(r.Context(), sessionID, "", session.Branch); err != nil {
		writeStoreError(w, err)
		return
	}

	writeJSON(w, http.StatusAccepted, protocol.ActionEventsResponse{Events: []protocol.Event{}})
}
