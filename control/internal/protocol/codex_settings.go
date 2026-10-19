package protocol

// CodexSettingsResponse contains display metadata. It never carries native
// configuration documents, credentials, instruction bodies, or MCP schemas.
type CodexSettingsResponse struct {
	CWD                string                         `json:"cwd"`
	Account            CodexAccountSummary            `json:"account"`
	Defaults           CodexDefaults                  `json:"defaults"`
	ConfigStatus       string                         `json:"config_status"`
	Managed            CodexManagedSummary            `json:"managed"`
	RateLimits         CodexRateLimitsSummary         `json:"rate_limits"`
	Skills             CodexSkillsSummary             `json:"skills"`
	MCP                CodexMCPSummary                `json:"mcp"`
	Collaboration      CodexCollaborationSummary      `json:"collaboration"`
	PermissionProfiles CodexPermissionProfilesSummary `json:"permission_profiles"`
	Warnings           []string                       `json:"warnings"`
}

type CodexAccountSummary struct {
	Status             string `json:"status"`
	Type               string `json:"type,omitempty"`
	PlanType           string `json:"plan_type,omitempty"`
	RequiresOpenAIAuth bool   `json:"requires_openai_auth"`
	LoginCommand       string `json:"login_command,omitempty"`
}

type CodexDefaults struct {
	Model           string `json:"model,omitempty"`
	ModelProvider   string `json:"model_provider,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	ApprovalPolicy  string `json:"approval_policy,omitempty"`
	SandboxMode     string `json:"sandbox_mode,omitempty"`
	ServiceTier     string `json:"service_tier,omitempty"`
	WebSearch       string `json:"web_search,omitempty"`
}

type CodexManagedSummary struct {
	Status                  string    `json:"status"`
	Configured              bool      `json:"configured"`
	ModelProvider           string    `json:"model_provider,omitempty"`
	AllowedApprovalPolicies *[]string `json:"allowed_approval_policies,omitempty"`
	AllowedSandboxModes     *[]string `json:"allowed_sandbox_modes,omitempty"`
	AllowedWebSearchModes   *[]string `json:"allowed_web_search_modes,omitempty"`
}

type CodexRateLimitsSummary struct {
	Status               string                 `json:"status"`
	OrdinaryUsageAllowed *bool                  `json:"ordinary_usage_allowed,omitempty"`
	Buckets              []CodexRateLimitBucket `json:"buckets"`
}

type CodexRateLimitBucket struct {
	ID                  string                `json:"id,omitempty"`
	Name                string                `json:"name,omitempty"`
	Primary             *CodexRateLimitWindow `json:"primary,omitempty"`
	Secondary           *CodexRateLimitWindow `json:"secondary,omitempty"`
	ReachedType         string                `json:"reached_type,omitempty"`
	SpendControlReached *bool                 `json:"spend_control_reached,omitempty"`
}

type CodexRateLimitWindow struct {
	UsedPercent        float64 `json:"used_percent"`
	WindowDurationMins *int64  `json:"window_duration_mins,omitempty"`
	ResetsAt           *int64  `json:"resets_at,omitempty"`
}

type CodexSkillsSummary struct {
	Status string           `json:"status"`
	Skills []CodexSkillInfo `json:"skills"`
}

type CodexSkillInfo struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Scope       string `json:"scope"`
	Enabled     bool   `json:"enabled"`
}

type CodexMCPSummary struct {
	Status  string               `json:"status"`
	Scope   string               `json:"scope"`
	Servers []CodexMCPServerInfo `json:"servers"`
}

type CodexMCPServerInfo struct {
	Name          string   `json:"name"`
	AuthStatus    string   `json:"auth_status"`
	RuntimeStatus string   `json:"runtime_status,omitempty"`
	Tools         []string `json:"tools"`
	ToolsStatus   string   `json:"tools_status"`
}

type CodexCollaborationSummary struct {
	Status string                   `json:"status"`
	Modes  []CodexCollaborationMode `json:"modes"`
}

type CodexCollaborationMode struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type CodexPermissionProfilesSummary struct {
	Status   string                   `json:"status"`
	Profiles []CodexPermissionProfile `json:"profiles"`
}

type CodexPermissionProfile struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	Allowed     bool   `json:"allowed"`
}
