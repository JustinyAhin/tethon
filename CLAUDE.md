# tethon

A mobile-first system for running coding agents (Claude Code, Codex, etc.) intensively, from anywhere — spin up agents on demand across repos and host machines, and drive them from a phone. A web companion acts as a secondary screen.

Design docs, decisions, and build order live in `kb/`. Read `kb/agent-control-design.md` for the full brief before making architectural choices.

## Codebase Structure

The repo now has the first working control-plane slices:

- `control/` — Go module for the hub, host daemon, CLI, session protocol, SQLite event log, real `claude-cli` adapter, repo registry, and per-session worktrees.
- `web/` — SvelteKit companion app. It can list hosts/sessions, start sessions, stream structured events, send input, and resolve approvals. Raw terminal support has not landed yet.
- `mobile/` — Expo (React Native) scaffold only. The phone workflow is still planned work.
- `deploy/hub/` — Docker Compose/Dokploy deployment files for `tethon-hub`.
- `scripts/` — operational scripts, currently macOS host daemon install/run and Beads backup.
- `kb/` — design brief, roadmap, runbooks, research notes, and known limitations. Read `kb/agent-control-design.md` before architectural changes, but treat it as target design unless it explicitly says "current".

Current routing: the host daemon registers outbound and keeps a host-initiated WebSocket open to the hub for session operations. The hub no longer needs inbound network reachability to each `tethon-hostd` listener.

## Working Principles

1. **Think before coding.** No silent assumptions. State what you're assuming, surface tradeoffs, ask before guessing. Push back when a simpler approach exists.
2. **Simplicity first.** Minimum code that solves the problem. No speculative features or abstractions for single-use code. Still early — bias toward proving the core loop over building infrastructure.
3. **Surgical changes.** Touch only what you must. Don't "improve" adjacent code or refactor what isn't broken. Match existing style.

## Shared Context

- Keep repo-level instructions here. Put stack-specific rules in the nearest component `CLAUDE.md` (e.g. `daemon/CLAUDE.md`) as those land.
- Never read `.env` files. When environment variables need adding or updating, describe what's needed and let the user handle it.

## After Runtime Changes

When code changes affect a running local or deployed Tethon component, tell the
user exactly which runtime still needs restarting or redeploying.

- `control/cmd/tethon-hostd`, `control/internal/server`, `control/internal/agent`,
  `control/internal/repo`, `control/internal/worktree`, or `scripts/macos-hostd.sh`
  changed: run Go checks from `control/`, then tell the user to reinstall and
  restart local hostd with `scripts/macos-hostd.sh install ...same args...` and
  `scripts/macos-hostd.sh restart`.
- `control/cmd/tethon-hub`, `control/internal/hub`, or `deploy/hub/` changed:
  run Go checks from `control/`, run
  `TETHON_HUB_AUTH_TOKEN=dummy docker compose -f deploy/hub/docker-compose.yml config`,
  then tell the user to redeploy the hub in Dokploy.
- `web/` changed: run `bun lint` and `bun check` from `web/`; if hub API shape
  changed, run `bun run gen:hub-sdk` from the repo root. Protocol or hub shape
  changes also need `bunx tsc --noEmit` from `mobile/`. Tell the user the Worker
  must be deployed or promoted for `web.tethon.dev` to change.
- Docs-only changes need no runtime restart or deploy.

Never claim a local/deployed runtime has changed just because code was committed.
Say what still needs restart/redeploy.

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

## Issue Tracking

This project uses `bd` (beads) for issue tracking (prefix `tethon`).

- `bd prime` — get workflow context
- `bd ready` — find unblocked work
- `bd create "Title" --type task --priority 2` — create issue
- `bd close <id>` — complete work

**Never close bd issues unless explicitly asked.** Finishing the work doesn't mean the ticket is done — the user decides when to close.

When a Beads command updates `.beads/interactions.jsonl` as part of the work being committed, include that file in the same commit. Do not sweep unrelated interaction entries into unrelated commits.

### Backup

There is no Dolt remote, so back up beads through a git-tracked Dolt-native backup directory:

- `bash scripts/beads-backup.sh` — sync the Beads Dolt database to `.beads-backup/`
- **Only run this when explicitly told to.** Do not back up automatically after creating, updating, or closing tickets — wait for the user to ask.
- Restore on a new machine with `bd init --prefix tethon --skip-hooks && bd backup restore .beads-backup/ --force`
