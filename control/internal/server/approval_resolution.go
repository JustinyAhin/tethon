package server

import (
	"context"
	"encoding/json"

	"tethon/internal/protocol"
)

func (s *Server) notifyApprovalResolved(sessionID string, payload any) {
	if s.approvalResolvedNotifier == nil {
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var identity struct {
		RequestID    json.RawMessage `json:"request_id"`
		RequestKey   string          `json:"request_key"`
		ConnectionID string          `json:"connection_id"`
	}
	if json.Unmarshal(data, &identity) != nil {
		return
	}

	key := identity.RequestKey
	if key == "" {
		if json.Unmarshal(identity.RequestID, &key) != nil {
			return
		}
	}

	if key == "" {
		return
	}
	if identity.ConnectionID != "" {
		key = identity.ConnectionID + ":" + key
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), approvalNotificationTimeout)
		defer cancel()

		if err := s.approvalResolvedNotifier(ctx, protocol.PushApprovalResolvedRequest{SessionID: sessionID, RequestID: key}); err != nil {
			s.logger.Warn("approval_resolution_push_failed", "session_id", sessionID, "request_id", key, "error", err.Error())
		}
	}()
}

func (s *Server) notifyNativeRequest(ctx context.Context, sessionID string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}

	var request protocol.CodexRequestPayload
	if json.Unmarshal(data, &request) != nil || request.RequestKey == "" {
		return
	}

	session, err := s.store.GetSession(ctx, sessionID)
	if err != nil {
		return
	}

	var commandDetails struct {
		Command string `json:"command"`
	}
	_ = json.Unmarshal(request.Params, &commandDetails)

	notification := ApprovalNotification{
		SessionID:     sessionID,
		RequestID:     request.ConnectionID + ":" + request.RequestKey,
		Agent:         "codex-cli",
		Repo:          session.Repo,
		ToolName:      request.Method,
		Command:       commandDetails.Command,
		NativeRequest: &request,
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), approvalNotificationTimeout)
		defer cancel()

		if err := s.approvalNotifier(ctx, notification); err != nil {
			s.logger.Warn("native_request_push_failed", "session_id", sessionID, "error", err.Error())
		}
	}()
}
