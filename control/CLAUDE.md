# tethon control

Go implementation of the **session protocol** — the append-only, cursor-based event log that the rest of the system is built on (see build order in `../kb/agent-control-design.md`). Currently the host-daemon/hub prototype: an HTTP server over an append-only session store, plus a CLI client to drive it.

Single Go module (`module tethon`). General repo rules live in the root `CLAUDE.md`; this covers Go specifics only.

## Layout

- `cmd/tethon/` — CLI client entrypoint (`hosts`, `session` subcommands); talks to the hub
- `cmd/tethon-hub/` — hub entrypoint; host registry and client-facing relay
- `cmd/tethon-openapi/` — emits the client-facing hub OpenAPI schema for web/mobile SDK generation
- `cmd/tethon-hostd/` — host daemon entrypoint; owns session state and agent processes
- `internal/hub/` — hub HTTP handlers, host registry, and session proxying
- `internal/protocol/` — event + session schema (the wire contract)
- `internal/server/` — host-owned session HTTP handlers + in-memory event broker (fan-out to subscribers)
- `internal/sessionlog/` — SQLite-backed append-only store; source of truth for sessions and events

## Protocol invariants

- Session events are append-only. Do not update or delete past events to express new state.
- Event `seq` values are per-session, strictly increasing, and start at `1`.
- Cursor replay means "return events where `seq > from`" — never replay `from` itself.
- The SQLite session log is the source of truth. The in-memory broker is only live fan-out for attached clients.
- Clients must be able to disconnect, reconnect with their last seen cursor, and continue without depending on live socket state.

## Package boundaries

- Keep `internal/protocol` dependency-light. It defines wire contracts and shared payload types only.
- Keep `internal/sessionlog` independent of HTTP/SSE concerns.
- Keep `internal/server` responsible for host-owned session HTTP behavior, request validation, and stream formatting.
- Keep `internal/hub` independent of session storage. It routes and relays; it does not own session truth. Its client-facing routes are Huma/OpenAPI-backed; host-daemon routes remain plain `net/http` internals.
- Keep `cmd/tethon` thin: parse flags, call server/client behavior, and print output.

## Worktree disk hygiene

Each session runs in its own git worktree (`internal/worktree`), created under the
host's `-worktree-root` (default `<db dir>/worktrees`). To keep parallel and
short-lived sessions from ballooning disk:

- **Git objects are shared.** Worktrees share the repo's object store, so only
  checked-out files are duplicated per session — not history.
- **Build/dependency caches are shared, not duplicated.** The daemon points each
  agent process at a shared cache root (`-cache-root`, default `<db dir>/cache`) via
  injected env: `GOCACHE`, `GOMODCACHE`, `npm_config_cache`, `PIP_CACHE_DIR`,
  `CARGO_HOME`. These are content-addressed / shared stores, safe across worktrees;
  unused vars are harmless to repos in other languages. The injection path is
  `ClaudeConfig.ExtraEnv` → `CommandSpec.Env` → `mergeEnv(os.Environ(), …)` (the
  shared values override anything inherited).
- **In-tree artifacts are per-worktree.** `node_modules`, `target/`, `dist/`, etc.
  live inside the worktree. A `done`/cancelled session's worktree is removed entirely
  (reclaiming them); a failed/interrupted session's worktree is **retained** for
  inspection — but because caches are external, what's retained is source plus
  in-tree output, not gigabytes of duplicated cache.
- **Removal is owned-only.** The manager only ever deletes worktrees it created or
  adopted from its own records, and only paths under its root (`ErrNotOwned`
  otherwise). It never touches the main checkout or a path it doesn't own.
- macOS/APFS copy-on-write may make worktree checkouts cheap, but this is an
  optimization — do not rely on it; the hygiene above must hold on any filesystem.

## Dependencies and schema changes

- Prefer the Go standard library unless a dependency materially reduces control-plane complexity.
- Keep SQLite pure-Go unless there is a strong reason to require CGO.
- Any event envelope, event payload, or database schema change needs replay/persistence test coverage.
- Treat event JSON as a client contract. Avoid breaking it casually once clients consume it.
- If `internal/protocol` changes any JSON-facing type, event payload, request/response shape, or exported protocol constant, regenerate shared protocol types and type-check both clients:

  ```sh
  cd ..
  bun run gen:protocol
  cd web
  bun check
  cd ../mobile
  bunx tsc --noEmit
  ```

- If `internal/hub` changes any client-facing route, request/response shape, path/query parameter, status behavior, or OpenAPI metadata, regenerate the shared hub SDK and type-check both clients:

  ```sh
  cd ..
  bun run gen:hub-sdk
  cd web
  bun check
  cd ../mobile
  bunx tsc --noEmit
  ```

## After editing Go code

Run from `control/`:

- `gofmt -w .` — format (Go has one canonical style; no config, no debate)
- `go build ./...` — compile. **This is the type check** — Go has no separate type-checker; the compiler resolves and checks every type, so a clean build means types are sound.
- `go vet ./...` — catch suspect constructs the compiler allows (bad format verbs, lost context, etc.)
- `go test ./...` — run tests

`gofmt -l .` lists any unformatted files without rewriting them (empty output = clean).

If the sandbox cannot write the default Go cache, use local temp caches:

```sh
GOCACHE=/tmp/tethon-gocache GOMODCACHE=/tmp/tethon-gomod go test ./...
```
