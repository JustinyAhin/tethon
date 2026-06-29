# tethon

Run coding agents (Claude Code, Codex, etc.) intensively, from anywhere — without being at the Mac. Spin up agents on demand across repos and host machines, and drive them from your phone with first-class ergonomics. A web companion acts as a secondary screen.

It's not a remote desktop. It's a purpose-built **agent control** experience: structured UI for the common case, raw terminal as an escape hatch, and push-notification approvals that work from the lock screen.

## Architecture

```
  Phone (Expo)  ─┐
                 ├──►  Hub  ──►  Host daemon (Mac)  ──► agents (worktrees)
  Web (SvelteKit)┘  (always-on   ──►  Host daemon (VPS)  ──► agents (worktrees)
                     small VPS)
```

- **Host daemon** (Go) — one per machine; owns session state, manages a git worktree pool, supervises agents, emits structured events, and registers/heartbeats with the hub.
- **Hub** — always-on coordinator on a small VPS; registry + routing + event relay. Single auth surface for clients.
- **Web companion** (SvelteKit) — dashboard and structured session UI. Raw terminal is still planned.
- **Mobile client** (Expo / React Native) — the primary surface target: approval cards, diff viewer, agent stream. The current app is only scaffolded.

The foundation is an **append-only event log per session with a cursor** — clients reconnect and replay from where they left off, so the experience survives flaky mobile networks.

## Status

Early but runnable through the hub, host daemon, CLI, and web companion.

Implemented now:

- Go hub, host daemon, and CLI under `control/`
- SQLite-backed append-only session/event log with cursor replay
- host registration, heartbeat, host/session relay, and bearer-token auth
- hub `/healthz`, hostd `/healthz` and `/status`, plus a macOS hostd doctor
- `claude-cli` adapter using the local Claude Code CLI and subscription auth
- per-session git worktrees with cleanup guardrails and shared build/dependency caches
- SvelteKit web UI for hosts, session creation, session list/detail, structured stream, input, approvals, and basic usage display
- macOS LaunchAgent install/run script for `tethon-hostd`
- Dokploy/Compose hub deployment path

Not implemented yet:

- host-initiated persistent session tunnel; the hub still reaches the registered host daemon URL directly
- mobile workflow beyond the Expo scaffold
- push approvals
- raw PTY/xterm escape hatch
- persistent hub registry
- `caffeinate` while sessions are active

## Repo

- `control/` — Go control plane: hub, host daemon, CLI, protocol, agent adapters, session log, repo/worktree management
- `web/` — SvelteKit companion app
- `mobile/` — Expo scaffold
- `deploy/hub/` — Dokploy/Compose hub deployment
- `scripts/` — host install/run and project scripts
- `kb/` — design brief, roadmap, runbooks, research, and limitations — start with [`kb/agent-control-design.md`](kb/agent-control-design.md)
- [`kb/first-remote-agent-session.md`](kb/first-remote-agent-session.md) — current happy path from install to first remote `claude-cli` session
- [`kb/hub-deploy-dokploy.md`](kb/hub-deploy-dokploy.md) — preferred Dokploy deployment path for `hub.tethon.dev`
- [`kb/macos-host-daemon.md`](kb/macos-host-daemon.md) — macOS install/run story for `tethon-hostd`
- [`kb/known-limitations.md`](kb/known-limitations.md) — current gaps and caveats
