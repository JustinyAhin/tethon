package protocol

import (
	"encoding/json"
	"time"
)

const (
	EventSessionStarted   = "session_started"
	EventOutput           = "output"
	EventAwaitingApproval = "awaiting_approval"
	EventApprovalResolved = "approval_resolved"
	EventInputReceived    = "input_received"
	EventDone             = "done"
	EventError            = "error"
)

type Event struct {
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload"`
}

type Session struct {
	ID        string    `json:"id"`
	Repo      string    `json:"repo"`
	CWD       string    `json:"cwd"`
	Branch    string    `json:"branch"`
	Agent     string    `json:"agent"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateSessionRequest struct {
	HostID string `json:"host_id,omitempty"`
	Repo   string `json:"repo"`
	CWD    string `json:"cwd"`
	Branch string `json:"branch"`
	Agent  string `json:"agent"`
}

type CreateSessionResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type RegisterHostRequest struct {
	HostID    string   `json:"host_id"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	Agents    []string `json:"agents"`
	RepoRoots []string `json:"repo_roots"`
}

type HostSummary struct {
	HostID    string    `json:"host_id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	Agents    []string  `json:"agents"`
	RepoRoots []string  `json:"repo_roots"`
	LastSeen  time.Time `json:"last_seen"`
	Online    bool      `json:"online"`
}

type InputRequest struct {
	Text string `json:"text"`
}

type ResolveApprovalRequest struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
}

type OutputPayload struct {
	Text string `json:"text"`
}

type ApprovalPayload struct {
	RequestID string `json:"request_id"`
	Command   string `json:"command"`
	Reason    string `json:"reason"`
}

type ApprovalResolvedPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Text      string `json:"text,omitempty"`
}

type InputReceivedPayload struct {
	Text string `json:"text"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}
