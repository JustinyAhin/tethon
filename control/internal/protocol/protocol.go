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
	ID           string    `json:"id"`
	RepoID       string    `json:"repo_id,omitempty"`
	Repo         string    `json:"repo"`
	CWD          string    `json:"cwd"`
	Branch       string    `json:"branch"`
	BaseRef      string    `json:"base_ref,omitempty"`
	WorktreePath string    `json:"worktree_path,omitempty"`
	Agent        string    `json:"agent"`
	Backend      string    `json:"backend,omitempty"`
	CLI          string    `json:"cli,omitempty"`
	Model        string    `json:"model,omitempty"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type SessionSummary struct {
	ID        string    `json:"id"`
	HostID    string    `json:"host_id,omitempty"`
	RepoID    string    `json:"repo_id,omitempty"`
	Repo      string    `json:"repo"`
	CWD       string    `json:"cwd"`
	Branch    string    `json:"branch"`
	BaseRef   string    `json:"base_ref,omitempty"`
	Agent     string    `json:"agent"`
	Backend   string    `json:"backend,omitempty"`
	CLI       string    `json:"cli,omitempty"`
	Model     string    `json:"model,omitempty"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	LastSeq   int64     `json:"last_seq"`
}

type ListSessionsResponse struct {
	Sessions []SessionSummary `json:"sessions"`
}

type CreateSessionRequest struct {
	HostID  string `json:"host_id,omitempty"`
	RepoID  string `json:"repo_id,omitempty"`
	Repo    string `json:"repo,omitempty"`
	CWD     string `json:"cwd"`
	Branch  string `json:"branch"`
	BaseRef string `json:"base_ref,omitempty"`
	Agent   string `json:"agent"`
	Prompt  string `json:"prompt,omitempty"`
}

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

type RegisterHostRequest struct {
	HostID    string     `json:"host_id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Agents    []string   `json:"agents"`
	RepoRoots []string   `json:"repo_roots"`
	Repos     []RepoInfo `json:"repos,omitempty"`
}

type HostSummary struct {
	HostID    string     `json:"host_id"`
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	Agents    []string   `json:"agents"`
	RepoRoots []string   `json:"repo_roots"`
	Repos     []RepoInfo `json:"repos,omitempty"`
	LastSeen  time.Time  `json:"last_seen"`
	Online    bool       `json:"online"`
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

type InputReceivedPayload struct {
	Text string `json:"text"`
}

type ErrorPayload struct {
	Message string `json:"message"`
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
