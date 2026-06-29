package hub

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type actionEventsOutput struct {
	Status int `status:"202"`
	Body   protocol.ActionEventsResponse
}

func (s *Server) registerHumaRoutes(api huma.API) {
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
		OperationID:   "cancelSession",
		Method:        http.MethodPost,
		Path:          "/sessions/{host_id}/{local_id}/cancel",
		Summary:       "Cancel an active session",
		Tags:          []string{"sessions"},
		DefaultStatus: http.StatusAccepted,
		Errors:        []int{http.StatusBadGateway, http.StatusServiceUnavailable},
	}, s.cancelSessionHuma)
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
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, strings.TrimRight(host.URL, "/")+"/sessions", nil)
		if err == nil {
			var status int
			status, err = s.doJSON(req, &hostResp)
			if status >= 300 {
				err = fmt.Errorf("host %s returned %d", host.HostID, status)
			}
		}
		cancel()
		if err != nil {
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
	status, body, err := s.postJSONRaw(ctx, strings.TrimRight(host.URL, "/")+"/sessions", hostReq)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	if status >= 300 {
		return nil, newResponseError(status, body)
	}
	if err := json.Unmarshal(body, &hostResp); err != nil {
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

	target := strings.TrimRight(host.URL, "/") + "/sessions/" + input.LocalID + "/stream?from=" + strconv.FormatInt(input.From, 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return nil, newResponseError(resp.StatusCode, body)
	}

	return &huma.StreamResponse{
		Body: func(ctx huma.Context) {
			defer resp.Body.Close()
			ctx.SetHeader("Content-Type", "text/event-stream")
			ctx.SetHeader("Cache-Control", "no-cache")
			ctx.SetHeader("Connection", "keep-alive")

			writer := ctx.BodyWriter()
			flusher, _ := writer.(http.Flusher)
			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadBytes('\n')
				if len(line) > 0 {
					if _, writeErr := writer.Write(line); writeErr != nil {
						return
					}
					if flusher != nil {
						flusher.Flush()
					}
				}
				if err != nil {
					return
				}
			}
		},
	}, nil
}

func (s *Server) sendInputHuma(ctx context.Context, input *sendInputInput) (*actionEventsOutput, error) {
	return s.postAction(ctx, input.HostID, input.LocalID, "input", input.Body)
}

func (s *Server) resolveApprovalHuma(ctx context.Context, input *approvalInput) (*actionEventsOutput, error) {
	return s.postAction(ctx, input.HostID, input.LocalID, "approvals/"+input.RequestID, input.Body)
}

func (s *Server) cancelSessionHuma(ctx context.Context, input *sessionPathInput) (*actionEventsOutput, error) {
	return s.postAction(ctx, input.HostID, input.LocalID, "cancel", map[string]string{})
}

func (s *Server) postAction(ctx context.Context, hostID, localID, suffix string, body any) (*actionEventsOutput, error) {
	host, ok := s.onlineHost(hostID)
	if !ok {
		return nil, huma.Error503ServiceUnavailable("host is offline or unknown")
	}

	target := strings.TrimRight(host.URL, "/") + "/sessions/" + localID + "/" + suffix
	status, raw, err := s.postJSONRaw(ctx, target, body)
	if err != nil {
		return nil, huma.Error502BadGateway(err.Error())
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

func (s *Server) postJSONRaw(ctx context.Context, target string, body any) (int, []byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, respBody, nil
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
