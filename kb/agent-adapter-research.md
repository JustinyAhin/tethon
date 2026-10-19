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

Use the native managed app-server for interactive Codex sessions.
The earlier `codex exec --json` recommendation is superseded for `tethon-87y`.
Keep non-interactive execution outside this integration.

### Interface decision

Connect directly to the managed daemon's Unix socket with a WebSocket handshake.
Discover its socket and versions with `codex app-server daemon version`.
If it is offline, use the idempotent `codex app-server daemon start` command.
Initialize each connection once. Close only the Tethon connection during cleanup.

A Tethon-owned stdio server would add an engine and prevent direct terminal
attachment to that transport. A custom local listener would add setup and
ownership rules. The managed server already supplies shared connections.
Keep the hub for Tethon device access and notifications.
Native `remote-control` supplies separate pairing commands. Tethon does not need
new pairing credentials or a public native listener.

`codex app-server proxy` relays raw socket bytes. It does not convert JSONL
messages into WebSocket frames. Do not use it as a JSONL subprocess.

See [the design owner](agent-control-design.md#native-codex-sessions) for state
ownership, defaults, workspace lifecycle, and supported versions.

### Performed checks: 2026-10-19

The CLI and running managed daemon both reported `0.163.0` on macOS.
The daemon was already running. The checks did not restart or stop it.

- Generated stable JSON Schema and TypeScript bindings in a temporary directory.
- Generated experimental JSON Schema separately to identify gated methods.
- Initialized a direct Unix WebSocket connection.
- Read account type through `account/read`: `chatgpt`. No credential files were read.
- Read seven model descriptors through `model/list`.
- Started a native thread in a temporary working folder.
- Completed a real text turn through `turn/start` and native notifications.
- Joined the same thread from a second connection with `thread/resume`.
- Confirmed that the first connection could close while the thread stayed loaded.
- Read the same thread after reconnection. The working folder stayed unchanged.
- Opened the official terminal UI with `codex resume --remote unix:// <thread-id>`.
- Confirmed that the terminal showed the same prompt and response.
- Closed the terminal. It reported that running work continues after disconnection.

The native server canonicalized `/tmp` to `/private/tmp`.
Compare resolved paths when checking working-folder identity.
A new thread without its first persisted turn could not be resumed by another
connection. Create and persist the first turn before offering terminal attachment.

The implemented local hub and host path also passed real command output,
follow-up, Stop, archive, restore, and working-file preservation checks.
A browser answered a real native command approval with the offered cancel action.
An independent native connection observed resolution and turn completion.
The phone answered a real native question after a host restart.
The browser removed that request without a reload. An old response generation
was rejected while the current native request stayed pending.

Real steering and native queued input passed across a host restart.
Native history retained three unique user inputs across two completed turns.
The stale-turn Stop request was rejected. No prompt was sent again.

A source-built iOS simulator app displayed native history, sent follow-ups,
stopped a running tool turn, and sent another follow-up on the same thread.
The official terminal displayed the phone reply. The phone displayed a later
terminal reply once. Draft recovery after leaving and reopening also passed.
These checks do not establish physical-device notification delivery.

An isolated project supplied its own `AGENTS.md` and local skill.
Native discovery listed the enabled project skill. A native turn returned
markers supplied only by those files. Both files stayed unchanged.

Use native interactive source defaults for `thread/list`.
The tested server reported Tethon-created threads with source `vscode`.
A filter restricted to `cli` and `appServer` hid those conversations.

An already-loaded idle thread ignored model and reasoning changes on
`thread/resume`. Explicit overrides take effect through the next `turn/start`.
A real turn accepted a selected model, effort, and discovered `:read-only`
permission profile. A later native resume reported the selected effective settings.
The clients require a prompt when a resumed conversation selects an override.

### Required native surfaces

| Purpose | Native methods or events |
| --- | --- |
| Connection | `initialize`, `initialized` |
| Conversations | `thread/start`, `thread/list`, `thread/read`, `thread/resume` |
| History | `thread/turns/list`, `thread/items/list` |
| Input and Stop | `turn/start`, `turn/steer`, `turn/interrupt` |
| Live output | `turn/*`, `item/*`, `thread/status/changed` |
| Requests | Native server request IDs and exact response shapes |
| Resolution | `serverRequest/resolved` |
| Discovery | `model/list`, `config/read`, `skills/list`, `mcpServerStatus/list` |
| Account | `account/read`, `account/rateLimits/read` |
| Native queue | Experimental `thread/queue/*`; requires explicit capability opt-in |

Keep unknown event methods and bounded payloads in delivery records.
Disable unsupported requests rather than inventing answers.
Reconcile pending requests from the live server after reconnect.
Do not recreate pending requests from old delivery events.

## Adapter boundary

Keep `claude-cli` and the fake adapter functional.
Add Codex through the existing registry with a local app-server connection.
Do not add a provider agent loop or replace native history with Tethon events.
Keep process ownership separate from connection ownership.

## Sources

- Claude Code CLI reference: https://code.claude.com/docs/en/cli-reference
- Claude Code with Pro/Max plans: https://support.claude.com/en/articles/11145838-use-claude-code-with-your-pro-or-max-plan
- Claude Agent SDK with Claude plan: https://support.claude.com/en/articles/15036540-use-the-claude-agent-sdk-with-your-claude-plan
- Codex app-server: https://learn.chatgpt.com/docs/app-server
- Codex commands: https://learn.chatgpt.com/docs/developer-commands
