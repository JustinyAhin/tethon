# Known limitations

Code reviewed: 2026-10-19. Deployed services were not tested in this review.
See the [design](agent-control-design.md) for target behavior.

## Clients

- The mobile app uses an internal EAS preview build.
- Raw PTY and xterm terminal views are not implemented.
- Web and mobile have the main session, input, and approval workflows.

## Session replay

The host protocol replays events where `seq > from`.
The CLI exposes `--from`.
The web page stores events with `PersistedState` and reconnects from the highest stored sequence number.
The mobile app stores session events and resumes from its stored cursor.
These code paths do not prove that every network and application-restart case has been tested.

## Hub state and networking

The hub keeps host registrations and the session index in memory.
Hosts register again after a hub restart.
Push-device registrations persist in SQLite.
Health endpoints do not prove durable registry or session recovery.

Hosts open outbound WebSocket connections to the hub.
The hub needs no inbound access to host diagnostics.
For deployment checks, use the [hub runbook](hub-deploy-dokploy.md#verify).

## Host daemon

- Sleep prevention with `caffeinate` remains planned.
- The Mac installation stores secrets in an environment file rather than Keychain.
- Sessions use separate Git worktrees by default.
- A direct-repository workspace mode requires a separate design and explicit selection.

## Host resource visibility

The host daemon has read-only resource snapshot handlers.
Hub relay and client views remain separate work.
The [resource policy](resource-visibility-policy.md) limits snapshots to Tethon-owned resources and defines redaction requirements.

## Agents and events

- Claude Code is the production adapter. Other agent adapters remain planned.
- Routine Claude `rate_limit_event` records are dropped.
- A dedicated provider-notice event remains an open protocol decision.
- Approved Claude tools emit `tool_result` with bounded and redacted payloads.
- `diff_available` and broader non-Claude tool events remain planned.

## Web access

Cloudflare Access can protect the web app.
Keep it off the hub until clients send the required service-token headers.
Current hub clients use bearer-token authentication.
