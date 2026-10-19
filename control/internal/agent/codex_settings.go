package agent

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"

	"tethon/internal/protocol"
)

type codexSettingsReader interface {
	Call(context.Context, string, any, any) error
}

// ReadSettings reads native defaults without changing the host configuration.
// Model and reasoning options remain owned by ListModels.
func (a *Codex) ReadSettings(ctx context.Context, cwd string) (protocol.CodexSettingsResponse, error) {
	client, _, err := a.connect(ctx)
	if err != nil {
		return protocol.CodexSettingsResponse{}, errors.New("Codex is unavailable on the host; start Codex and run codex login if sign-in is needed")
	}
	defer client.Close()

	return readCodexSettings(ctx, client, cwd, a.config.Experimental), nil
}

func readCodexSettings(ctx context.Context, client codexSettingsReader, cwd string, experimental bool) protocol.CodexSettingsResponse {
	result := protocol.CodexSettingsResponse{
		CWD:          cwd,
		ConfigStatus: "unavailable",
		Warnings:     []string{},
		Account:      protocol.CodexAccountSummary{Status: "unavailable"},
		Managed:      protocol.CodexManagedSummary{Status: "unavailable"},
		RateLimits: protocol.CodexRateLimitsSummary{
			Status:  "unavailable",
			Buckets: []protocol.CodexRateLimitBucket{},
		},
		Skills: protocol.CodexSkillsSummary{
			Status: "unavailable",
			Skills: []protocol.CodexSkillInfo{},
		},
		MCP: protocol.CodexMCPSummary{
			Status:  "unavailable",
			Scope:   "host",
			Servers: []protocol.CodexMCPServerInfo{},
		},
		Collaboration: protocol.CodexCollaborationSummary{
			Status: "unavailable",
			Modes:  []protocol.CodexCollaborationMode{},
		},
		PermissionProfiles: protocol.CodexPermissionProfilesSummary{
			Status:   "unavailable",
			Profiles: []protocol.CodexPermissionProfile{},
		},
	}

	var account struct {
		Account *struct {
			Type string `json:"type"`
			Plan string `json:"planType"`
		} `json:"account"`
		RequiresAuth bool `json:"requiresOpenaiAuth"`
	}

	if err := client.Call(ctx, "account/read", map[string]bool{"refreshToken": false}, &account); err != nil {
		if settingsAuthRequired(err) {
			result.Account.Status = "login_required"
			result.Account.LoginCommand = "codex login"
		}
		result.Warnings = append(result.Warnings, "Could not read the Codex account. Check Codex on the host and run codex login if sign-in expired.")
	} else {
		result.Account.Status = "ready"
		result.Account.RequiresOpenAIAuth = account.RequiresAuth
		if account.Account != nil {
			result.Account.Type = settingsText(account.Account.Type)
			result.Account.PlanType = settingsText(account.Account.Plan)
		} else if account.RequiresAuth {
			result.Account.Status = "login_required"
			result.Account.LoginCommand = "codex login"
			result.Warnings = append(result.Warnings, "Codex needs sign-in. Run codex login on the selected host.")
		}
	}

	readCodexConfig(ctx, client, cwd, &result)
	readCodexRequirements(ctx, client, &result)
	readCodexLimits(ctx, client, &result)
	readCodexSkills(ctx, client, cwd, &result)
	readCodexMCP(ctx, client, &result)

	if experimental {
		readCodexPermissionProfiles(ctx, client, cwd, &result)

		var modes struct {
			Data []struct {
				Name   string `json:"name"`
				Mode   string `json:"mode"`
				Model  string `json:"model"`
				Effort string `json:"reasoning_effort"`
			} `json:"data"`
		}

		if err := client.Call(ctx, "collaborationMode/list", map[string]any{}, &modes); err == nil {
			result.Collaboration.Status = "ready"
			for _, mode := range modes.Data {
				if mode.Mode != "default" && mode.Mode != "plan" {
					continue
				}

				result.Collaboration.Modes = append(result.Collaboration.Modes, protocol.CodexCollaborationMode{
					Name:            settingsText(mode.Name),
					Mode:            mode.Mode,
					Model:           settingsText(mode.Model),
					ReasoningEffort: settingsText(mode.Effort),
				})
			}
		}
	}

	return result
}

func readCodexPermissionProfiles(ctx context.Context, client codexSettingsReader, cwd string, result *protocol.CodexSettingsResponse) {
	params := map[string]any{"cwd": cwd, "limit": 100}
	seen := map[string]bool{}

	for {
		var page struct {
			Data []struct {
				ID          string `json:"id"`
				Description string `json:"description"`
				Allowed     bool   `json:"allowed"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}

		if err := client.Call(ctx, "permissionProfile/list", params, &page); err != nil {
			return
		}

		for _, profile := range page.Data {
			// IDs must remain exact native selectors. Omit IDs that the display
			// sanitizer would alter rather than returning a different selector.
			if profile.ID == "" || settingsText(profile.ID) != profile.ID {
				continue
			}

			result.PermissionProfiles.Profiles = append(result.PermissionProfiles.Profiles, protocol.CodexPermissionProfile{
				ID:          profile.ID,
				Description: settingsText(profile.Description),
				Allowed:     profile.Allowed,
			})
		}

		if page.Next == "" {
			result.PermissionProfiles.Status = "ready"
			return
		}
		if seen[page.Next] || len(seen) >= 32 {
			return
		}

		seen[page.Next] = true
		params["cursor"] = page.Next
	}
}

func readCodexConfig(ctx context.Context, client codexSettingsReader, cwd string, result *protocol.CodexSettingsResponse) {
	// Decode only display fields. Native config can contain credentials and
	// arbitrary provider, tool, and instruction data; never forward that document.
	var response struct {
		Config struct {
			Model    string          `json:"model"`
			Provider string          `json:"model_provider"`
			Effort   string          `json:"model_reasoning_effort"`
			Approval json.RawMessage `json:"approval_policy"`
			Sandbox  string          `json:"sandbox_mode"`
			Tier     string          `json:"service_tier"`
			Search   string          `json:"web_search"`
		} `json:"config"`
	}

	if err := client.Call(ctx, "config/read", map[string]any{"includeLayers": false, "cwd": cwd}, &response); err != nil {
		result.Warnings = append(result.Warnings, "Could not read native defaults for the selected working folder.")
		return
	}

	config := response.Config
	result.ConfigStatus = "ready"
	result.Defaults = protocol.CodexDefaults{
		Model:           settingsText(config.Model),
		ModelProvider:   settingsText(config.Provider),
		ReasoningEffort: settingsText(config.Effort),
		ApprovalPolicy:  settingsApprovalPolicy(config.Approval),
		SandboxMode:     settingsText(config.Sandbox),
		ServiceTier:     settingsText(config.Tier),
		WebSearch:       settingsText(config.Search),
	}
}

func readCodexRequirements(ctx context.Context, client codexSettingsReader, result *protocol.CodexSettingsResponse) {
	var response struct {
		Requirements *struct {
			Provider  string            `json:"modelProvider"`
			Approvals []json.RawMessage `json:"allowedApprovalPolicies"`
			Sandbox   []string          `json:"allowedSandboxModes"`
			Search    []string          `json:"allowedWebSearchModes"`
		} `json:"requirements"`
	}

	if err := client.Call(ctx, "configRequirements/read", nil, &response); err != nil {
		result.Warnings = append(result.Warnings, "Managed policy details are unavailable. Codex still enforces its native requirements.")
		return
	}

	result.Managed.Status = "ready"
	if response.Requirements == nil {
		return
	}

	result.Managed.Configured = true
	result.Managed.ModelProvider = settingsText(response.Requirements.Provider)

	if response.Requirements.Approvals != nil {
		values := []string{}
		result.Managed.AllowedApprovalPolicies = &values
	}
	for _, policy := range response.Requirements.Approvals {
		if value := settingsApprovalPolicy(policy); value != "" {
			*result.Managed.AllowedApprovalPolicies = append(*result.Managed.AllowedApprovalPolicies, value)
		}
	}

	if response.Requirements.Sandbox != nil {
		values := []string{}
		result.Managed.AllowedSandboxModes = &values
	}
	for _, mode := range response.Requirements.Sandbox {
		*result.Managed.AllowedSandboxModes = append(*result.Managed.AllowedSandboxModes, settingsText(mode))
	}

	if response.Requirements.Search != nil {
		values := []string{}
		result.Managed.AllowedWebSearchModes = &values
	}
	for _, mode := range response.Requirements.Search {
		*result.Managed.AllowedWebSearchModes = append(*result.Managed.AllowedWebSearchModes, settingsText(mode))
	}
}

type nativeSettingsLimitWindow struct {
	Used     float64 `json:"usedPercent"`
	Duration *int64  `json:"windowDurationMins"`
	Resets   *int64  `json:"resetsAt"`
}

type nativeSettingsLimit struct {
	ID           string                     `json:"limitId"`
	Name         string                     `json:"limitName"`
	Primary      *nativeSettingsLimitWindow `json:"primary"`
	Secondary    *nativeSettingsLimitWindow `json:"secondary"`
	Reached      string                     `json:"rateLimitReachedType"`
	SpendReached *bool                      `json:"spendControlReached"`
}

func readCodexLimits(ctx context.Context, client codexSettingsReader, result *protocol.CodexSettingsResponse) {
	if result.Account.Status == "login_required" {
		return
	}

	var limits struct {
		Allowed *bool                          `json:"ordinaryUsageAllowed"`
		Limits  nativeSettingsLimit            `json:"rateLimits"`
		ByID    map[string]nativeSettingsLimit `json:"rateLimitsByLimitId"`
	}

	if err := client.Call(ctx, "account/rateLimits/read", map[string]bool{"excludeResetCreditDetails": true}, &limits); err != nil {
		if settingsAuthRequired(err) {
			result.Account.Status = "login_required"
			result.Account.LoginCommand = "codex login"
		}
		if result.Account.Type == "chatgpt" || result.Account.Status != "ready" {
			result.Warnings = append(result.Warnings, "Account limits are unavailable. Check Codex on the host and run codex login if sign-in expired.")
		} else {
			result.Warnings = append(result.Warnings, "Account limits are unavailable for this Codex provider.")
		}
		return
	}

	result.RateLimits.Status = "ready"
	result.RateLimits.OrdinaryUsageAllowed = limits.Allowed
	if limits.Allowed != nil && !*limits.Allowed {
		result.Warnings = append(result.Warnings, "Codex reports that included usage is blocked. Check usage and reset times on the host.")
	}

	if len(limits.ByID) == 0 {
		result.RateLimits.Buckets = append(result.RateLimits.Buckets, settingsLimit(limits.Limits))
		return
	}

	keys := make([]string, 0, len(limits.ByID))
	for id := range limits.ByID {
		keys = append(keys, id)
	}
	sort.Strings(keys)

	for _, id := range keys {
		limit := limits.ByID[id]
		if limit.ID == "" {
			limit.ID = id
		}
		result.RateLimits.Buckets = append(result.RateLimits.Buckets, settingsLimit(limit))
	}
}

func settingsLimit(limit nativeSettingsLimit) protocol.CodexRateLimitBucket {
	window := func(value *nativeSettingsLimitWindow) *protocol.CodexRateLimitWindow {
		if value == nil {
			return nil
		}

		return &protocol.CodexRateLimitWindow{
			UsedPercent:        value.Used,
			WindowDurationMins: value.Duration,
			ResetsAt:           value.Resets,
		}
	}

	return protocol.CodexRateLimitBucket{
		ID:                  settingsText(limit.ID),
		Name:                settingsText(limit.Name),
		Primary:             window(limit.Primary),
		Secondary:           window(limit.Secondary),
		ReachedType:         settingsText(limit.Reached),
		SpendControlReached: limit.SpendReached,
	}
}

func readCodexSkills(ctx context.Context, client codexSettingsReader, cwd string, result *protocol.CodexSettingsResponse) {
	var response struct {
		Data []struct {
			CWD    string `json:"cwd"`
			Skills []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Scope       string `json:"scope"`
				Enabled     bool   `json:"enabled"`
				Interface   *struct {
					Name string `json:"displayName"`
				} `json:"interface"`
			} `json:"skills"`
			Errors []json.RawMessage `json:"errors"`
		} `json:"data"`
	}

	if err := client.Call(ctx, "skills/list", map[string]any{"cwds": []string{cwd}}, &response); err != nil {
		result.Warnings = append(result.Warnings, "Native skills are unavailable for the selected working folder.")
		return
	}

	result.Skills.Status = "ready"
	for _, entry := range response.Data {
		if entry.CWD != cwd {
			continue
		}

		if len(entry.Errors) > 0 {
			result.Warnings = append(result.Warnings, "Some native skills could not load. Check Codex on the host.")
		}

		for _, skill := range entry.Skills {
			info := protocol.CodexSkillInfo{
				Name:        settingsText(skill.Name),
				Description: settingsText(skill.Description),
				Scope:       settingsText(skill.Scope),
				Enabled:     skill.Enabled,
			}
			if skill.Interface != nil {
				info.DisplayName = settingsText(skill.Interface.Name)
			}

			result.Skills.Skills = append(result.Skills.Skills, info)
		}
	}
}

func readCodexMCP(ctx context.Context, client codexSettingsReader, result *protocol.CodexSettingsResponse) {
	params := map[string]any{"limit": 100, "detail": "toolsAndAuthOnly"}
	seen := map[string]bool{}

	for {
		var page struct {
			Data []struct {
				Name    string                     `json:"name"`
				Auth    string                     `json:"authStatus"`
				Runtime string                     `json:"runtimeStatus"`
				Tools   map[string]json.RawMessage `json:"tools"`
				Error   *string                    `json:"toolsError"`
			} `json:"data"`
			Next string `json:"nextCursor"`
		}

		if err := client.Call(ctx, "mcpServerStatus/list", params, &page); err != nil {
			result.Warnings = append(result.Warnings, "Native MCP status is unavailable. Check the MCP connection in Codex on the host.")
			return
		}

		for _, server := range page.Data {
			info := protocol.CodexMCPServerInfo{
				Name:          settingsText(server.Name),
				AuthStatus:    settingsText(server.Auth),
				RuntimeStatus: settingsText(server.Runtime),
				ToolsStatus:   "ready",
				Tools:         []string{},
			}
			if server.Error != nil {
				info.ToolsStatus = "unavailable"
			}

			for name := range server.Tools {
				info.Tools = append(info.Tools, settingsText(name))
			}
			sort.Strings(info.Tools)
			result.MCP.Servers = append(result.MCP.Servers, info)
		}

		if page.Next == "" {
			result.MCP.Status = "ready"
			return
		}

		if seen[page.Next] || len(seen) >= 32 {
			result.Warnings = append(result.Warnings, "Native MCP discovery returned an incomplete page list.")
			return
		}

		seen[page.Next] = true
		params["cursor"] = page.Next
	}
}

func settingsApprovalPolicy(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		switch value {
		case "untrusted", "on-request", "never":
			return value
		}
	}

	var granular struct {
		Granular json.RawMessage `json:"granular"`
	}

	if json.Unmarshal(raw, &granular) == nil && len(granular.Granular) > 0 {
		return "granular"
	}

	return ""
}

var settingsCredential = regexp.MustCompile(`(?i)(api[-_ ]?key|token|secret|password|authorization)\s*[:=]|bearer\s+|\bsk[-_]|-----BEGIN .*PRIVATE KEY`)

func settingsText(value string) string {
	if settingsCredential.MatchString(value) {
		return "[redacted]"
	}

	value = strings.TrimSpace(strings.Map(func(char rune) rune {
		if char < ' ' {
			return ' '
		}
		return char
	}, value))

	chars := []rune(value)
	if len(chars) > 512 {
		value = string(chars[:512])
	}

	return value
}

func settingsAuthRequired(err error) bool {
	message := strings.ToLower(err.Error())
	markers := []string{
		"not logged in",
		"not signed in",
		"unauthorized",
		"token expired",
		"token has expired",
		"authentication required",
		"refresh token",
		"invalid authentication",
	}
	for _, marker := range markers {
		if strings.Contains(message, marker) {
			return true
		}
	}

	return false
}
