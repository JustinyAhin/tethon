package agent

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const (
	claudeHookModeApprove = "approve"
	claudeHookModeDefer   = "defer"
	claudeHookModeDeny    = "deny"
	claudeHookModeReply   = "reply"
)

type claudeHookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolUseID     string          `json:"tool_use_id"`
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

func RunClaudeHook(in io.Reader, out io.Writer, getenv func(string) string) error {
	var input claudeHookInput
	if err := json.NewDecoder(in).Decode(&input); err != nil {
		return fmt.Errorf("decode claude hook input: %w", err)
	}
	mode := getenv("TETHON_CLAUDE_HOOK_MODE")
	requestID := getenv("TETHON_CLAUDE_HOOK_REQUEST_ID")
	text := getenv("TETHON_CLAUDE_HOOK_TEXT")

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

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
