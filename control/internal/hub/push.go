package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"tethon/internal/protocol"
	"tethon/internal/resource"
)

const (
	approvalNotificationCategoryID = "tethon_approval"
	approvalNotificationChannelID  = "approvals"
	expoPushSendURL                = "https://exp.host/--/api/v2/push/send"
	expoPushMessageLimit           = 100
	expoDeviceNotRegister          = "DeviceNotRegistered"
)

type PushSender interface {
	SendTest(context.Context, []ExpoDevice, protocol.PushTestRequest) (protocol.PushTestResponse, error)
	SendApproval(context.Context, []ExpoDevice, protocol.PushApprovalTestRequest) (protocol.PushTestResponse, error)
}

type ExpoPushSender struct {
	endpoint    string
	accessToken string
	client      *http.Client
}

type ExpoPushConfig struct {
	Endpoint    string
	AccessToken string
	Client      *http.Client
}

func NewExpoPushSender(cfg ExpoPushConfig) *ExpoPushSender {
	endpoint := cfg.Endpoint
	if endpoint == "" {
		endpoint = expoPushSendURL
	}
	client := cfg.Client
	if client == nil {
		client = http.DefaultClient
	}
	return &ExpoPushSender{
		endpoint:    endpoint,
		accessToken: strings.TrimSpace(cfg.AccessToken),
		client:      client,
	}
}

func (s *ExpoPushSender) SendTest(ctx context.Context, devices []ExpoDevice, req protocol.PushTestRequest) (protocol.PushTestResponse, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Tethon"
	}
	body := strings.TrimSpace(req.Body)
	if body == "" {
		body = "Push test"
	}

	return s.sendMessages(ctx, devices, func(device ExpoDevice) expoPushMessage {
		return expoPushMessage{
			To:    device.PushToken,
			Title: title,
			Body:  body,
		}
	})
}

func (s *ExpoPushSender) SendApproval(ctx context.Context, devices []ExpoDevice, req protocol.PushApprovalTestRequest) (protocol.PushTestResponse, error) {
	return s.sendMessages(ctx, devices, func(device ExpoDevice) expoPushMessage {
		return approvalPushMessage(device.PushToken, req)
	})
}

func (s *ExpoPushSender) sendMessages(ctx context.Context, devices []ExpoDevice, message func(ExpoDevice) expoPushMessage) (protocol.PushTestResponse, error) {
	summary := protocol.PushTestResponse{
		Registered: len(devices),
		Rejections: []protocol.PushTestRejection{},
	}
	for start := 0; start < len(devices); start += expoPushMessageLimit {
		end := min(start+expoPushMessageLimit, len(devices))
		chunk := devices[start:end]
		tickets, err := s.sendChunk(ctx, chunk, message)
		if err != nil {
			return summary, err
		}
		summary.Attempted += len(chunk)
		for i, ticket := range tickets {
			if ticket.Status == "ok" {
				summary.Accepted++
				continue
			}
			summary.Rejected++
			rejection := protocol.PushTestRejection{
				DeviceID: chunk[i].DeviceID,
				Code:     ticket.Details.Error,
				Message:  ticket.Message,
			}
			summary.Rejections = append(summary.Rejections, rejection)
		}
	}
	return summary, nil
}

func (s *ExpoPushSender) sendChunk(ctx context.Context, devices []ExpoDevice, message func(ExpoDevice) expoPushMessage) ([]expoPushTicket, error) {
	messages := make([]expoPushMessage, 0, len(devices))
	for _, device := range devices {
		messages = append(messages, message(device))
	}
	data, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if s.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.accessToken)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("expo push send returned %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}

	var out expoPushResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if len(out.Errors) > 0 {
		return nil, errors.New(out.Errors[0].Message)
	}
	if len(out.Data) != len(devices) {
		return nil, fmt.Errorf("expo push returned %d tickets for %d messages", len(out.Data), len(devices))
	}
	return out.Data, nil
}

type expoPushMessage struct {
	To         string         `json:"to"`
	Title      string         `json:"title"`
	Body       string         `json:"body"`
	Data       map[string]any `json:"data,omitempty"`
	CategoryID string         `json:"categoryId,omitempty"`
	ChannelID  string         `json:"channelId,omitempty"`
}

type expoPushResponse struct {
	Data   []expoPushTicket `json:"data"`
	Errors []expoPushError  `json:"errors"`
}

type expoPushTicket struct {
	Status  string            `json:"status"`
	ID      string            `json:"id,omitempty"`
	Message string            `json:"message,omitempty"`
	Details expoPushErrorInfo `json:"details,omitempty"`
}

type expoPushError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

type expoPushErrorInfo struct {
	Error string `json:"error,omitempty"`
}

func approvalPushMessage(token string, req protocol.PushApprovalTestRequest) expoPushMessage {
	agent := safeContextValue(req.Agent)
	if agent == "" {
		agent = "Agent"
	}
	toolName := safeContextValue(req.ToolName)
	repo := safePathValue(req.Repo)
	command := resource.RedactCommandLine(req.Command, resource.RedactionOptions{MaxBytes: 96})

	bodyTarget := safeContextValue(req.SessionID)
	if repo != "" {
		bodyTarget = repo
	}
	actor := agent
	if toolName != "" {
		actor += "/" + toolName
	}
	body := actor + " needs approval"
	if bodyTarget != "" {
		body += " in " + bodyTarget
	}

	data := map[string]any{
		"kind":       "approval_requested",
		"host_id":    req.HostID,
		"session_id": req.SessionID,
		"request_id": req.RequestID,
		"agent":      agent,
	}
	if repo != "" {
		data["repo"] = repo
	}
	if toolName != "" {
		data["tool_name"] = toolName
	}
	if command.Value != "" {
		data["command_preview"] = command.Value
		if command.Redacted {
			data["command_redacted"] = true
		}
		if command.Truncated {
			data["command_truncated"] = true
		}
	}

	return expoPushMessage{
		To:         token,
		Title:      "Approval needed",
		Body:       body,
		Data:       data,
		CategoryID: approvalNotificationCategoryID,
		ChannelID:  approvalNotificationChannelID,
	}
}

func safeContextValue(value string) string {
	redacted := resource.RedactCommandLine(strings.TrimSpace(value), resource.RedactionOptions{MaxBytes: 64})
	return redacted.Value
}

func safePathValue(value string) string {
	redacted := resource.RedactPath(strings.TrimSpace(value), nil, 96)
	return redacted.Value
}
