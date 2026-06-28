# tethon control

Go implementation of the **session protocol** — the append-only, cursor-based event log that the rest of the system is built on (see build order in `../kb/agent-control-design.md`). Currently the host-daemon/hub prototype: an HTTP server over an append-only session store, plus a CLI client to drive it.

Single Go module (`module tethon`). General repo rules live in the root `CLAUDE.md`; this covers Go specifics only.

## Layout

- `cmd/tethon/` — CLI entrypoint (`serve`, `session` subcommands)
- `internal/protocol/` — event + session schema (the wire contract)
- `internal/server/` — HTTP handlers + in-memory event broker (fan-out to subscribers)
- `internal/sessionlog/` — SQLite-backed append-only store; source of truth for sessions and events

## After editing Go code

Run from `control/`:

- `gofmt -w .` — format (Go has one canonical style; no config, no debate)
- `go build ./...` — compile. **This is the type check** — Go has no separate type-checker; the compiler resolves and checks every type, so a clean build means types are sound.
- `go vet ./...` — catch suspect constructs the compiler allows (bad format verbs, lost context, etc.)
- `go test ./...` — run tests

`gofmt -l .` lists any unformatted files without rewriting them (empty output = clean).
