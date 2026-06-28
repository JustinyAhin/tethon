# tethon

A mobile-first system for running coding agents (Claude Code, Codex, etc.) intensively, from anywhere — spin up agents on demand across repos and host machines, and drive them from a phone. A web companion acts as a secondary screen.

Design docs, decisions, and build order live in `kb/`. Read `kb/agent-control-design.md` for the full brief before making architectural choices.

## Codebase Structure

The repo is empty beyond `kb/` — everything below is planned, built roughly in this order:

- **Host daemon** — Go. One per machine; owns session state, manages a git worktree pool, spawns/supervises agents, emits structured events.
- **Hub** — always-on coordinator on a small VPS. Routing + session index + event relay. Single auth/connection surface for clients.
- **Web companion** — SvelteKit. Dashboard + raw xterm.js terminal. Fastest place to validate the protocol.
- **Mobile client** — Expo (React Native). Structured-first UI: approval cards, diff viewer, agent stream. The primary surface.

## Working Principles

1. **Think before coding.** No silent assumptions. State what you're assuming, surface tradeoffs, ask before guessing. Push back when a simpler approach exists.
2. **Simplicity first.** Minimum code that solves the problem. No speculative features or abstractions for single-use code. Still early — bias toward proving the core loop over building infrastructure.
3. **Surgical changes.** Touch only what you must. Don't "improve" adjacent code or refactor what isn't broken. Match existing style.

## Shared Context

- Keep repo-level instructions here. Put stack-specific rules in the nearest component `CLAUDE.md` (e.g. `daemon/CLAUDE.md`) as those land.
- Never read `.env` files. When environment variables need adding or updating, describe what's needed and let the user handle it.

## Git

- **Never commit unless explicitly asked.** Do the work, run the checks, then stop and report.
- Do not add `Co-Authored-By` lines to commit messages.
- Keep commit descriptions short. Add a longer body only when it carries real context (non-obvious why, tricky tradeoff).
- Prefix every commit with a topic tag in square brackets so history is greppable. Pick one:
  - `[proto]` — session protocol, event schema, reconnect/replay
  - `[daemon]` — Go host daemon: worktree pool, agent supervision, PTY
  - `[hub]` — coordinator: registration, routing, event relay, auth
  - `[web]` — SvelteKit companion
  - `[mobile]` — Expo client: cards, diff viewer, stream, notifications
  - `[feat]` — new feature not tied to one component
  - `[fix]` — bug fixes
  - `[infra]` — deploy, build, CI, scripts
  - `[chore]` — deps, tooling, internal scripts
- Examples: `[proto] add cursor-based replay`, `[daemon] supervise agent with hard kill`, `[mobile] approval card actions`.
