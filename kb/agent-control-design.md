# Remote Coding-Agent Control — Design Brief

## Status of this document

This is the target architecture and product bar. It is not a complete description
of what the current code does.

Current prototype caveat: `tethon-hostd` registers and heartbeats outbound to
the hub, but session routing still works by the hub calling the host daemon URL
that the host registered. Until host-initiated persistent session connections
land, the hub machine must be able to reach each host daemon URL directly.

## What we're building

A mobile-first system for running coding agents (Claude Code, Codex, etc.) **intensively, from anywhere, without being at the Mac.** Spin up agents on demand, in different repos and folders, on one or more host machines, and drive them from a phone with first-class ergonomics. A web companion acts as a secondary screen.

The point is not a 1:1 remote desktop. It's a purpose-built, best-in-class *agent control* experience.

## The high bar

- **Feels native, not like a remote terminal.** Structured UI for the 90% case; raw terminal only as a rarely-used escape hatch.
- **Survives flaky networks.** Agents keep running and buffer output when no client is attached; phone reconnects and replays from a cursor. Must hold up on unreliable mobile connections.
- **Approvals are the killer feature.** When an agent wants to run something risky, a push notification arrives with enough context to decide, and Approve / Deny / Reply-with-text work from the lock screen — app-open not required.
- **Multi-host from day one.** Add a new Mac or VPS by running a daemon; it self-registers. No client reconfiguration.
- **Parallel agents** in different working directories without collisions.
- **Mac-independent iteration.** Day-to-day improvements ship without being tethered to Xcode.

## The decisions, locked

- **Host daemon language: Go.** Single binary, great concurrency, trivial to drop onto any host (Mac or VPS).
- **Mobile client: Expo (React Native).** Gets ~95–98% of native feel for this app's interaction profile (lists, streaming feeds, approval cards, chat input). Crucially, OTA updates via EAS Update mean most iteration ships without rebuilding or touching the Mac. Interactive notifications (incl. text-reply) are fully supported.
- **Web companion: SvelteKit.** Treated as a thinner second screen / dashboard with a full xterm.js raw terminal. Talks the same hub API.
- **Transport: host-initiated outbound to a hub.** Tailscale available for bulk data (file pulls, heavy log streaming), but the control plane does not depend on it.
- **Interface model: structured-first, TUI as escape hatch.** Daemon emits structured events; a raw PTY is available on demand to the same working dir.

Implementation note: the first runnable slice intentionally uses HTTP/SSE and a
registered host URL before the persistent outbound tunnel. See
`kb/known-limitations.md`.

## Architecture

```
  Phone (Expo)  ─┐
                 ├──►  Hub  ──►  Host daemon (Mac)   ──► agents (worktrees)
  Web (SvelteKit)┘  (always-on   ──►  Host daemon (VPS)   ──► agents (worktrees)
                     small VPS)   ──►  Host daemon (...)    ──► agents (worktrees)
```

### Hub (coordinator)
- Always-on, tiny, near-stateless (routing + session index). Lives on a small VPS (e.g. existing Hetzner).
- Single connection + single auth surface for clients, even across many hosts.
- Holds the registry: which hosts are online, their capabilities (`agents`, `repos`), and live sessions.
- Relays the structured event stream between hosts and clients.

### Host daemon (Go, one per machine)
- Registers with the hub on boot; heartbeats; reports capabilities.
- **Dials out** to the hub (host-initiated WebSocket) — robust to NAT, roaming, sleep.
- Manages a **git worktree pool** per repo → one agent per worktree = clean parallel agents, no branch collisions.
- Spawns/supervises agent processes (timeouts, resource caps, hard kill with worktree cleanup).
- Emits structured events; keeps a **raw PTY available on demand** as the escape hatch.
- **Owns all session state** — the source of truth. Clients are thin.
- Mac specifics: `caffeinate` while sessions are active; pluggable secret source (Keychain on Mac, env/file on VPS).

### Clients
- **Mobile (Expo):** structured-first UI — approval cards, native diff viewer, chat-style agent stream, send-message input. Raw terminal (xterm.js in WebView) as the secondary escape hatch.
- **Web (SvelteKit):** dashboard + full xterm.js terminal; same hub API.

## The session protocol (the thing to get right first)

Design the event stream as an **append-only log per session with a cursor** — not a live-only socket.

- Spawn: `POST /sessions { repo, branch, agent, cwd }`
- Stream: `GET /sessions/:id/stream` (SSE/WebSocket), resumable from a cursor
- Input: `POST /sessions/:id/input`
- Current event types: `session_started`, `output`, `awaiting_approval`, `approval_resolved`, `input_received`, `usage`, `done`, `error`
- Planned/possible event types: `tool_use_requested`, `diff_available`, and dedicated non-output notice events
- Reconnect contract: client says "I'm at event N," gets N+1 onward. This single decision is what makes the mobile experience solid on bad networks.

## Approval lifecycle (the core UX)

1. Agent requests a risky action → daemon emits `awaiting_approval` and **holds the pending state** (daemon is source of truth).
2. Hub sends a push with an interactive category: **Approve / Deny / Reply**, with enough context in the payload to decide.
3. User taps an action → intent round-trips to the hub → daemon resolves the pending state and the agent continues.
4. **Never assume the phone did background work.** Background notification JS isn't guaranteed (Android Doze; iOS ~2–3/hr cap), so the phone is only a prompt; the daemon does the work. (Design rule: notification carries context, action sends intent, daemon owns state.)

Implementation note (Expo): register the notification-response listener at module top-level and also check `getLastNotificationResponse` at startup, so taps work when the app was killed (known iOS sharp edge).

## Build & iteration story (Expo)

- **OTA-first:** build the native shell rarely; ship JS changes over-the-air via EAS Update (free tier covers a solo dev). Iterate from anywhere — no Mac, no rebuild.
- **Native builds when needed:** EAS cloud free tier (15 iOS + 15 Android/mo) or `eas build --local` on GitHub Actions. TestFlight install survives ~1 year for use while traveling.
- **Apple Developer account ($99/yr):** required for push + long-lived profiles.

## Known trade-offs accepted

- App size ~25–40MB (RN runtime). Smaller than almost everything else on the phone; not a concern.
- Raw terminal on mobile is a WebView and will feel like one — acceptable because it's the rarely-used fallback, not the core loop.
- Fancy OS integrations (Live Activities / Dynamic Island "agent running…" status) are possible but higher-friction in Expo. Flagged as a potential future native module if it becomes a wanted feature — it's a strong fit for an agent-monitoring app.

## Suggested build order

1. **Session protocol** — event schema + reconnect/replay logic. Prove it with a CLI client.
2. **Host daemon (Go)** — single session, single worktree, structured output from Claude Code.
3. **Hub** — registration + event relay.
4. **Web companion (SvelteKit)** — fastest place to validate the protocol (home turf).
5. **Mobile (Expo)** — structured cards once the protocol is proven.
6. **Approvals via push** — the killer feature, built on the proven protocol.
7. **Raw terminal escape hatch** — last, on both clients.
8. Multi-host scale-out, then VPS host support.

## Open items before the schema is truly locked

- Dedicated event shape for routine provider notices such as Claude rate-limit stream events.
- Whether `tool_use_requested` and `diff_available` should be first-class events before mobile/push work.
- Codex and any other agent output surfaces, to keep the event schema agent-agnostic.
