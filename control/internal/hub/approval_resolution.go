package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"tethon/internal/protocol"
)

type approvalResolvedPushSender interface {
	SendApprovalResolved(context.Context, []ExpoDevice, protocol.PushApprovalResolvedRequest) (protocol.PushTestResponse, error)
}

func (s *ExpoPushSender) SendApprovalResolved(ctx context.Context, devices []ExpoDevice, req protocol.PushApprovalResolvedRequest) (protocol.PushTestResponse, error) {
	return s.sendMessages(ctx, devices, func(device ExpoDevice) expoPushMessage {
		return expoPushMessage{
			To: device.PushToken,
			Data: map[string]any{
				"kind":       "approval_resolved",
				"host_id":    req.HostID,
				"session_id": req.SessionID,
				"request_id": req.RequestID,
			},
			ContentAvailable: true,
		}
	})
}

func approvalResolvedNotifyHostID(r *http.Request) (string, bool) {
	if r.Method != http.MethodPost {
		return "", false
	}

	rest := strings.TrimPrefix(r.URL.Path, "/hosts/")
	if rest == r.URL.Path {
		return "", false
	}

	hostID, suffix, ok := strings.Cut(rest, "/")
	return hostID, ok && hostID != "" && suffix == "notify/approval-resolved"
}

func (s *Server) handleApprovalResolvedNotify(w http.ResponseWriter, r *http.Request, hostID string) {
	var req protocol.PushApprovalResolvedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.HostID != "" && req.HostID != hostID {
		writeError(w, http.StatusBadRequest, "host_id does not match path")
		return
	}

	req.HostID = hostID
	if strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.RequestID) == "" {
		writeError(w, http.StatusBadRequest, "session_id and request_id are required")
		return
	}

	sender, ok := s.push.(approvalResolvedPushSender)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "approval resolution push sender is not configured")
		return
	}

	resp, err := s.sendPush(r.Context(), func(devices []ExpoDevice) (protocol.PushTestResponse, error) {
		return sender.SendApprovalResolved(r.Context(), devices, req)
	})
	if err != nil {
		writeHubStatusError(w, err)
		return
	}

	s.logger.Info("hub_approval_resolution_push_sent",
		"host_id", req.HostID,
		"session_id", req.SessionID,
		"request_id", req.RequestID,
		"accepted", resp.Accepted,
		"rejected", resp.Rejected,
	)
	writeJSON(w, http.StatusAccepted, resp)
}
