package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
)

const (
	EventCodexState           = "codex_state"
	EventCodexEvent           = "codex_event"
	EventCodexRequest         = "codex_request"
	EventCodexRequestResolved = "codex_request_resolved"
	MaxCodexPayloadBytes      = 256 * 1024
	WorkspaceExisting         = "existing"
	WorkspaceIsolated         = "isolated"
)

type CodexConnectionState string

const (
	CodexDisconnected    CodexConnectionState = "disconnected"
	CodexConnecting      CodexConnectionState = "connecting"
	CodexConnected       CodexConnectionState = "connected"
	CodexReconnecting    CodexConnectionState = "reconnecting"
	CodexConnectionError CodexConnectionState = "error"
)

type CodexConversationState string

const (
	CodexConversationReady       CodexConversationState = "ready"
	CodexConversationArchived    CodexConversationState = "archived"
	CodexConversationUnavailable CodexConversationState = "unavailable"
)

type CodexTurnState string

const (
	CodexTurnIdle        CodexTurnState = "idle"
	CodexTurnInProgress  CodexTurnState = "in_progress"
	CodexTurnCompleted   CodexTurnState = "completed"
	CodexTurnInterrupted CodexTurnState = "interrupted"
	CodexTurnFailed      CodexTurnState = "failed"
)

// CodexSessionState records the last observed native state, not native history.
// A completed turn does not end the conversation or the Tethon session.
type CodexSessionState struct {
	ThreadID          string                 `json:"thread_id"`
	CWD               string                 `json:"cwd,omitempty"`
	SocketPath        string                 `json:"socket_path"`
	ServerVersion     string                 `json:"server_version,omitempty"`
	ConnectionID      string                 `json:"connection_id"`
	ConnectionState   CodexConnectionState   `json:"connection_state"`
	ConversationState CodexConversationState `json:"conversation_state"`
	TurnState         CodexTurnState         `json:"turn_state"`
	TurnID            string                 `json:"turn_id,omitempty"`
}

type CodexSessionResponse struct {
	Seq             int64                 `json:"seq"`
	State           CodexSessionState     `json:"state"`
	PendingRequests []CodexRequestPayload `json:"pending_requests"`
	Queue           []json.RawMessage     `json:"queue"`
	QueueAvailable  bool                  `json:"queue_available"`
	QueueNextCursor string                `json:"queue_next_cursor,omitempty"`
	WorkspaceOwned  bool                  `json:"workspace_owned"`
}

type CodexConversation struct {
	ThreadID  string `json:"thread_id"`
	Name      string `json:"name,omitempty"`
	Preview   string `json:"preview"`
	CWD       string `json:"cwd"`
	Status    string `json:"status"`
	Model     string `json:"model,omitempty"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type CodexConversationsResponse struct {
	Conversations []CodexConversation `json:"conversations"`
	NextCursor    string              `json:"next_cursor,omitempty"`
}

type CodexHistoryTurn struct {
	ID     string            `json:"id"`
	Status string            `json:"status"`
	Items  []json.RawMessage `json:"items"`
	Error  json.RawMessage   `json:"error,omitempty"`
}

type CodexHistoryResponse struct {
	ThreadID   string             `json:"thread_id"`
	Turns      []CodexHistoryTurn `json:"turns"`
	NextCursor string             `json:"next_cursor,omitempty"`
}

// CodexEventPayload preserves versioned native params, including unknown methods.
// Clients must show unsupported methods without applying inferred state changes.
type CodexEventPayload struct {
	Method        string          `json:"method"`
	ThreadID      string          `json:"thread_id,omitempty"`
	TurnID        string          `json:"turn_id,omitempty"`
	ItemID        string          `json:"item_id,omitempty"`
	RequestID     json.RawMessage `json:"request_id,omitempty"`
	ConnectionID  string          `json:"connection_id,omitempty"`
	Params        json.RawMessage `json:"params,omitempty"`
	Truncated     bool            `json:"truncated,omitempty"`
	OriginalBytes int             `json:"original_bytes,omitempty"`
}

type CodexRequestKind string

const (
	CodexToolApproval   CodexRequestKind = "tool_approval"
	CodexUserInput      CodexRequestKind = "user_input"
	CodexMCPRequest     CodexRequestKind = "mcp"
	CodexUnknownRequest CodexRequestKind = "unknown"
)

// RequestID retains the native JSON string or number. Clients use RequestKey
// without parsing it. ConnectionID identifies the request's connection generation.
// Params and Result retain native response shapes.
type CodexRequestPayload struct {
	RequestKey    string           `json:"request_key"`
	RequestID     json.RawMessage  `json:"request_id"`
	ConnectionID  string           `json:"connection_id"`
	Method        string           `json:"method"`
	Kind          CodexRequestKind `json:"kind"`
	ThreadID      string           `json:"thread_id,omitempty"`
	TurnID        string           `json:"turn_id,omitempty"`
	ItemID        string           `json:"item_id,omitempty"`
	Params        json.RawMessage  `json:"params,omitempty"`
	Unsupported   bool             `json:"unsupported,omitempty"`
	Truncated     bool             `json:"truncated,omitempty"`
	OriginalBytes int              `json:"original_bytes,omitempty"`
}

type CodexRespondRequest struct {
	RequestKey   string          `json:"request_key"`
	RequestID    json.RawMessage `json:"request_id,omitempty"`
	ConnectionID string          `json:"connection_id"`
	Result       json.RawMessage `json:"result"`
}

type CodexRequestResolvedPayload struct {
	RequestKey   string          `json:"request_key"`
	RequestID    json.RawMessage `json:"request_id,omitempty"`
	ConnectionID string          `json:"connection_id"`
	Result       json.RawMessage `json:"result,omitempty"`
}

var ErrCodexPayloadTooLarge = errors.New("native Codex payload exceeds the delivery limit")

// BoundCodexEvent removes oversized params. It retains the native method and IDs.
// Callers must treat Truncated as incomplete data and must not infer missing state.
func BoundCodexEvent(payload CodexEventPayload) (CodexEventPayload, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return CodexEventPayload{}, err
	}
	if len(encoded) <= MaxCodexPayloadBytes {
		return payload, nil
	}

	payload.OriginalBytes = len(encoded)
	payload.Params = nil
	payload.Truncated = true

	encoded, err = json.Marshal(payload)
	if err != nil {
		return CodexEventPayload{}, err
	}
	if len(encoded) > MaxCodexPayloadBytes {
		return CodexEventPayload{}, ErrCodexPayloadTooLarge
	}

	return payload, nil
}

// BoundCodexRequest disables oversized requests. A client must never submit a
// response using omitted choices or truncated params.
func BoundCodexRequest(payload CodexRequestPayload) (CodexRequestPayload, error) {
	key, err := CodexRequestID(payload.RequestID)
	if err != nil {
		return CodexRequestPayload{}, err
	}

	payload.RequestKey = key
	switch payload.Kind {
	case CodexToolApproval, CodexUserInput, CodexMCPRequest:
	default:
		payload.Unsupported = true
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return CodexRequestPayload{}, err
	}
	if len(encoded) <= MaxCodexPayloadBytes {
		return payload, nil
	}

	payload.OriginalBytes = len(encoded)
	payload.Params = nil
	payload.Truncated = true
	payload.Unsupported = true

	encoded, err = json.Marshal(payload)
	if err != nil {
		return CodexRequestPayload{}, err
	}
	if len(encoded) > MaxCodexPayloadBytes {
		return CodexRequestPayload{}, ErrCodexPayloadTooLarge
	}

	return payload, nil
}

// CodexRequestID validates and compacts a native request ID without converting
// numbers to floating point or changing a JSON string into a number.
func CodexRequestID(raw json.RawMessage) (string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	switch value.(type) {
	case string, json.Number:
	default:
		return "", errors.New("native request ID must be a JSON string or number")
	}

	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return "", err
	}
	if compact.Len() > MaxCodexPayloadBytes {
		return "", ErrCodexPayloadTooLarge
	}

	return compact.String(), nil
}
