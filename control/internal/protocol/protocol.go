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
	EventToolResult       = "tool_result"
	EventInputReceived    = "input_received"
	EventUsage            = "usage"
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
	ID             string             `json:"id"`
	RepoID         string             `json:"repo_id,omitempty"`
	Repo           string             `json:"repo"`
	CWD            string             `json:"cwd"`
	Branch         string             `json:"branch"`
	BaseRef        string             `json:"base_ref,omitempty"`
	WorktreePath   string             `json:"worktree_path,omitempty"`
	Agent          string             `json:"agent"`
	Backend        string             `json:"backend,omitempty"`
	CLI            string             `json:"cli,omitempty"`
	Model          string             `json:"model,omitempty"`           // Provider-observed runtime model.
	RequestedModel string             `json:"requested_model,omitempty"` // User choice. Empty preserves the agent default.
	Status         string             `json:"status"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	Codex          *CodexSessionState `json:"codex,omitempty"`
}

type SessionSummary struct {
	ID             string             `json:"id"`
	HostID         string             `json:"host_id,omitempty"`
	RepoID         string             `json:"repo_id,omitempty"`
	Repo           string             `json:"repo"`
	CWD            string             `json:"cwd"`
	Branch         string             `json:"branch"`
	BaseRef        string             `json:"base_ref,omitempty"`
	Agent          string             `json:"agent"`
	Backend        string             `json:"backend,omitempty"`
	CLI            string             `json:"cli,omitempty"`
	Model          string             `json:"model,omitempty"`
	RequestedModel string             `json:"requested_model,omitempty"`
	Status         string             `json:"status"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	LastSeq        int64              `json:"last_seq"`
	Codex          *CodexSessionState `json:"codex,omitempty"`
}

type ListSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

type ListHostsResponse struct {
	Hosts []HostSummary `json:"hosts"`
}

type CreateSessionRequest struct {
	RequestID              string `json:"request_id,omitempty"`
	HostID                 string `json:"host_id,omitempty"`
	RepoID                 string `json:"repo_id,omitempty"`
	Repo                   string `json:"repo,omitempty"`
	CWD                    string `json:"cwd"`
	Branch                 string `json:"branch"`
	BaseRef                string `json:"base_ref,omitempty"`
	Agent                  string `json:"agent"`
	Prompt                 string `json:"prompt,omitempty"`
	PermissionMode         string `json:"permission_mode,omitempty"`
	RequestedModel         string `json:"requested_model,omitempty"`
	NativeThreadID         string `json:"native_thread_id,omitempty"`
	WorkspaceMode          string `json:"workspace_mode,omitempty"`
	ReasoningEffort        string `json:"reasoning_effort,omitempty"`
	CodexPermissionProfile string `json:"codex_permission_profile,omitempty"`
}

const (
	ModelCatalogReady       = "ready"
	ModelCatalogUnavailable = "unavailable"
)

// ModelCatalog contains choices from the selected agent's provider-owned source.
// An unavailable catalog permits the agent default, but no explicit selection.
type ModelCatalog struct {
	Agent   string      `json:"agent"`
	Status  string      `json:"status"`
	Source  string      `json:"source,omitempty"`
	Message string      `json:"message,omitempty"`
	Models  []ModelInfo `json:"models"`
}

type ModelInfo struct {
	ID                     string                 `json:"id"`
	Label                  string                 `json:"label"`
	Provider               string                 `json:"provider"`
	Description            string                 `json:"description,omitempty"`
	IsDefault              bool                   `json:"is_default,omitempty"`
	ReasoningEfforts       []ModelReasoningEffort `json:"reasoning_efforts,omitempty"`
	DefaultReasoningEffort string                 `json:"default_reasoning_effort,omitempty"`
	InputModalities        []string               `json:"input_modalities,omitempty"`
}

type ModelReasoningEffort struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

type SessionStartedPayload = CreateSessionRequest

type CreateSessionResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// RepoInfo advertises a host-registered repository to clients.
type RepoInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

// RepoRefsResponse lists a repository's existing refs so clients can offer a
// base_ref picker. Branches lead with the default branch; both slices are
// always present (empty rather than null).
type RepoRefsResponse struct {
	Branches []string `json:"branches"`
	Tags     []string `json:"tags"`
}

type RegisterDeviceRequest struct {
	DeviceID    string `json:"device_id"`
	PushService string `json:"push_service"`
	PushToken   string `json:"push_token"`
	Platform    string `json:"platform"`
	AppID       string `json:"app_id,omitempty"`
	AppVersion  string `json:"app_version,omitempty"`
}

type RegisterDeviceResponse struct {
	DeviceID    string    `json:"device_id"`
	PushService string    `json:"push_service"`
	Platform    string    `json:"platform"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type PushTestRequest struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type PushApprovalTestRequest struct {
	NativeRequestKey   string `json:"native_request_key,omitempty"`
	NativeConnectionID string `json:"native_connection_id,omitempty"`
	NativeMethod       string `json:"native_method,omitempty"`
	HostID             string `json:"host_id"`
	SessionID          string `json:"session_id"`
	RequestID          string `json:"request_id"`
	Agent              string `json:"agent,omitempty"`
	Repo               string `json:"repo,omitempty"`
	ToolName           string `json:"tool_name,omitempty"`
	Command            string `json:"command,omitempty"`
}

type PushApprovalResolvedRequest struct {
	HostID    string `json:"host_id"`
	SessionID string `json:"session_id"`
	RequestID string `json:"request_id"`
}

type PushTestResponse struct {
	Registered int                 `json:"registered"`
	Attempted  int                 `json:"attempted"`
	Accepted   int                 `json:"accepted"`
	Rejected   int                 `json:"rejected"`
	Rejections []PushTestRejection `json:"rejections"`
}

type PushTestRejection struct {
	DeviceID string `json:"device_id"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message,omitempty"`
}

type AgentAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type RegisterHostRequest struct {
	AgentStatus map[string]AgentAvailability `json:"agent_status,omitempty"`
	HostID      string                       `json:"host_id"`
	Name        string                       `json:"name"`
	URL         string                       `json:"url"`
	Agents      []string                     `json:"agents"`
	RepoRoots   []string                     `json:"repo_roots"`
	Repos       []RepoInfo                   `json:"repos,omitempty"`
}

type HostSummary struct {
	AgentStatus map[string]AgentAvailability `json:"agent_status,omitempty"`
	HostID      string                       `json:"host_id"`
	Name        string                       `json:"name"`
	URL         string                       `json:"url"`
	Agents      []string                     `json:"agents"`
	RepoRoots   []string                     `json:"repo_roots"`
	Repos       []RepoInfo                   `json:"repos,omitempty"`
	LastSeen    time.Time                    `json:"last_seen"`
	Online      bool                         `json:"online"`
}

type InputRequest struct {
	Text                   string `json:"text"`
	Mode                   string `json:"mode,omitempty" enum:"start,steer,queue"`
	TurnID                 string `json:"turn_id,omitempty"`
	RequestID              string `json:"request_id,omitempty"` // Client idempotency key. Independent of native request IDs.
	RequestedModel         string `json:"requested_model,omitempty"`
	ReasoningEffort        string `json:"reasoning_effort,omitempty"`
	CodexPermissionProfile string `json:"codex_permission_profile,omitempty"`
}

type StopRequest struct {
	TurnID string `json:"turn_id,omitempty"`
}

type ResolveApprovalRequest struct {
	Action string `json:"action"`
	Text   string `json:"text,omitempty"`
}

type ActionEventsResponse struct {
	Events []Event `json:"events"`
}

type ListSessionResourcesResponse struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Snapshots   []SessionResourceSnapshot `json:"snapshots"`
	Warnings    []ResourceWarning         `json:"warnings,omitempty"`
}

type SessionResourceResponse struct {
	GeneratedAt time.Time               `json:"generated_at"`
	Snapshot    SessionResourceSnapshot `json:"snapshot"`
}

type SessionResourceSnapshot struct {
	SessionID         string                  `json:"session_id"`
	Status            string                  `json:"status"`
	Agent             string                  `json:"agent"`
	RepoID            string                  `json:"repo_id,omitempty"`
	Repo              string                  `json:"repo"`
	CWD               string                  `json:"cwd,omitempty"`
	WorktreePath      string                  `json:"worktree_path,omitempty"`
	RuntimeSeconds    int64                   `json:"runtime_seconds,omitempty"`
	ProcessRoot       ResourceProcessIdentity `json:"process_root"`
	Processes         []ResourceProcess       `json:"processes"`
	ListeningPorts    []ResourceListeningPort `json:"listening_ports"`
	Aggregate         ResourceAggregate       `json:"aggregate"`
	WorktreeDiskUsage *ResourceDiskUsage      `json:"worktree_disk_usage,omitempty"`
	Warnings          []ResourceWarning       `json:"warnings,omitempty"`
}

type ResourceProcessIdentity struct {
	PID              int       `json:"pid,omitempty"`
	PGID             int       `json:"pgid,omitempty"`
	StartedAt        time.Time `json:"started_at,omitempty"`
	Executable       string    `json:"executable,omitempty"`
	OwnsProcessGroup bool      `json:"owns_process_group,omitempty"`
}

type ResourceProcess struct {
	PID              int    `json:"pid"`
	PPID             int    `json:"ppid,omitempty"`
	PGID             int    `json:"pgid,omitempty"`
	Command          string `json:"command,omitempty"`
	CommandRedacted  bool   `json:"command_redacted,omitempty"`
	CommandTruncated bool   `json:"command_truncated,omitempty"`
	Executable       string `json:"executable,omitempty"`
	RSSBytes         int64  `json:"rss_bytes,omitempty"`
	CPUTimeMS        int64  `json:"cpu_time_ms,omitempty"`
}

type ResourceListeningPort struct {
	PID      int    `json:"pid,omitempty"`
	Protocol string `json:"protocol"`
	Address  string `json:"address,omitempty"`
	Port     int    `json:"port"`
}

type ResourceAggregate struct {
	ProcessCount int   `json:"process_count"`
	RSSBytes     int64 `json:"rss_bytes,omitempty"`
	CPUTimeMS    int64 `json:"cpu_time_ms,omitempty"`
}

type ResourceDiskUsage struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
}

type ResourceWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type OutputPayload struct {
	Text string `json:"text"`
}

type ApprovalPayload struct {
	RequestID string          `json:"request_id"`
	Command   string          `json:"command"`
	Reason    string          `json:"reason"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolInput json.RawMessage `json:"tool_input,omitempty"`
	Agent     string          `json:"agent,omitempty"`
}

type ApprovalResolvedPayload struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
	Text      string `json:"text,omitempty"`
}

type ToolResultPayload struct {
	Agent             string          `json:"agent"`
	ToolName          string          `json:"tool_name"`
	ToolInput         json.RawMessage `json:"tool_input,omitempty"`
	ToolUseID         string          `json:"tool_use_id,omitempty"`
	ApprovalRequestID string          `json:"approval_request_id,omitempty"`
	Status            string          `json:"status"`
	DurationMS        *int64          `json:"duration_ms,omitempty"`
	Response          json.RawMessage `json:"response,omitempty"`
	Error             string          `json:"error,omitempty"`
	IsInterrupt       bool            `json:"is_interrupt,omitempty"`
	Redacted          bool            `json:"redacted,omitempty"`
	Truncated         bool            `json:"truncated,omitempty"`
	MaxBytes          int             `json:"max_bytes,omitempty"`
}

type InputReceivedPayload struct {
	Text string `json:"text"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}

type DonePayload struct {
	Reason string `json:"reason"`
}

type UsagePayload struct {
	Agent   string        `json:"agent"`
	Backend string        `json:"backend"`
	CLI     string        `json:"cli"`
	Model   string        `json:"model,omitempty"`
	Tokens  UsageTokens   `json:"tokens"`
	Cost    *UsageCost    `json:"cost,omitempty"`
	Raw     *UsageRawInfo `json:"raw,omitempty"`
}

type UsageTokens struct {
	Input         int64 `json:"input,omitempty"`
	Output        int64 `json:"output,omitempty"`
	CacheRead     int64 `json:"cache_read,omitempty"`
	CacheWrite    int64 `json:"cache_write,omitempty"`
	CacheWrite5m  int64 `json:"cache_write_5m,omitempty"`
	CacheWrite1h  int64 `json:"cache_write_1h,omitempty"`
	TotalBillable int64 `json:"total_billable,omitempty"`
}

type UsageCost struct {
	Currency       string  `json:"currency"`
	Estimated      bool    `json:"estimated"`
	EstimateStatus string  `json:"estimate_status"`
	TotalUSD       float64 `json:"total_usd"`
	InputUSD       float64 `json:"input_usd,omitempty"`
	OutputUSD      float64 `json:"output_usd,omitempty"`
	CacheReadUSD   float64 `json:"cache_read_usd,omitempty"`
	CacheWriteUSD  float64 `json:"cache_write_usd,omitempty"`
	PricingSource  string  `json:"pricing_source"`
	PricingVersion string  `json:"pricing_version"`
}

type UsageRawInfo struct {
	ProviderUsagePresent bool `json:"provider_usage_present,omitempty"`
}
