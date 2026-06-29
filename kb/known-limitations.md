# Known Limitations

Last reviewed: 2026-06-29.

This file tracks current implementation gaps that are easy to miss when reading
the target design in `kb/agent-control-design.md`.

## Networking

- The host daemon registers and heartbeats outbound, but the hub still routes
  session operations by calling the host daemon URL recorded at registration.
  For remote use today, the VPS hub must be able to reach the Mac host daemon
  URL, usually over Tailscale.
- Host-initiated persistent session connections have not landed yet.

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
  `/status` diagnostics. These are process diagnostics, not a replacement for
  the planned host-initiated persistent session connection.

## Host Daemon

- `caffeinate` while sessions are active is still planned.
- macOS install/run is documented and scripted, but secret storage is an env
  file, not Keychain.
- Session isolation via per-session git worktrees remains the default. A
  direct-repo mode may be useful for local single-session workflows, but it
  should be added only as an explicit workspace mode with clear warnings about
  modifying the main checkout.

## Agent Events

- The Claude adapter currently surfaces `rate_limit_event` as user-visible
  `output` text. Beads tracks this as `tethon-4kl`; the likely fix is to drop
  routine events or introduce a dedicated notice/rate-limit event type.
- Claude approved tool executions emit first-class `tool_result` events with
  bounded/redacted input and result payloads. `diff_available` and broader
  non-Claude tool-use event coverage are target protocol ideas, not current
  emitted events.

## Web Deployment

- The deployed web app can be gated by Cloudflare Access, but the hub itself
  should not be placed behind Cloudflare Access yet. Current hub clients send
  bearer-token auth, not Cloudflare Access service-token headers.
