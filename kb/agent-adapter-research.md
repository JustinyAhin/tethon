# Agent Adapter Research

## Constraint

Tethon should use the user's existing Claude Code and Codex subscriptions. It should not require Anthropic or OpenAI API keys for the MVP agent adapters.

Local verification on 2026-06-28:

- `claude auth status --text` reports `Login method: Claude Max account`.
- `codex doctor` reports ChatGPT auth, stored ChatGPT tokens, and no stored API key.
- No `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, `CODEX_API_KEY`, or `CODEX_ACCESS_TOKEN` environment variables were present during the check.

## Claude Code

Recommended MVP adapter path: invoke the installed `claude` CLI directly from `hostd`.

Why:

- Claude Code supports subscription login for local use.
- `claude -p --verbose --output-format stream-json` emits structured JSON events.
- The JSON stream includes system initialization, assistant messages, rate-limit events, final result, usage, session id, and permission denial fields.
- This avoids requiring Anthropic API keys or embedding a separate SDK dependency in the first adapter.

Observed command:

```sh
printf 'Reply with exactly: ok\n' | claude -p --verbose --output-format stream-json --tools ''
```

Observed useful event shapes:

- `system` with `subtype: "init"`, `session_id`, `cwd`, `tools`, `permissionMode`, `apiKeySource`.
- `assistant` with `message.content[]`, `session_id`, `request_id`.
- `rate_limit_event` with subscription-rate-limit state.
- `result` with `subtype`, `is_error`, `result`, `stop_reason`, `session_id`, `usage`, `permission_denials`.

Important details:

- `--output-format stream-json` requires `--verbose` when using `--print`.
- CLI help exposes `--input-format stream-json` for realtime streaming input, but the first MVP adapter should not depend on that until tested with multi-turn sessions.
- CLI help exposes `--permission-mode` values including `default`, `auto`, `plan`, `acceptEdits`, `dontAsk`, and `bypassPermissions`.
- For subscription safety, the adapter should refuse to run when `ANTHROPIC_API_KEY` is present unless explicitly overridden.

Open question before implementation:

- Whether `claude -p --input-format stream-json --output-format stream-json` can support a long-lived interactive subprocess that receives multiple user messages over stdin, or whether the MVP should model each user input as a resumed CLI turn.

## Codex

Recommended MVP adapter path: invoke the installed `codex` CLI directly from `hostd`, starting with non-interactive `codex exec --json`.

Why:

- Codex local CLI can use ChatGPT subscription auth.
- `codex exec --json` emits JSONL events suitable for mapping into Tethon events.
- This avoids requiring OpenAI API keys for the MVP adapter.

Observed command:

```sh
codex exec --json --sandbox read-only "Reply with exactly: ok"
```

Observed useful event shapes:

- `thread.started` with `thread_id`.
- `turn.started`.
- `item.completed` with `item.type: "agent_message"` and `text`.
- `turn.completed` with usage.

Important details:

- The installed Codex CLI is `codex-cli 0.142.3`.
- `codex doctor` confirms the local auth mode is ChatGPT, not API key.
- The installed `codex exec` exposes `--json`, `--sandbox`, `--cd`, `--add-dir`, `--ephemeral`, and resume support.
- The installed `codex exec` help does not expose `--ask-for-approval`, even though broader Codex docs discuss approval policy. The adapter should use the installed CLI surface as source of truth.
- `codex exec` is non-interactive by design. It is likely a better Phase 2 fallback/secondary adapter than the first long-lived coding-agent target.

Open question before implementation:

- Whether `codex app-server` or another subscription-backed local Codex surface provides a stable enough interactive stream for long-lived sessions. It is currently documented as experimental, so do not build MVP on it yet.

## Adapter recommendation

Build the Phase 2 adapter interface around a local subprocess, not a provider API client:

- `Start(ctx, session, initialPrompt)` launches a local CLI process in the session cwd.
- stdout/stderr JSON events are parsed and appended to the Tethon session log.
- `Input(ctx, text)` is initially allowed only for adapters that prove long-lived stdin support.
- `Cancel(ctx)` terminates the process and emits `done` or `error`.
- Adapters expose capabilities: `stream`, `interactive_input`, `approvals`, `resume`.

First implementation target:

1. `claude-cli` adapter using the installed `claude` binary and subscription auth.
2. Keep fake agent for tests.
3. Add `codex-cli` only after the Claude path proves the hostd adapter lifecycle.

## Sources

- Claude Code CLI reference: https://code.claude.com/docs/en/cli-reference
- Claude Code with Pro/Max plans: https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan
- Claude Agent SDK with Claude plan: https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan
- Codex manual, non-interactive mode: https://developers.openai.com/codex/noninteractive.md
- Codex manual, CLI reference: https://developers.openai.com/codex/cli/reference.md
- Codex manual, approvals and security: https://developers.openai.com/codex/agent-approvals-security.md
