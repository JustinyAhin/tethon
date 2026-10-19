package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	"tethon/internal/codex"
	"tethon/internal/protocol"
)

type codexActionResponse struct {
	Events []protocol.Event `json:"events"`
	Error  string           `json:"error,omitempty"`
}

func (s *codexSession) InputNative(ctx context.Context, request protocol.InputRequest, effort string) ([]protocol.Event, error) {
	requestID, err := uuid.Parse(request.RequestID)
	if err != nil {
		return nil, errors.New("native input requires a UUID transport request_id")
	}
	request.RequestID = requestID.String()

	if len(request.Text) > protocol.MaxCodexPayloadBytes {
		return nil, protocol.ErrCodexPayloadTooLarge
	}

	s.actions.Lock()
	defer s.actions.Unlock()

	payload := mustJSON(struct {
		Request protocol.InputRequest `json:"request"`
		Effort  string                `json:"effort"`
	}{Request: request, Effort: effort})
	digest := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(digest[:])
	status, response, err := s.sink.BeginCodexAction(ctx, request.RequestID, fingerprint)
	if err != nil {
		return nil, err
	}

	if status != "new" {
		if status == "uncertain" {
			return nil, &codex.DeliveryError{Err: errors.New("input delivery is unconfirmed; reconcile native history before any new attempt")}
		}

		var cached codexActionResponse
		if err := json.Unmarshal(response, &cached); err != nil {
			return nil, err
		}
		if status == "rejected" {
			return nil, errors.New(cached.Error)
		}

		return cached.Events, nil
	}

	events, actionErr := s.sendInput(ctx, request, effort)
	var uncertain *codex.DeliveryError
	if errors.As(actionErr, &uncertain) {
		return nil, actionErr
	}

	status = "acknowledged"
	result := codexActionResponse{Events: events}
	if actionErr != nil {
		status = "rejected"
		result.Error = actionErr.Error()
	}

	if err := s.sink.CompleteCodexAction(context.Background(), request.RequestID, fingerprint, status, mustJSON(result)); err != nil {
		return nil, &codex.DeliveryError{Err: err}
	}

	return events, actionErr
}

func (s *codexSession) confirmHistoryActions(ctx context.Context, history protocol.CodexHistoryResponse) error {
	for _, turn := range history.Turns {
		for _, raw := range turn.Items {
			var item struct {
				Type     string `json:"type"`
				ClientID string `json:"clientId"`
			}

			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			if item.Type != "userMessage" || item.ClientID == "" {
				continue
			}

			if err := s.sink.ConfirmCodexAction(ctx, item.ClientID); err != nil {
				return err
			}
		}
	}

	return nil
}

func (s *codexSession) confirmQueueActions(ctx context.Context, entries []json.RawMessage) error {
	for _, raw := range entries {
		var entry struct {
			ClientID string `json:"clientUserMessageId"`
		}

		if err := json.Unmarshal(raw, &entry); err != nil {
			return err
		}
		if entry.ClientID == "" {
			continue
		}

		if err := s.sink.ConfirmCodexAction(ctx, entry.ClientID); err != nil {
			return err
		}
	}

	return nil
}
