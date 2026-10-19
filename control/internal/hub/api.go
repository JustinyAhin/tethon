package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"tethon/internal/protocol"
)

var configureHumaErrorsOnce sync.Once

type hubStatusError struct {
	status  int
	Message string `json:"error"`
}

func (e *hubStatusError) Error() string {
	return e.Message
}

func (e *hubStatusError) GetStatus() int {
	return e.status
}

func configureHumaErrors() {
	configureHumaErrorsOnce.Do(func() {
		huma.NewError = func(status int, message string, _ ...error) huma.StatusError {
			return &hubStatusError{status: status, Message: message}
		}
		huma.NewErrorWithContext = func(_ huma.Context, status int, message string, errs ...error) huma.StatusError {
			return huma.NewError(status, message, errs...)
		}
	})
}

func (s *Server) humaHandler() http.Handler {
	mux, _ := s.newHumaAPI()
	return mux
}

func (s *Server) OpenAPI() *huma.OpenAPI {
	_, api := s.newHumaAPI()
	return api.OpenAPI()
}

func (s *Server) newHumaAPI() (*http.ServeMux, huma.API) {
	configureHumaErrors()

	mux := http.NewServeMux()
	config := huma.DefaultConfig("Tethon Hub API", "0.1.0")
	config.CreateHooks = nil
	config.SchemasPath = ""
	config.DocsPath = "/docs"
	config.OpenAPIPath = "/openapi"
	if config.Components == nil {
		config.Components = &huma.Components{}
	}
	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearerAuth": {
			Type:        "http",
			Scheme:      "bearer",
			Description: "Shared bearer token for deployed hub clients.",
		},
	}
	config.Security = []map[string][]string{{"bearerAuth": []string{}}}

	api := humago.New(mux, config)
	s.registerHumaRoutes(api)
	return mux, api
}

type listHostsOutput struct {
	Body protocol.ListHostsResponse
}

type registerDeviceInput struct {
	Body protocol.RegisterDeviceRequest
}

type registerDeviceOutput struct {
	Body protocol.RegisterDeviceResponse
}

type unregisterDeviceInput struct {
	DeviceID string `path:"device_id"`
}

type unregisterDeviceOutput struct {
	Status int `status:"204"`
}

type pushTestInput struct {
	Body protocol.PushTestRequest
}

type pushTestOutput struct {
	Body protocol.PushTestResponse
}

type pushApprovalTestInput struct {
	Body protocol.PushApprovalTestRequest
}

type registerHostInput struct {
	Body protocol.RegisterHostRequest
}

type hostOutput struct {
	Status int `status:"201"`
	Body   protocol.HostSummary
}

type heartbeatInput struct {
	HostID string `path:"host_id"`
}

type heartbeatOutput struct {
	Body protocol.HostSummary
}

type listSessionsOutput struct {
	Body protocol.ListSessionsResponse
}

type agentModelsInput struct {
	HostID string `path:"host_id"`
	Agent  string `path:"agent"`
}

type agentModelsOutput struct {
	Body protocol.ModelCatalog
}

type codexSettingsInput struct {
	HostID string `path:"host_id"`
	CWD    string `query:"cwd"`
}

type codexSettingsOutput struct {
	Body protocol.CodexSettingsResponse
}

type createSessionInput struct {
	Body protocol.CreateSessionRequest
}

type createSessionOutput struct {
	Status int `status:"201"`
	Body   protocol.CreateSessionResponse
}

type sessionPathInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
}

type cancelSessionInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
	Body    *protocol.StopRequest
}

type codexSessionOutput struct {
	Body protocol.CodexSessionResponse
}

type codexConversationsInput struct {
	HostID string `path:"host_id"`
	Cursor string `query:"cursor"`
	Search string `query:"search"`
}

type codexConversationsOutput struct {
	Body protocol.CodexConversationsResponse
}

type codexHistoryInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
	Cursor  string `query:"cursor"`
	Limit   int    `query:"limit" default:"20" minimum:"1" maximum:"100"`
}

type codexHistoryOutput struct {
	Body protocol.CodexHistoryResponse
}

type streamSessionInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
	From    int64  `query:"from" minimum:"0" default:"0"`
}

type sendInputInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
	Body    protocol.InputRequest
}

type approvalInput struct {
	HostID    string `path:"host_id"`
	LocalID   string `path:"local_id"`
	RequestID string `path:"request_id"`
	Body      protocol.ResolveApprovalRequest
}

type codexRespondInput struct {
	HostID  string `path:"host_id"`
	LocalID string `path:"local_id"`
	Body    protocol.CodexRespondRequest
}

type actionEventsOutput struct {
	Status int `status:"202"`
	Body   protocol.ActionEventsResponse
}

type deleteSessionOutput struct {
	Status int `status:"204"`
}

type repoRefsInput struct {
	HostID string `path:"host_id"`
	RepoID string `path:"repo_id"`
}

type repoRefsOutput struct {
	Body protocol.RepoRefsResponse
}

func (s *Server) registerHumaRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "getCodexSettings",
		Method:      http.MethodGet,
		Path:        "/hosts/{host_id}/agents/codex-cli/settings",
		Summary:     "Read native Codex account, defaults, skills, and tool status",
		Tags:        []string{"hosts"},
		Errors:      []int{http.StatusBadRequest, http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.getCodexSettingsHuma)
	huma.Register(api, huma.Operation{
		OperationID: "listCodexConversations",
		Method:      http.MethodGet,
		Path:        "/hosts/{host_id}/agents/codex-cli/conversations",
		Summary:     "List native Codex conversations on a host",
		Tags:        []string{"sessions"},
		Errors:      []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.listCodexConversationsHuma)
	huma.Register(api, huma.Operation{
		OperationID: "getCodexHistory",
		Method:      http.MethodGet,
		Path:        "/sessions/{host_id}/{local_id}/history",
		Summary:     "Read a page of native Codex turn history",
		Tags:        []string{"sessions"},
		Errors:      []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.getCodexHistoryHuma)
	huma.Register(api, huma.Operation{
		OperationID: "listAgentModels",
		Method:      http.MethodGet,
		Path:        "/hosts/{host_id}/agents/{agent}/models",
		Summary:     "List provider models available to an agent on a host",
		Tags:        []string{"hosts"},
		Errors:      []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.listAgentModelsHuma)
	huma.Register(api, huma.Operation{
		OperationID:      "registerDevice",
		Method:           http.MethodPost,
		Path:             "/devices/register",
		Summary:          "Register a mobile device for push notifications",
		Tags:             []string{"devices"},
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadRequest, http.StatusServiceUnavailable},
	}, s.registerDeviceHuma)

	huma.Register(api, huma.Operation{
		OperationID:   "unregisterDevice",
		Method:        http.MethodDelete,
		Path:          "/devices/{device_id}",
		Summary:       "Unregister a mobile device from push notifications",
		Tags:          []string{"devices"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusNotFound, http.StatusServiceUnavailable},
	}, s.unregisterDeviceHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "sendTestPush",
		Method:           http.MethodPost,
		Path:             "/push/test",
		Summary:          "Send a test push notification to registered devices",
		Tags:             []string{"push"},
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.sendTestPushHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "sendApprovalTestPush",
		Method:           http.MethodPost,
		Path:             "/push/approvals/test",
		Summary:          "Send a test approval push notification to registered devices",
		Tags:             []string{"push"},
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadRequest, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.sendApprovalTestPushHuma)

	huma.Register(api, huma.Operation{
		OperationID: "listHosts",
		Method:      http.MethodGet,
		Path:        "/hosts",
		Summary:     "List registered hosts",
		Tags:        []string{"hosts"},
		Errors:      []int{http.StatusMethodNotAllowed},
	}, s.listHostsHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "registerHost",
		Method:           http.MethodPost,
		Path:             "/hosts/register",
		Summary:          "Register or refresh a host",
		Tags:             []string{"hosts"},
		DefaultStatus:    http.StatusCreated,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadRequest},
	}, s.registerHostHuma)

	huma.Register(api, huma.Operation{
		OperationID: "heartbeatHost",
		Method:      http.MethodPost,
		Path:        "/hosts/{host_id}/heartbeat",
		Summary:     "Refresh a host heartbeat",
		Tags:        []string{"hosts"},
		Errors:      []int{http.StatusNotFound},
	}, s.heartbeatHostHuma)

	huma.Register(api, huma.Operation{
		OperationID: "listSessions",
		Method:      http.MethodGet,
		Path:        "/sessions",
		Summary:     "List sessions across online hosts",
		Tags:        []string{"sessions"},
		Errors:      []int{http.StatusBadGateway},
	}, s.listSessionsHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "createSession",
		Method:           http.MethodPost,
		Path:             "/sessions",
		Summary:          "Create a session on a host",
		Tags:             []string{"sessions"},
		DefaultStatus:    http.StatusCreated,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadRequest, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.createSessionHuma)

	eventSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeOf(protocol.Event{}))
	huma.Register(api, huma.Operation{
		OperationID: "streamSession",
		Method:      http.MethodGet,
		Path:        "/sessions/{host_id}/{local_id}/stream",
		Summary:     "Stream session events from a cursor",
		Tags:        []string{"sessions"},
		Errors:      []int{http.StatusBadGateway, http.StatusServiceUnavailable},
		Responses: map[string]*huma.Response{
			"200": {
				Description: "Server-sent events. Each data frame contains a protocol Event.",
				Content: map[string]*huma.MediaType{
					"text/event-stream": {Schema: eventSchema},
				},
			},
		},
	}, s.streamSessionHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "sendSessionInput",
		Method:           http.MethodPost,
		Path:             "/sessions/{host_id}/{local_id}/input",
		Summary:          "Send input to a session",
		Tags:             []string{"sessions"},
		DefaultStatus:    http.StatusAccepted,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.sendInputHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "resolveSessionApproval",
		Method:           http.MethodPost,
		Path:             "/sessions/{host_id}/{local_id}/approvals/{request_id}",
		Summary:          "Resolve a pending approval",
		Tags:             []string{"sessions"},
		DefaultStatus:    http.StatusAccepted,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.resolveApprovalHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "respondCodexRequest",
		Method:           http.MethodPost,
		Path:             "/sessions/{host_id}/{local_id}/requests/respond",
		Summary:          "Respond to a native Codex request using its exact result shape",
		Tags:             []string{"sessions"},
		DefaultStatus:    http.StatusAccepted,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.respondCodexRequestHuma)

	huma.Register(api, huma.Operation{
		OperationID: "getCodexSession",
		Method:      http.MethodGet,
		Path:        "/sessions/{host_id}/{local_id}/native",
		Summary:     "Read native Codex state and pending requests",
		Tags:        []string{"sessions"},
		Errors:      []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.getCodexSessionHuma)

	huma.Register(api, huma.Operation{
		OperationID:      "cancelSession",
		Method:           http.MethodPost,
		Path:             "/sessions/{host_id}/{local_id}/cancel",
		Summary:          "Cancel an active session",
		Tags:             []string{"sessions"},
		DefaultStatus:    http.StatusAccepted,
		SkipValidateBody: true,
		Errors:           []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.cancelSessionHuma)

	huma.Register(api, huma.Operation{
		OperationID:   "deleteSession",
		Method:        http.MethodDelete,
		Path:          "/sessions/{host_id}/{local_id}",
		Summary:       "Delete a terminal session",
		Tags:          []string{"sessions"},
		DefaultStatus: http.StatusNoContent,
		Errors: []int{
			http.StatusNotFound,
			http.StatusConflict,
			http.StatusBadGateway,
			http.StatusServiceUnavailable,
		},
	}, s.deleteSessionHuma)

	huma.Register(api, huma.Operation{
		OperationID: "listRepoRefs",
		Method:      http.MethodGet,
		Path:        "/repos/{host_id}/{repo_id}/refs",
		Summary:     "List a repo's branches and tags",
		Tags:        []string{"repos"},
		Errors:      []int{http.StatusNotFound, http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.listRepoRefsHuma)

	s.registerCodexLifecycle(api)
}

func (s *Server) registerDeviceHuma(ctx context.Context, input *registerDeviceInput) (*registerDeviceOutput, error) {
	if s.devices == nil {
		return nil, huma.Error503ServiceUnavailable("device store is not configured")
	}
	req := input.Body
	if err := validateDevice(req); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	resp, err := s.devices.Register(ctx, req)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &registerDeviceOutput{Body: resp}, nil
}

func (s *Server) unregisterDeviceHuma(ctx context.Context, input *unregisterDeviceInput) (*unregisterDeviceOutput, error) {
	if s.devices == nil {
		return nil, huma.Error503ServiceUnavailable("device store is not configured")
	}
	if err := s.devices.Unregister(ctx, input.DeviceID); err != nil {
		if errors.Is(err, errDeviceNotFound) {
			return nil, huma.Error404NotFound("device not found")
		}
		return nil, huma.Error500InternalServerError(err.Error())
	}
	return &unregisterDeviceOutput{Status: http.StatusNoContent}, nil
}

func (s *Server) sendTestPushHuma(ctx context.Context, input *pushTestInput) (*pushTestOutput, error) {
	resp, err := s.sendPush(ctx, func(devices []ExpoDevice) (protocol.PushTestResponse, error) {
		return s.push.SendTest(ctx, devices, input.Body)
	})
	if err != nil {
		return nil, err
	}
	return &pushTestOutput{Body: resp}, nil
}

func (s *Server) sendApprovalTestPushHuma(ctx context.Context, input *pushApprovalTestInput) (*pushTestOutput, error) {
	if err := validatePushApprovalTest(input.Body); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	resp, err := s.sendApprovalPush(ctx, input.Body)
	if err != nil {
		return nil, err
	}
	return &pushTestOutput{Body: resp}, nil
}

func (s *Server) handleApprovalNotify(w http.ResponseWriter, r *http.Request, hostID string) {
	var req protocol.PushApprovalTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	req.HostID = hostID
	if err := validatePushApprovalTest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.approvalPushDuplicate(req) {
		writeJSON(w, http.StatusOK, protocol.PushTestResponse{})
		return
	}
	resp, err := s.sendApprovalPush(r.Context(), req)
	if err != nil {
		writeHubStatusError(w, err)
		return
	}
	s.recordApprovalPush(req)
	s.logger.Info("hub_approval_push_sent",
		"host_id", req.HostID,
		"session_id", req.SessionID,
		"request_id", req.RequestID,
		"registered", resp.Registered,
		"attempted", resp.Attempted,
		"accepted", resp.Accepted,
		"rejected", resp.Rejected,
	)
	writeJSON(w, http.StatusAccepted, resp)
}

func (s *Server) sendApprovalPush(ctx context.Context, req protocol.PushApprovalTestRequest) (protocol.PushTestResponse, error) {
	resp, err := s.sendPush(ctx, func(devices []ExpoDevice) (protocol.PushTestResponse, error) {
		return s.push.SendApproval(ctx, devices, req)
	})
	return resp, err
}

func (s *Server) sendPush(ctx context.Context, send func([]ExpoDevice) (protocol.PushTestResponse, error)) (protocol.PushTestResponse, error) {
	if s.devices == nil {
		return protocol.PushTestResponse{}, huma.Error503ServiceUnavailable("device store is not configured")
	}
	if s.push == nil {
		return protocol.PushTestResponse{}, huma.Error503ServiceUnavailable("push sender is not configured")
	}
	devices, err := s.devices.ActiveExpoDevices(ctx)
	if err != nil {
		return protocol.PushTestResponse{}, huma.Error500InternalServerError(err.Error())
	}
	resp, err := send(devices)
	if err != nil {
		return protocol.PushTestResponse{}, huma.Error502BadGateway(err.Error())
	}
	for _, rejection := range resp.Rejections {
		if rejection.Code != expoDeviceNotRegister {
			continue
		}
		if err := s.devices.Disable(ctx, rejection.DeviceID); err != nil && !errors.Is(err, errDeviceNotFound) {
			return protocol.PushTestResponse{}, huma.Error500InternalServerError(err.Error())
		}
	}
	return resp, nil
}

func (s *Server) approvalPushDuplicate(req protocol.PushApprovalTestRequest) bool {
	key := approvalPushDedupeKey(req)
	s.mu.RLock()
	_, ok := s.approvalPushKeys[key]
	s.mu.RUnlock()
	return ok
}

func (s *Server) recordApprovalPush(req protocol.PushApprovalTestRequest) {
	key := approvalPushDedupeKey(req)
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.approvalPushKeys[key]; ok {
		return
	}
	s.approvalPushKeys[key] = struct{}{}
	s.approvalPushOrder = append(s.approvalPushOrder, key)
	for len(s.approvalPushOrder) > approvalPushDedupeLimit {
		oldest := s.approvalPushOrder[0]
		s.approvalPushOrder = s.approvalPushOrder[1:]
		delete(s.approvalPushKeys, oldest)
	}
}

func approvalPushDedupeKey(req protocol.PushApprovalTestRequest) string {
	return strings.Join([]string{
		strings.TrimSpace(req.HostID),
		strings.TrimSpace(req.SessionID),
		strings.TrimSpace(req.RequestID),
	}, "\x00")
}

func writeHubStatusError(w http.ResponseWriter, err error) {
	type statusError interface {
		error
		GetStatus() int
	}
	if err, ok := err.(statusError); ok {
		writeError(w, err.GetStatus(), err.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func (s *Server) listHostsHuma(_ context.Context, _ *struct{}) (*listHostsOutput, error) {
	s.mu.RLock()
	hostIDs := make([]string, 0, len(s.hosts))
	for hostID := range s.hosts {
		hostIDs = append(hostIDs, hostID)
	}
	s.mu.RUnlock()
	sort.Strings(hostIDs)

	summaries := make([]protocol.HostSummary, 0, len(hostIDs))
	for _, hostID := range hostIDs {
		summaries = append(summaries, s.summary(hostID))
	}
	return &listHostsOutput{Body: protocol.ListHostsResponse{Hosts: summaries}}, nil
}

func (s *Server) registerHostHuma(_ context.Context, input *registerHostInput) (*hostOutput, error) {
	req := input.Body
	if err := validateHost(req); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if req.Name == "" {
		req.Name = req.HostID
	}

	s.mu.Lock()
	s.hosts[req.HostID] = hostRecord{RegisterHostRequest: req, LastSeen: s.now().UTC()}
	s.mu.Unlock()

	summary := s.summary(req.HostID)
	s.logHostRegistered(summary)
	return &hostOutput{Status: http.StatusCreated, Body: summary}, nil
}

func (s *Server) heartbeatHostHuma(_ context.Context, input *heartbeatInput) (*heartbeatOutput, error) {
	s.mu.Lock()
	record, ok := s.hosts[input.HostID]
	if ok {
		record.LastSeen = s.now().UTC()
		s.hosts[input.HostID] = record
	}
	s.mu.Unlock()
	if !ok {
		s.logger.Warn("hub_host_heartbeat_unknown", "host_id", input.HostID)
		return nil, huma.Error404NotFound("host not found")
	}
	summary := s.summary(input.HostID)
	s.logHostHeartbeat(summary)
	return &heartbeatOutput{Body: summary}, nil
}

func (s *Server) listSessionsHuma(ctx context.Context, _ *struct{}) (*listSessionsOutput, error) {
	hosts := s.onlineHosts()
	summaries := make([]protocol.SessionSummary, 0)
	for _, host := range hosts {
		var hostResp protocol.ListSessionsResponse
		reqCtx, cancel := context.WithTimeout(ctx, sessionListFanoutTimeout)
		status, body, err := s.reverseRequestRaw(reqCtx, host.HostID, http.MethodGet, "/sessions", "", nil)
		cancel()
		if err != nil {
			return nil, reverseHumaError(err)
		}
		if status >= 300 {
			return nil, newResponseError(status, body)
		}
		if err := json.Unmarshal(body, &hostResp); err != nil {
			return nil, huma.Error502BadGateway(err.Error())
		}
		for _, session := range hostResp.Sessions {
			session.HostID = host.HostID
			session.ID = host.HostID + "/" + session.ID
			summaries = append(summaries, session)
		}
	}
	sort.Slice(summaries, func(i, j int) bool {
		if !summaries[i].CreatedAt.Equal(summaries[j].CreatedAt) {
			return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
		}
		if summaries[i].HostID != summaries[j].HostID {
			return summaries[i].HostID < summaries[j].HostID
		}
		return summaries[i].ID < summaries[j].ID
	})
	return &listSessionsOutput{Body: protocol.ListSessionsResponse{Sessions: summaries}}, nil
}

func (s *Server) listAgentModelsHuma(ctx context.Context, input *agentModelsInput) (*agentModelsOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodGet, "/agents/"+url.PathEscape(input.Agent)+"/models", "", nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var catalog protocol.ModelCatalog
	if err := json.Unmarshal(body, &catalog); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}

	return &agentModelsOutput{Body: catalog}, nil
}

func (s *Server) getCodexSettingsHuma(ctx context.Context, input *codexSettingsInput) (*codexSettingsOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	query := url.Values{}
	if input.CWD != "" {
		query.Set("cwd", input.CWD)
	}

	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodGet, "/agents/codex-cli/settings", query.Encode(), nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var settings protocol.CodexSettingsResponse
	if err := json.Unmarshal(body, &settings); err != nil {
		return nil, huma.Error502BadGateway("invalid native settings response")
	}

	return &codexSettingsOutput{Body: settings}, nil
}

func (s *Server) createSessionHuma(ctx context.Context, input *createSessionInput) (*createSessionOutput, error) {
	req := input.Body
	if req.HostID == "" {
		return nil, huma.Error400BadRequest("host_id is required")
	}

	host, ok := s.onlineHost(req.HostID)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	hostReq := req
	hostReq.HostID = ""
	var hostResp protocol.CreateSessionResponse
	body, err := json.Marshal(hostReq)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	status, raw, err := s.reverseRequestRaw(ctx, host.HostID, http.MethodPost, "/sessions", "", body)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, raw)
	}
	if err := json.Unmarshal(raw, &hostResp); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	hostResp.ID = req.HostID + "/" + hostResp.ID
	return &createSessionOutput{Status: http.StatusCreated, Body: hostResp}, nil
}

func (s *Server) streamSessionHuma(ctx context.Context, input *streamSessionInput) (*huma.StreamResponse, error) {
	host, ok := s.onlineHost(input.HostID)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	pending, start, err := s.reverseStream(ctx, host.HostID, "/sessions/"+input.LocalID+"/stream", "from="+strconv.FormatInt(input.From, 10))
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if start.Status >= 300 {
		defer pending.close(false)
		status, body, err := collectReverseResponse(ctx, pending, start.Status)
		if err != nil {
			return nil, reverseHumaError(err)
		}
		return nil, newResponseError(status, body)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", "text/event-stream")
			ctx.SetHeader("Cache-Control", "no-cache")
			ctx.SetHeader("Connection", "keep-alive")

			writer := ctx.BodyWriter()
			flusher, _ := writer.(http.Flusher)
			writeReverseStream(ctx.Context(), writer, flusher, pending)
		},
	}, nil
}

func (s *Server) sendInputHuma(ctx context.Context, input *sendInputInput) (*actionEventsOutput, error) {
	return s.postAction(ctx, input.HostID, input.LocalID, "input", input.Body)
}

func (s *Server) resolveApprovalHuma(ctx context.Context, input *approvalInput) (*actionEventsOutput, error) {
	return s.postAction(ctx, input.HostID, input.LocalID, "approvals/"+input.RequestID, input.Body)
}

func (s *Server) respondCodexRequestHuma(ctx context.Context, input *codexRespondInput) (*actionEventsOutput, error) {
	if _, err := protocol.CodexRequestID(json.RawMessage(input.Body.RequestKey)); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if input.Body.ConnectionID == "" || len(input.Body.Result) == 0 || !json.Valid(input.Body.Result) {
		return nil, huma.Error400BadRequest("connection_id and a valid native result are required")
	}

	encoded, err := json.Marshal(input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	if len(encoded) > protocol.MaxCodexPayloadBytes {
		return nil, huma.Error400BadRequest(protocol.ErrCodexPayloadTooLarge.Error())
	}

	return s.postAction(ctx, input.HostID, input.LocalID, "requests/respond", input.Body)
}

func (s *Server) cancelSessionHuma(ctx context.Context, input *cancelSessionInput) (*actionEventsOutput, error) {
	body := input.Body
	if body == nil {
		body = &protocol.StopRequest{}
	}
	return s.postAction(ctx, input.HostID, input.LocalID, "cancel", body)
}

func (s *Server) getCodexSessionHuma(ctx context.Context, input *sessionPathInput) (*codexSessionOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodGet, "/sessions/"+input.LocalID+"/native", "", nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var snapshot protocol.CodexSessionResponse
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	if snapshot.PendingRequests == nil {
		snapshot.PendingRequests = []protocol.CodexRequestPayload{}
	}

	return &codexSessionOutput{Body: snapshot}, nil
}

func (s *Server) listCodexConversationsHuma(ctx context.Context, input *codexConversationsInput) (*codexConversationsOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	if input.Search != "" {
		query.Set("search", input.Search)
	}

	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodGet, "/agents/codex-cli/conversations", query.Encode(), nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var response protocol.CodexConversationsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	if response.Conversations == nil {
		response.Conversations = []protocol.CodexConversation{}
	}

	return &codexConversationsOutput{Body: response}, nil
}

func (s *Server) getCodexHistoryHuma(ctx context.Context, input *codexHistoryInput) (*codexHistoryOutput, error) {
	if _, ok := s.onlineHost(input.HostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	}
	query.Set("limit", strconv.Itoa(input.Limit))

	status, body, err := s.reverseRequestRaw(ctx, input.HostID, http.MethodGet, "/sessions/"+input.LocalID+"/history", query.Encode(), nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}

	var response protocol.CodexHistoryResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	if response.Turns == nil {
		response.Turns = []protocol.CodexHistoryTurn{}
	}

	return &codexHistoryOutput{Body: response}, nil
}

func (s *Server) deleteSessionHuma(ctx context.Context, input *sessionPathInput) (*deleteSessionOutput, error) {
	host, ok := s.onlineHost(input.HostID)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	status, raw, err := s.reverseRequestRaw(ctx, host.HostID, http.MethodDelete, "/sessions/"+input.LocalID, "", nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, raw)
	}
	return &deleteSessionOutput{Status: status}, nil
}

func (s *Server) listRepoRefsHuma(ctx context.Context, input *repoRefsInput) (*repoRefsOutput, error) {
	host, ok := s.onlineHost(input.HostID)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	status, body, err := s.reverseRequestRaw(ctx, host.HostID, http.MethodGet, "/repos/"+input.RepoID+"/refs", "", nil)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}
	var refs protocol.RepoRefsResponse
	if err := json.Unmarshal(body, &refs); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &repoRefsOutput{Body: refs}, nil
}

func (s *Server) postAction(ctx context.Context, hostID, localID, suffix string, body any) (*actionEventsOutput, error) {
	if _, ok := s.onlineHost(hostID); !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	data, err := json.Marshal(body)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	status, raw, err := s.reverseRequestRaw(ctx, hostID, http.MethodPost, "/sessions/"+localID+"/"+suffix, "", data)
	if err != nil {
		return nil, reverseHumaError(err)
	}
	if status >= 300 {
		return nil, newResponseError(status, raw)
	}
	var out protocol.ActionEventsResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	return &actionEventsOutput{Status: status, Body: out}, nil
}

func newResponseError(status int, body []byte) error {
	message := strings.TrimSpace(string(body))
	var errResp protocol.ErrorResponse
	if err := json.Unmarshal(body, &errResp); err == nil && errResp.Error != "" {
		message = errResp.Error
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return huma.NewError(status, message)
}

func reverseHumaError(err error) error {
	if errors.Is(err, errReverseUnavailable) {
		return huma.Error503ServiceUnavailable("host is offline or unknown")
	}
	return huma.Error502BadGateway(err.Error())
}

func collectReverseResponse(ctx context.Context, pending *reversePending, status int) (int, []byte, error) {
	var out bytes.Buffer
	for {
		select {
		case chunk := <-pending.body:
			_, _ = out.Write(chunk)
		case done := <-pending.done:
			drainBody(&out, pending)
			if done.Type == "response_error" {
				if done.Status > 0 {
					return done.Status, errorBody(done.Error), nil
				}
				return 0, nil, responseFrameError(done)
			}
			return status, out.Bytes(), nil
		case <-ctx.Done():
			pending.close(true)
			return 0, nil, ctx.Err()
		}
	}
}
