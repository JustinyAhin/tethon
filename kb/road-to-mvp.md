# Road to MVP

Last synced with Beads/code: 2026-06-29.

## MVP definition

The MVP is a usable remote coding-agent control loop:

- A host daemon runs on a Mac, registers with a hub, and owns session state.
- A client can start a coding-agent session in a chosen repo/cwd, stream structured output, send input, and reconnect from a cursor without losing events.
- Risky actions pause as approvals, and the user can approve, deny, or reply from the primary client.
- At least one real agent works end to end.
- The web companion is good enough to operate sessions from a laptop browser.
- The mobile app is good enough for the core phone workflow: monitor, respond, approve, and recover from flaky network.

Out of scope for MVP: raw PTY polish, multi-user teams, advanced repo browsing, full file management, Live Activities/Dynamic Island, and production-grade billing or account management.

## Phase 0 — Protocol spike

Status: done.

Goal: prove the append-only event log and cursor replay contract before building clients or real agent supervision.

Delivered:

- Go control module under `control/`.
- SQLite-backed session/event store.
- HTTP/SSE stream with `seq > from` replay.
- CLI commands for starting fake sessions, streaming, input, and approvals.
- Tests for ordering, persistence, reconnect replay, and approval lifecycle.

Exit check:

- Start fake session, disconnect, append events, reconnect from last cursor, and receive only later events.

## Phase 1 — Host daemon and hub split

Status: done.

Goal: prove the real architecture boundary: clients talk to one hub, but the host daemon owns session truth.

Deliver:

- `cmd/tethon-hub`: host registry, client-facing session routes, stream relay.
- `cmd/tethon-hostd`: owns SQLite session store, fake-agent sessions, and approval state.
- Host registration with host id, hostname, available agents, and repo roots.
- Heartbeats and online/offline host state in the hub.
- CLI talks to hub only.
- Preserve the same cursor replay behavior through the hub relay.

Keep simple:

- Local HTTP/SSE is fine for this phase.
- Host can connect to hub over localhost first.
- Fake agent remains the only agent.

Exit check:

- Start hub.
- Start host daemon pointing at hub.
- CLI creates a session through hub.
- Hub routes to host daemon.
- CLI streams through hub.
- Restart the CLI and resume from cursor without duplicate events.

## Phase 2 — Real agent v0

Status: done.

Goal: replace the fake agent with one real coding-agent backend while keeping the protocol stable.

Deliver:

- Agent adapter interface inside host daemon.
- Claude Code adapter first, if current CLI supports the needed stream and permission surfaces.
- Structured mapping from agent output into events: `output`, `awaiting_approval`, `done`, `error`.
- Session process supervision: start, stdin/input, exit status, cancellation, hard kill.
- Per-session working directory selection.
- Basic logs for agent process lifecycle and stream parsing errors.

Research before implementation:

- Done in `kb/agent-adapter-research.md`.

Exit check:

- Start a real Claude Code session from the CLI.
- See structured output over SSE.
- Send user input.
- Session exits with a `done` or `error` event.
- Restart client mid-session and replay missed output from cursor.

## Phase 3 — Worktree isolation

Status: done.

Goal: make parallel sessions safe by giving each session its own git worktree.

Deliver:

- Repo registry on the host daemon: stable repo ids, display names, base paths.
- Worktree manager per repo.
- Session create chooses repo, branch/base ref, and cwd.
- One session per worktree.
- Cleanup policy for completed, failed, and killed sessions.
- Guardrails to avoid deleting non-owned worktrees.
- Disk hygiene: shared dependency/build caches where safe, generated-artifact cleanup, and no accidental per-worktree cache bloat.

Exit check:

- Start two sessions for the same repo concurrently.
- Each runs in its own worktree.
- Branch/file changes do not collide.
- Killing one session does not affect the other.
- Repeated short-lived sessions do not leave large generated artifacts or duplicated dependency caches behind.

## Phase 4 — Web companion v0

Status: partially done; Beads epic still open.

Goal: validate the hub API with the fastest interactive client.

Deliver:

- SvelteKit app under `web/`.
- Host list with online/offline state.
- Repo/cwd/session start form.
- Session list and session detail view.
- Structured event stream UI with reconnect from cursor.
- Input box for sending messages.
- Approval cards with approve, deny, and reply.
- Minimal auth suitable for solo use.

Delivered so far:

- SvelteKit app under `web/`.
- Host list with online/offline state.
- Repo/cwd/session start form.
- Session list and session detail view.
- Structured event stream UI.
- Input box for sending messages.
- Approval cards with approve, deny, and reply.
- Solo auth path using Cloudflare Access for web plus hub bearer-token auth.

Still open:

- New-session form improvements for base refs, branch defaults, and cwd suggestions.
- Hard verification of web reload/reconnect from a persisted cursor. The server supports cursor replay, but the current web page streams from `0` and dedupes in memory.

Exit check:

- From web, start a real agent session through the hub.
- Disconnect/reload browser and recover stream from cursor.
- Resolve an approval from the web UI.

## Phase 5 — Mobile app v0

Status: mostly not started; only scaffold is done.

Goal: make the phone workflow usable before adding push.

Deliver:

- Expo app under `mobile/`.
- Session list and detail screens.
- Structured stream UI optimized for phone reading.
- Send-message input.
- Approval cards with approve, deny, reply.
- Cursor persistence on device so app restarts resume cleanly.
- Basic auth against hub.

Delivered so far:

- Expo app scaffold under `mobile/`.

Still open:

- Session list/detail screens.
- Phone-optimized stream UI.
- Send-message input.
- Approval cards.
- On-device cursor persistence.
- Hub auth.

Exit check:

- From phone, start or open a session.
- Background/kill/reopen the app and resume from last cursor.
- Send input and resolve approval.

## Phase 6 — Push approvals

Status: not started.

Goal: make approvals work without opening the app.

Deliver:

- Hub push service integration for Expo notifications.
- Device registration and token storage.
- Interactive notification category: approve, deny, reply.
- Notification payload carries enough context to decide safely.
- Notification action sends intent to hub; host daemon resolves pending approval.
- Mobile app registers notification-response listener at module top level and checks last notification response at startup.

Exit check:

- Real agent pauses for approval.
- Phone receives an interactive notification.
- Approve/deny/reply from lock screen.
- Host daemon resolves the pending approval and the agent continues.

## Phase 7 — MVP hardening

Status: partially done; Beads epic still open.

Goal: make the solo-developer MVP reliable enough to use while away from the Mac.

Deliver:

- Host daemon install/run story for macOS.
- `caffeinate` while sessions are active.
- Basic auth/token rotation for hub, host daemon, web, and mobile.
- Hub deployment instructions for a small VPS.
- Persistent hub registry for hosts and sessions.
- Structured logs and useful error messages.
- Health endpoints for hub and host daemon.
- Basic retry/backoff for host-to-hub connection.

Delivered so far:

- macOS LaunchAgent install/run story for `tethon-hostd`.
- Stable app-data paths for hostd state, worktrees, and shared caches.
- Basic auth/token rotation for hub, host daemon, web, and CLI.
- Retry/backoff for host registration and heartbeat.
- Dokploy hub deploy runbook exists in `kb/hub-deploy-dokploy.md`.

Still open:

- `caffeinate` while sessions are active.
- Persistent hub registry for hosts and sessions.
- More complete structured logs and useful error messages.
- Host daemon health endpoint.
- Mobile auth.

Exit check:

- Fresh Mac can install and register host daemon.
- Hub survives restart and hosts reconnect.
- Active sessions keep running if clients disconnect.
- Mobile and web can recover session state after network interruption.

## Phase 8 — MVP release candidate

Status: open.

Goal: freeze the minimum product and use it for real work.

Deliver:

- One documented happy path from install to first remote agent session.
- Web and mobile both operate the same hub API.
- Real agent sessions support output, input, done/error, and approvals.
- Worktree isolation is on by default.
- Regression tests for protocol replay, hub relay, approval lifecycle, and worktree cleanup.
- Known limitations documented in `kb/known-limitations.md`.

Delivered so far:

- First remote `claude-cli` session runbook in `kb/first-remote-agent-session.md`.
- Known limitations document.

Still open:

- Web and mobile client parity.
- Mobile-away-from-Mac release-candidate exit check.
- Final regression/limitations pass once mobile and push are implemented.

Exit check:

- Start a session from mobile while away from the Mac.
- Agent runs on the Mac in an isolated worktree.
- User can monitor, reply, approve/deny, and recover after network loss.
- Same session is visible and controllable from web.

## Cross-cutting rules

- Do not weaken cursor replay. Every client-facing stream must support reconnect from last seen event.
- Keep daemon-owned state authoritative. Clients and push handlers send intents; they do not own session state.
- Keep raw terminal as an escape hatch, not the core workflow.
- Prefer structured events over terminal scraping.
- Add real complexity only after the previous phase is working end to end.
- When changing event JSON or database schema, add replay and persistence tests in the same change.
