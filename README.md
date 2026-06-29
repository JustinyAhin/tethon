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

- **Host daemon** (Go) — one per machine; owns session state, manages a git worktree pool, supervises agents, emits structured events. Dials out to the hub.
- **Hub** — always-on coordinator on a small VPS; registry + routing + event relay. Single auth surface for clients.
- **Web companion** (SvelteKit) — dashboard + raw xterm.js terminal.
- **Mobile client** (Expo / React Native) — the primary surface: approval cards, diff viewer, agent stream.

The foundation is an **append-only event log per session with a cursor** — clients reconnect and replay from where they left off, so the experience survives flaky mobile networks.

## Status

Early. `control/` holds the first piece: a Go implementation of the session protocol (HTTP server over an append-only store, plus a CLI client).

## Repo

- `control/` — Go session-protocol prototype
- `kb/` — design brief, decisions, and build order — start with [`kb/agent-control-design.md`](kb/agent-control-design.md)
- [`kb/hub-deploy-dokploy.md`](kb/hub-deploy-dokploy.md) — preferred Dokploy deployment path for `hub.tethon.dev`
