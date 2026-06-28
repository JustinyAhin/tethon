# tethon control

Go implementation of the **session protocol** — the append-only, cursor-based event log that the rest of the system is built on (see build order in `../kb/agent-control-design.md`). Currently the host-daemon/hub prototype: an HTTP server over an append-only session store, plus a CLI client to drive it.

Single Go module (`module tethon`). General repo rules live in the root `CLAUDE.md`; this covers Go specifics only.

## Layout

- `cmd/tethon/` — CLI entrypoint (`serve`, `session` subcommands)
- `internal/protocol/` — event + session schema (the wire contract)
- `internal/server/` — HTTP handlers + in-memory event broker (fan-out to subscribers)
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
- Keep `internal/server` responsible for HTTP behavior, request validation, and stream formatting.
- Keep `cmd/tethon` thin: parse flags, call server/client behavior, and print output.

## Dependencies and schema changes

- Prefer the Go standard library unless a dependency materially reduces control-plane complexity.
- Keep SQLite pure-Go unless there is a strong reason to require CGO.
- Any event envelope, event payload, or database schema change needs replay/persistence test coverage.
- Treat event JSON as a client contract. Avoid breaking it casually once clients consume it.

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
