# Agent control design

This document defines the target architecture and product requirements.
See [known limitations](known-limitations.md) for current gaps and the [MVP milestones](road-to-mvp.md) for acceptance criteria.

## Product scope

Tethon runs coding agents across repositories and host machines.
The phone is the primary client. The web app provides browser access.

- Show structured output, tool results, approval requests, and usage.
- Keep agents running when clients disconnect.
- Resume event streams from the last stored cursor.
- Support multiple hosts without client reconfiguration.
- Offer isolated Git worktrees when sessions need separate working files.
- Support approval actions from notifications.
- Add raw terminal access after the structured workflow is reliable.

## Architecture

Clients connect to one authenticated hub.
Each host daemon opens an outbound WebSocket connection to the hub.
This transport is implemented. The hub needs no inbound route to a host listener.
Tailscale can provide optional bulk-data access.

### Hub

The Go hub registers hosts and routes session requests.
It relays events and sends approval notifications.
The host registry is currently in memory. Push-device registrations use SQLite.
The hub does not own session events or pending approval state.

### Host daemon

The Go host daemon owns agent processes, worktrees, approvals, and the SQLite session log.
It registers capabilities, sends heartbeats, and reconnects to the hub.

- Use one worktree per session.
- Supervise agent processes and support cancellation and hard termination.
- Preserve failed or interrupted work for inspection.
- Share build caches outside worktrees.
- Keep diagnostics on a local or private listener.

Sleep prevention, Keychain integration, and raw PTY access remain planned.
Resource limits and timeouts are target supervision requirements.

### Native Codex sessions

For `tethon-87y`, hostd connects to the managed Codex app-server through its
local Unix socket. Keep the hub as the phone and browser transport.
Start the native daemon only when it is offline.
Do not restart or stop a shared daemon during connection cleanup.

Codex owns threads, turns, history, pending requests, settings, and queued input.
Tethon owns device access, routing, notifications, and append-only delivery records.
Keep native identifiers in these records. Reconcile native state after a
connection loss before enabling actions. Do not replay uncertain prompts.

Use an existing folder for new Codex conversations by default.
Offer isolated worktrees as an explicit option.
Turn completion, Stop, archive, and client disconnection must preserve working files.
Cleanup is a separate action. Keep the existing Claude adapter behavior.

Preserve native model, reasoning, permission, and instruction defaults unless
the user selects an override. Pass user prompts without added coordinator instructions.
Use native model, skill, MCP, and account discovery.

The first supported protocol line is Codex `0.163.x` on Unix hosts.
Verify other versions before enabling them.
See [adapter research](agent-adapter-research.md#codex) for performed checks,
required methods, and interface limits.

### Clients

The mobile app uses Expo and React Native.
The web app uses SvelteKit on Cloudflare Workers.
Both clients use the hub API and send actions to the host through the hub.
Clients store event copies for display and reconnect. The host log remains authoritative.

Raw terminal views and operating-system integrations remain separate product work.

## Session protocol

The protocol uses an append-only event log with a sequence number for each session.
After event `N`, a reconnect returns only events where `seq > N`.
Keep this contract independent of live connections and agent-specific output formats.

The [protocol source](../control/internal/protocol/protocol.go) owns event types and payloads.
The [Go rules](../control/CLAUDE.md#protocol-invariants) define implementation invariants.
Client-facing hub routes generate the shared SDK in `packages/hub-sdk/`.

## Approval lifecycle

1. The agent requests a tool action that needs approval.
2. The daemon records the pending request and emits `awaiting_approval`.
3. The hub sends a notification with enough context for a decision.
4. The client sends Approve, Deny, or Reply to the hub.
5. The daemon resolves the request and resumes the agent.

Do not depend on background notification execution.
Keep pending state on the daemon until it receives a decision.
Register response handling at module scope and inspect the last response at startup.
See the [mobile rules](../mobile/CLAUDE.md) for implementation requirements.

## Build and release

Use EAS Update for changes compatible with the installed native runtime.
Native changes require a new build.
Follow [release operations](ci-deploy.md#mobile-ota) for credentials, build profiles, updates, and verification.

## Build order

The [MVP milestones](road-to-mvp.md) define scope and exit checks.
Use Beads for task status and dependencies.
Keep raw terminal work after the core session and approval workflow.

## Open protocol decisions

- A dedicated event for routine provider notices, including rate limits.
- Additional tool-request and diff events beyond current `tool_result` coverage.
