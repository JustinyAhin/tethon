package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	claudeHookModeApprove = "approve"
	claudeHookModeDefer   = "defer"
	claudeHookModeDeny    = "deny"
	claudeHookModeReply   = "reply"
)

const claudeHookLogEnv = "TETHON_CLAUDE_HOOK_LOG"

type claudeHookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolUseID     string          `json:"tool_use_id"`
	ToolResponse  json.RawMessage `json:"tool_response"`
	Error         string          `json:"error"`
	IsInterrupt   bool            `json:"is_interrupt"`
	DurationMS    *int64          `json:"duration_ms"`
}

type claudeHookOutput struct {
	HookSpecificOutput claudeHookSpecificOutput `json:"hookSpecificOutput"`
}

type claudeHookSpecificOutput struct {
	HookEventName            string          `json:"hookEventName"`
	PermissionDecision       string          `json:"permissionDecision"`
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`
}

type claudeHookToolResult struct {
	HookEventName     string          `json:"hook_event_name"`
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

func RunClaudeHook(in io.Reader, out io.Writer, getenv func(string) string) error {
	var input claudeHookInput
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return fmt.Errorf("decode claude hook input: %w", err)
	}
	mode := getenv("TETHON_CLAUDE_HOOK_MODE")
	requestID := getenv("TETHON_CLAUDE_HOOK_REQUEST_ID")
	text := getenv("TETHON_CLAUDE_HOOK_TEXT")

	switch input.HookEventName {
	case "PostToolUse":
		return appendClaudeHookToolResult(getenv(claudeHookLogEnv), claudeHookToolResult{
			HookEventName:     input.HookEventName,
			Agent:             "claude-cli",
			ToolName:          input.ToolName,
			ToolInput:         input.ToolInput,
			ToolUseID:         input.ToolUseID,
			ApprovalRequestID: requestID,
			Status:            "success",
			DurationMS:        input.DurationMS,
			Response:          input.ToolResponse,
		})
	case "PostToolUseFailure":
		return appendClaudeHookToolResult(getenv(claudeHookLogEnv), claudeHookToolResult{
			HookEventName:     input.HookEventName,
			Agent:             "claude-cli",
			ToolName:          input.ToolName,
			ToolInput:         input.ToolInput,
			ToolUseID:         input.ToolUseID,
			ApprovalRequestID: requestID,
			Status:            "failure",
			DurationMS:        input.DurationMS,
			Error:             input.Error,
			IsInterrupt:       input.IsInterrupt,
		})
	}

	decision := claudeHookSpecificOutput{
		HookEventName:      "PreToolUse",
		PermissionDecision: "defer",
	}
	if input.HookEventName != "" && input.HookEventName != "PreToolUse" {
		return json.NewEncoder(out).Encode(claudeHookOutput{HookSpecificOutput: decision})
	}
	if requestID != "" && input.ToolUseID != requestID {
		return json.NewEncoder(out).Encode(claudeHookOutput{HookSpecificOutput: decision})
	}

	switch mode {
	case claudeHookModeApprove:
		decision.PermissionDecision = "allow"
		if len(input.ToolInput) == 0 {
			input.ToolInput = json.RawMessage(`{}`)
		}
		decision.UpdatedInput = input.ToolInput
	case claudeHookModeDeny, claudeHookModeReply:
		decision.PermissionDecision = "deny"
		decision.PermissionDecisionReason = text
		if strings.TrimSpace(decision.PermissionDecisionReason) == "" {
			decision.PermissionDecisionReason = "User denied this tool request."
		}
	default:
		decision.PermissionDecision = "defer"
	}
	return json.NewEncoder(out).Encode(claudeHookOutput{HookSpecificOutput: decision})
}

func appendClaudeHookToolResult(path string, result claudeHookToolResult) error {
	if path == "" {
		return nil
	}
	if len(result.ToolInput) == 0 {
		result.ToolInput = json.RawMessage(`{}`)
	}
	toolInput, inputRedacted, inputTruncated := sanitizeRawJSON(result.ToolInput, toolResultMaxBytes)
	response, responseRedacted, responseTruncated := sanitizeRawJSON(result.Response, toolResultMaxBytes)
	errText, errorTruncated := truncateString(result.Error, toolResultMaxBytes)
	result.ToolInput = toolInput
	result.Response = response
	result.Error = errText
	result.Redacted = result.Redacted || inputRedacted || responseRedacted
	result.Truncated = result.Truncated || inputTruncated || responseTruncated || errorTruncated
	if result.Truncated {
		result.MaxBytes = toolResultMaxBytes
	}
	if len(result.Response) == 0 {
		result.Response = nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open claude hook log: %w", err)
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(result); err != nil {
		return fmt.Errorf("write claude hook log: %w", err)
	}
	return nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
