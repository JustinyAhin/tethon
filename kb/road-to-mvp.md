# MVP milestones

This document defines scope and exit checks. Beads owns task status and dependencies.
Use `bd epic status` for progress and `bd show <id>` for ticket details.
See [known limitations](known-limitations.md) for current gaps.

## MVP definition

A user can control a real coding agent from a phone through the hub.
The host owns session state and runs each session in a separate worktree.
Clients can read output, send input, resolve approvals, and recover after connection loss.
The web app can control the same sessions.

Raw terminal polish, multi-user accounts, advanced repository browsing, and general file management are outside MVP scope.
Live Activities, Dynamic Island, billing, and account management are also outside MVP scope.

## Phase 0 — Protocol spike

Prove the append-only event log before adding real agents or clients.

Required scope:

- SQLite session and event storage.
- HTTP/SSE replay where `seq > from`.
- CLI session, input, stream, and approval commands using the fake agent.
- Ordering, persistence, replay, and approval tests.

Exit check: disconnect a fake session stream, append events, and reconnect from the last cursor.
The stream returns only later events.

## Phase 1 — Host daemon and hub split

Tracker: `tethon-sgn`.

- The host owns the session store and approvals.
- The hub registers hosts, tracks heartbeats, and routes client requests.
- Registrations include host identity, agent capabilities, and repositories.
- The CLI connects only to the hub.
- Cursor replay works through the relay.

Exit check: create and stream a session through the hub.
Restart the CLI and resume without duplicate events.
Local HTTP and the fake agent are sufficient for this milestone.

## Phase 2 — Real agent v0

Tracker: `tethon-19c`.
See [adapter research](agent-adapter-research.md) for the original CLI observations.

- Define an agent adapter interface.
- Map real agent output to session events.
- Supervise process start, input, exit, cancellation, and hard termination.
- Select a working directory for each session.
- Record process and stream errors.

Exit check: start Claude Code, read output, send input, and receive `done` or `error`.
Reconnect during the session and replay missed output.

## Phase 3 — Worktree isolation

Tracker: `tethon-zqe`.

- Register repositories with stable IDs, paths, and base references.
- Create one worktree per session.
- Support repository, branch, base-reference, and working-directory selection.
- Share dependency caches outside worktrees.
- Remove only owned worktrees under the cleanup policy.

Exit check: run two sessions in one repository without file or branch collisions.
Cancelling one session does not affect the other.
Repeated sessions do not leave duplicate caches or unwanted generated files.

## Phase 4 — Web companion v0

Tracker: `tethon-bko`.

- Show hosts, online state, session lists, and session details.
- Start sessions with repository and working-directory selection.
- Show structured events and resume from a stored cursor.
- Send input and resolve Approve, Deny, and Reply actions.
- Protect access for a single operator.
- Support base references, branch defaults, and working-directory selection.

Exit check: start a real session, reload the page, recover events, and resolve an approval.

## Phase 5 — Mobile app v0

Tracker: `tethon-927`.

- Authenticate against the hub.
- Start or open sessions from the phone.
- Show readable event streams and session details.
- Send input and resolve approvals.
- Store events and resume after an app restart.

Exit check: start or open a session, close the app, and resume from the stored cursor.
Send input and resolve an approval.

## Phase 6 — Push approvals

Tracker: `tethon-c1n`.

- Register devices and store push tokens.
- Send approval notifications with sufficient decision context.
- Support Approve, Deny, and Reply notification actions.
- Send decisions through the hub to the authoritative host daemon.
- Register the response listener at module scope.
- Inspect the last notification response at startup.

Exit check: receive an approval notification for a real session.
Resolve it from the lock screen and confirm that the agent continues.

## Phase 7 — MVP hardening

Tracker: `tethon-1a5`.

- Provide macOS installation and VPS deployment procedures.
- Keep active sessions awake on macOS.
- Support token rotation across the hub, hosts, web, mobile, and CLI.
- Preserve the required host and session index across hub restarts.
- Provide health checks, useful logs, and actionable errors.
- Retry host registration and reconnect after failures.

Exit check: install a host on a fresh Mac and register it.
Restart the hub and confirm that hosts reconnect.
Confirm that agents continue during client disconnection and that clients recover afterward.

## Phase 8 — MVP release candidate

Tracker: `tethon-jrl`.

- Maintain the [first-session guide](first-remote-agent-session.md).
- Use the same hub API from web and mobile.
- Support real output, input, terminal events, and approvals.
- Keep worktree isolation enabled by default.
- Cover replay, relay, approval, and cleanup failures with regression tests.
- Maintain [known limitations](known-limitations.md).

Exit check: start a session from the phone while away from the Mac.
Read output, send input, resolve approvals, and recover after connection loss.
Control the same session from the web app.

## Cross-cutting rules

- Keep host-owned state authoritative.
- Preserve cursor replay on every client-facing stream.
- Prefer structured events over terminal parsing.
- Prove each milestone before adding the next layer of complexity.
- Protect event and schema changes with replay and persistence tests.
