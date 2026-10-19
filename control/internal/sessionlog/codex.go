package sessionlog

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"tethon/internal/protocol"
)

var ErrUnsupportedCodexRequest = errors.New("native request is unsupported or incomplete")

// SetCodexState stores a complete observed snapshot. Callers serialize updates
// for each session to prevent an older snapshot from replacing newer state.
func (s *Store) SetCodexState(ctx context.Context, sessionID string, state protocol.CodexSessionState) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(encoded) > protocol.MaxCodexPayloadBytes {
		return protocol.ErrCodexPayloadTooLarge
	}
	result, err := s.db.ExecContext(ctx, `UPDATE sessions SET codex_state = ?, updated_at = ? WHERE id = ?`, string(encoded), formatTime(time.Now().UTC()), sessionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) CreatePendingCodexRequest(ctx context.Context, sessionID string, request protocol.CodexRequestPayload) error {
	if request.Unsupported || request.Truncated || request.ConnectionID == "" || request.Method == "" {
		return ErrUnsupportedCodexRequest
	}
	switch request.Kind {
	case protocol.CodexToolApproval, protocol.CodexUserInput, protocol.CodexMCPRequest:
	default:
		return ErrUnsupportedCodexRequest
	}
	id, err := protocol.CodexRequestID(request.RequestID)
	if err != nil {
		return err
	}
	request.RequestKey = id
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if len(encoded) > protocol.MaxCodexPayloadBytes {
		return protocol.ErrCodexPayloadTooLarge
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO codex_requests (session_id, connection_id, request_id, status, payload_json, created_at)
		SELECT ?, ?, ?, 'pending', ?, ? WHERE EXISTS (SELECT 1 FROM sessions WHERE id = ?)`, sessionID, request.ConnectionID, id, string(encoded), formatTime(time.Now().UTC()), sessionID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) ListPendingCodexRequests(ctx context.Context, sessionID string) ([]protocol.CodexRequestPayload, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT payload_json FROM codex_requests WHERE session_id = ? AND status = 'pending' ORDER BY created_at, connection_id, request_id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	requests := []protocol.CodexRequestPayload{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var request protocol.CodexRequestPayload
		if err := json.Unmarshal([]byte(raw), &request); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	return requests, rows.Err()
}

func (s *Store) ResolveCodexRequest(ctx context.Context, sessionID, connectionID string, requestID, result json.RawMessage) error {
	if len(result) == 0 || !json.Valid(result) {
		return errors.New("native response result must contain valid JSON")
	}
	return s.MarkCodexRequestResolved(ctx, sessionID, connectionID, requestID, result)
}

// MarkCodexRequestResolved also handles native resolution notifications. A nil
// result means the provider resolved the request without reporting its result.
func (s *Store) MarkCodexRequestResolved(ctx context.Context, sessionID, connectionID string, requestID, result json.RawMessage) error {
	id, err := protocol.CodexRequestID(requestID)
	if err != nil {
		return err
	}
	var storedResult any
	if len(result) != 0 {
		if !json.Valid(result) {
			return errors.New("native response result must contain valid JSON")
		}
		if len(result) > protocol.MaxCodexPayloadBytes {
			return protocol.ErrCodexPayloadTooLarge
		}
		storedResult = string(result)
	}
	updated, err := s.db.ExecContext(ctx, `UPDATE codex_requests SET status = 'resolved', result_json = ?, resolved_at = ?
		WHERE session_id = ? AND connection_id = ? AND request_id = ? AND status = 'pending'`, storedResult, formatTime(time.Now().UTC()), sessionID, connectionID, id)
	if err != nil {
		return err
	}
	count, err := updated.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return ErrNotFound
	}
	return nil
}

// InvalidateCodexRequests expires pending requests for the supplied OLD
// connection generation. It does not invalidate requests from a new connection.
func (s *Store) InvalidateCodexRequests(ctx context.Context, sessionID, connectionID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE codex_requests SET status = 'stale', resolved_at = ?
		WHERE session_id = ? AND connection_id = ? AND status = 'pending'`, formatTime(time.Now().UTC()), sessionID, connectionID)
	return err
}

func boundCodexEventJSON(eventType string, raw []byte) ([]byte, error) {
	switch eventType {
	case protocol.EventCodexEvent:
		var payload protocol.CodexEventPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		bounded, err := protocol.BoundCodexEvent(payload)
		if err != nil {
			return nil, err
		}
		return json.Marshal(bounded)
	case protocol.EventCodexRequest:
		var payload protocol.CodexRequestPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		bounded, err := protocol.BoundCodexRequest(payload)
		if err != nil {
			return nil, err
		}
		return json.Marshal(bounded)
	case protocol.EventCodexState, protocol.EventCodexRequestResolved:
		if len(raw) > protocol.MaxCodexPayloadBytes {
			return nil, protocol.ErrCodexPayloadTooLarge
		}
	}
	return raw, nil
}
