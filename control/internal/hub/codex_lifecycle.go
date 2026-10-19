package hub

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"tethon/internal/protocol"
)

func (s *Server) registerCodexLifecycle(api huma.API) {
	for _, action := range []struct {
		operation string
		path      string
		summary   string
	}{
		{"archiveCodexConversation", "archive", "Archive an inactive native Codex conversation"},
		{"unarchiveCodexConversation", "unarchive", "Restore an archived native Codex conversation"},
		{"cleanupCodexWorkspace", "workspace/cleanup", "Remove a clean Tethon-owned isolated workspace"},
	} {
		huma.Register(api, huma.Operation{
			OperationID:   action.operation,
			Method:        http.MethodPost,
			Path:          "/sessions/{host_id}/{local_id}/" + action.path,
			Summary:       action.summary,
			Tags:          []string{"sessions"},
			DefaultStatus: http.StatusAccepted,
			Errors:        []int{http.StatusConflict, http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
		}, func(ctx context.Context, input *sessionPathInput) (*actionEventsOutput, error) {
			return s.postAction(ctx, input.HostID, input.LocalID, action.path, struct{}{})
		})
	}

	huma.Register(api, huma.Operation{
		OperationID:   "deleteCodexConversation",
		Method:        http.MethodDelete,
		Path:          "/sessions/{host_id}/{local_id}/conversation",
		Summary:       "Delete native Codex history and preserve working files",
		Tags:          []string{"sessions"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusConflict, http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.deleteCodexConversationHuma)
}

func (s *Server) deleteCodexConversationHuma(ctx context.Context, input *sessionPathInput) (*actionEventsOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	path := "/sessions/" + input.LocalID + "/conversation"
	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodDelete, path, "", nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var response protocol.ActionEventsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}

	return &actionEventsOutput{Status: status, Body: response}, nil
}
