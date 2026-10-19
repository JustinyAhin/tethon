package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"tethon/internal/codex"
	"tethon/internal/protocol"
)

func (s *Server) findNativeCreate(w http.ResponseWriter, r *http.Request, request *protocol.CreateSessionRequest) (string, bool) {
	requestID, err := uuid.Parse(request.RequestID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "native session creation requires a UUID request_id")
		return "", true
	}
	request.RequestID = requestID.String()

	encoded, err := json.Marshal(request)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid native session request")
		return "", true
	}
	checksum := sha256.Sum256(encoded)
	fingerprint := hex.EncodeToString(checksum[:])

	session, found, err := s.store.FindCreatedSession(r.Context(), request.RequestID, fingerprint)
	if err != nil {
		writeNativeError(w, err)
		return "", true
	}
	if found {
		writeJSON(w, http.StatusOK, protocol.CreateSessionResponse{ID: session.ID, Status: session.Status})
		return "", true
	}

	return fingerprint, false
}

func (s *Server) resumeNativeIntent(ctx context.Context, sessionID, fingerprint string, request protocol.CreateSessionRequest) error {
	if err := s.store.BindCreatedSession(ctx, request.RequestID, fingerprint, sessionID); err != nil {
		return err
	}

	actionErr := s.resumeKnownNative(ctx, sessionID, request)
	var uncertain *codex.DeliveryError

	receiptCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if actionErr != nil {
		params, _ := json.Marshal(protocol.ErrorPayload{Message: actionErr.Error()})
		_, _ = s.appendEvent(receiptCtx, sessionID, protocol.EventCodexEvent, protocol.CodexEventPayload{
			Method:   "tethon/inputError",
			ThreadID: request.NativeThreadID,
			Params:   params,
		})
	}
	if errors.As(actionErr, &uncertain) {
		return actionErr
	}

	status, message := "acknowledged", ""
	if actionErr != nil {
		status, message = "rejected", actionErr.Error()
	}

	if err := s.store.CompleteCreatedSession(receiptCtx, request.RequestID, fingerprint, sessionID, status, message); err != nil {
		return &codex.DeliveryError{Err: err}
	}

	return actionErr
}

func (s *Server) resumeKnownNative(ctx context.Context, sessionID string, request protocol.CreateSessionRequest) error {
	if s.getRunning(sessionID) == nil {
		session, err := s.store.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}

		if err := s.recoverCodexSession(ctx, session); err != nil {
			return err
		}
	}

	if request.Prompt == "" {
		return nil
	}

	running, ok := s.getRunning(sessionID).(nativeRunningSession)
	if !ok {
		return errors.New("the native conversation is unavailable")
	}

	_, err := running.InputNative(ctx, protocol.InputRequest{
		Text:                   request.Prompt,
		Mode:                   "start",
		RequestID:              request.RequestID,
		RequestedModel:         request.RequestedModel,
		ReasoningEffort:        request.ReasoningEffort,
		CodexPermissionProfile: request.CodexPermissionProfile,
	}, request.ReasoningEffort)

	return err
}
