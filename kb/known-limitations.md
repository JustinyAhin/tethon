# Known Limitations

Last reviewed: 2026-06-29.

This file tracks current implementation gaps that are easy to miss when reading
the target design in `kb/agent-control-design.md`.

## Networking

- The host daemon registers outbound and keeps a host-initiated WebSocket open
  to the hub for session operations. The hub does not need inbound reachability
  to the Mac host daemon listener.

## Clients

- The web companion is the only usable client for the current happy path.
- The mobile app is scaffolded, but it does not yet list sessions, stream
  events, send input, persist cursors, authenticate to the hub, or resolve
  approvals.
- Push approvals are not implemented.
- Raw PTY/xterm terminal support is not implemented in web or mobile.

## Session Replay

- The server-side protocol supports `seq > from` cursor replay.
- The CLI exposes `--from`.
- The web stream helper can pass a `from` value, but the current session page
  starts streams from `0` and dedupes by `seq` in memory. It does not persist
  the last cursor across reloads yet.

## Hub State

- The hub host registry is in memory. Hosts re-register via retry/backoff, but
  the hub does not persist registry/session index state across restarts yet.
- The hub has unauthenticated `/healthz`; hostd has local `/healthz` and
  `/status` diagnostics. These are process diagnostics, not a durable hub
  registry or session index.

## Host Daemon

- `caffeinate` while sessions are active is still planned.
- macOS install/run is documented and scripted, but secret storage is an env
  file, not Keychain.
- Session isolation via per-session git worktrees remains the default. A
  direct-repo mode may be useful for local single-session workflows, but it
  should be added only as an explicit workspace mode with clear warnings about
  modifying the main checkout.

## Host Resource Visibility

- Host resource visibility is planned as a read-only, Tethon-owned-resource
  snapshot. It is not a general remote process manager.
- The security/privacy policy for process, port, command, and path exposure is
  documented in `kb/resource-visibility-policy.md`.

## Agent Events

- Routine Claude `rate_limit_event` records are dropped. A dedicated
  notice/rate-limit event type remains a possible future protocol addition.
- Claude approved tool executions emit first-class `tool_result` events with
  bounded/redacted input and result payloads. `diff_available` and broader
  non-Claude tool-use event coverage are target protocol ideas, not current
  emitted events.

## Web Deployment

- The deployed web app can be gated by Cloudflare Access, but the hub itself
  should not be placed behind Cloudflare Access yet. Current hub clients send
  bearer-token auth, not Cloudflare Access service-token headers.
