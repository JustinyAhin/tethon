# Tethon control

Follow the [root instructions](../AGENTS.md).
This Go module contains the hub, host daemon, CLI, and session protocol.

## Protocol invariants

- Append session events. Do not change or delete past events to express new state.
- Start each session's event sequence at `1`. Increase it for each event.
- Replay only events where `seq > from`.
- Keep the host daemon's SQLite session log authoritative.
- Use the in-memory broker only to distribute live events.
- Preserve cursor replay across client disconnects and reconnects.

## Package boundaries

- Keep wire contracts in `internal/protocol` with few dependencies.
- Keep `internal/sessionlog` independent of HTTP and SSE.
- Keep host session handlers, validation, and stream formatting in `internal/server`.
- Use `internal/hub` for routing and relay. Keep session storage on the host.
- Use Huma/OpenAPI for client-facing hub routes. Keep host routes in `net/http`.
- Keep `cmd/tethon` limited to flags, client calls, and output.

## Worktree disk hygiene

- Use one worktree per session under `-worktree-root`.
- Share Git objects and build caches across sessions.
- Keep caches outside worktrees under `-cache-root`.
- Pass shared cache paths through `GOCACHE`, `GOMODCACHE`, `npm_config_cache`, `PIP_CACHE_DIR`, and `CARGO_HOME`.
- Remove worktrees for completed or cancelled sessions. Retain failed or interrupted work for inspection.
- Delete only worktrees owned by the manager and contained within its root.
- Preserve the main checkout and other worktrees.
- Do not depend on APFS copy-on-write behavior.

## Dependencies and schema changes

Prefer the Go standard library. Keep SQLite pure-Go unless a dependency change has a clear need.
Protect event and database schema changes with replay and persistence tests.
Preserve event JSON compatibility for existing clients.

For JSON-facing protocol changes, run from the repository root:

```sh
bun run gen:protocol
```

This includes event payloads, request and response types, and exported protocol constants.
For client-facing hub API changes, run from the repository root:

```sh
bun run gen:hub-sdk
```

This includes routes, parameters, status behavior, and OpenAPI metadata.
After either change, check both clients:

```sh
cd web
bun check
cd ../mobile
bunx tsc --noEmit
```

## After editing Go code

Format changed Go files with `gofmt`.
Run from `control/`:

```sh
go build ./...
go vet ./...
go test ./...
```

Use `gofmt -l .` to find formatting errors.
If the default cache is not writable, use temporary caches:

```sh
GOCACHE=/tmp/tethon-gocache GOMODCACHE=/tmp/tethon-gomod go test ./...
```
