# Cloudflare Durable Objects Research

Last researched: 2026-06-29 (two passes — see "Update" section below).

## Question

Could Cloudflare Durable Objects fit Tethon's workflow and improve the user
experience compared with the current hub/hostd shape?

## Short answer

Yes, but not as the next immediate change.

Durable Objects look like a strong fit for the future host-initiated real-time
hub layer. They should not replace the host daemon, the per-session worktree
model, or the host-owned session log.

Recommended posture:

- Keep the current Go hub through the web/mobile MVP loop.
- Preserve hostd as the source of truth for sessions, approvals, worktrees, and
  agent process state.
- Revisit Durable Objects when implementing host-initiated persistent hub
  connections.
- Prototype them first as a narrow transport/routing layer, not as a full hub
  rewrite.

There is now one concrete, near-term forcing function that the original pass did
not know about: session streaming is broken on the deployed Cloudflare Worker.
The fix for that is a small SSE pass-through, not a Durable Object — but it is
where a session-scoped Durable Object would eventually live. See
"Update 2026-06-29: deployed Worker cannot relay SSE by consuming it in JS".

## Update 2026-06-29: deployed Worker cannot relay SSE by consuming it in JS

A second research pass was triggered by a concrete production failure, not a
future design question. Streaming a session from `web.tethon.dev` (Cloudflare
Worker) returns `[500] Error: Network connection lost.` on the
`/_app/remote/.../streamSession` request, while the identical code works under
local `bun dev` (Node). The document GET returns 200 because
`handleRenderingErrors` is on; the failure is the streaming remote call.

### Root cause

The session stream runs entirely inside the Worker as a SvelteKit `query.live`
generator that consumes the hub's SSE in JavaScript and re-emits it:

- `web/src/routes/sessions/[host]/[local]/+page.svelte:31` calls
  `streamSession({ id, from })` and iterates it with `for await`.
- `web/src/lib/remote-functions/sessions.remote.ts:84` is the `query.live`
  generator; it pulls from `streamSessionEvents` and re-`yield`s each event.
- `web/src/lib/server/hub-client.ts:143` opens a raw `fetch` to the hub SSE and
  reads the body in JS (`res.body.pipeThrough(new TextDecoderStream()).getReader()`
  then a `reader.read()` loop), parsing frames and yielding parsed events.

So the Worker holds a long-lived upstream `fetch`, consumes it chunk-by-chunk in
JS, and relays it over its own streaming response. Cloudflare's own guidance is
explicit that the verbatim-forward path is the robust one: "If your Worker only
forwards subrequest responses to the client verbatim without reading their body
text, then its body handling is already optimal." Reading the body in JS and
re-emitting is exactly the pattern that produces `Network connection lost.` when
the runtime recycles the request's I/O; the same class of failure is reported
for Workers relaying another Worker's stream over RPC ("ReadableStream received
over RPC disconnected prematurely").

This is **not** a duration limit. Cloudflare confirms there is no hard wall-clock
limit on a streamed HTTP response while the client stays connected, and the
30s CPU limit excludes time spent waiting on I/O. The failure is the JS relay
pattern, not stream length.

### Contributing config facts (verified in code)

- `web/wrangler.toml` sets `compatibility_flags = ["nodejs_als"]` only — there is
  no `nodejs_compat`, so only Web Streams (`ReadableStream`/`TransformStream`) are
  available. Worker name is `tethon-web`; no `[[durable_objects]]` or
  `[[migrations]]` block exists today.
- There are no `+server.ts` routes and no `EventSource`/`WebSocket` usage in
  `web/src` today; the whole streaming path goes through `query.live`.
- The Go side already relays SSE correctly and is cursor-backed end to end. The
  hub (`control/internal/hub/api.go` `streamSessionHuma`) proxies hostd's stream
  verbatim line-by-line, flushing after each `\n`, and passes `?from=<seq>`
  straight through. hostd (`control/internal/server/server.go` `handleStream`)
  does cursor replay (`Replay(ctx, id, fromSeq)` for `seq > from`) then live
  fan-out via an in-memory broker. Only the **web Worker leg** is broken — the
  hub sets `Cache-Control: no-cache` but neither leg emits SSE keep-alive
  comments (see idle-timeout caveat below).

## Two ways to fix the streaming leg

### Tier 1 — `+server.ts` SSE pass-through (no Durable Object) — the immediate fix

Add `web/src/routes/sessions/[host]/[local]/stream/+server.ts` that does the hub
`fetch` server-side (token stays in `$app/env/private`) and returns the upstream
body **verbatim**, without reading it in JS:

```ts
const upstream = await fetch(streamUrl, {
    headers: hubHeaders({ accept: 'text/event-stream' })
});
return new Response(upstream.body, {
    headers: {
        'content-type': 'text/event-stream',
        'cache-control': 'no-cache',
        connection: 'keep-alive'
    }
});
```

The browser consumes it with native `EventSource` (or `fetch` + `getReader`),
moving the SSE off `query.live`. Carry over today's cursor logic by passing
`?from=<lastSeq>` to the endpoint. Keep remote functions for the one-shot calls
(host list, session list, create, input, approval, cancel); only the streaming
leg changes.

Caveats to honor:

- Cloudflare's edge applies a ~100s idle timeout on Free/Pro plans (HTTP 524).
  The hub stream is event-driven and can sit idle, so emit a periodic SSE
  keep-alive comment (`: keep-alive\n\n`, ~every 30s) — best added on the hub side
  so every client benefits, or injected via a `TransformStream` in the
  pass-through. This is an edge/zone behavior, separate from the Workers runtime's
  lack of a hard duration limit.
- Verbatim pass-through plus `text/event-stream` + `no-cache` avoids edge
  buffering.

This is the smallest change that unblocks phone dogfooding and adds no new runtime
platform.

### Tier 2 — per-session Durable Object (the robust streaming layer)

A `SessionObject` keyed by `host_id/session_id` is the durable answer and folds in
work otherwise done separately:

- One upstream connection to the hub per session, fanned out to many clients
  (phone + web) over **hibernatable** client WebSockets: the object sleeps during
  idle gaps and wakes on `webSocketMessage`/new events, so a 20-minute idle phone
  session is cheap.
- The object's SQLite store mirrors the append-only event log at the edge, so
  reconnect replay is served from the object instead of re-streaming from the hub.
  This **subsumes the client-side cursor persistence** shipped in `tethon-bko.10`
  (currently localStorage) and makes it server-authoritative.
- Per-connection cursor can ride in `WebSocket.serializeAttachment()` (≤16 KiB,
  structured-clone) so it survives hibernation.

It still has to ingest from the hub: either the object holds an outbound SSE/WS to
the hub (outbound connections do not hibernate and keep the object billable up to
15 min/connection — cost-test this), or the hub is changed to push. The clean
version pairs with the host-initiated hub work described below. Requires a
`[[durable_objects]]` binding + `[[migrations]]` block in `wrangler.toml`.

### Recommendation (updated)

Do Tier 1 now — it is the correct, minimal fix for the deployed-streaming bug and
is independent of the larger hub question. Treat Tier 2 as the deliberate v1 of
the streaming/fan-out layer, adopted alongside (or as the first slice of) the
host-initiated hub work; when it lands it should absorb `tethon-bko.10`'s cursor
responsibility. Nothing here changes the earlier conclusion that hostd stays
authoritative and Durable Objects must not run agents or own the first session
log.

## Current Tethon shape

Relevant current constraints from `agent-control-design.md`,
`known-limitations.md`, and `road-to-mvp.md`:

- Clients talk to one hub.
- Host daemon owns session truth.
- Session events are append-only with cursor replay.
- The current hub registers/heartbeats hosts, but still calls the registered
  host daemon URL for session operations.
- Remote use currently requires the VPS hub to reach each hostd URL, usually
  over Tailscale.
- The target architecture is host-initiated outbound connections from hostd to
  the hub.

That target architecture is the point where Durable Objects become relevant.

## What Durable Objects provide

Cloudflare Durable Objects combine a globally addressable object, in-memory
coordination, and attached durable storage.

Useful properties for Tethon:

- A Durable Object has a globally unique name, so requests from clients and
  hostd can be routed to the same coordination point.
- Each object has strongly consistent attached storage.
- A single object can coordinate many connected clients.
- Durable Objects support WebSocket servers, including WebSocket Hibernation.
- Hibernating WebSockets can keep clients connected through Cloudflare while the
  object is not in memory.
- SQLite-backed Durable Objects are the recommended storage backend and are
  generally available.
- Alarms can wake an object later with at-least-once execution.

Relevant limits and behavior (figures re-verified against Cloudflare docs
2026-06-29):

- Each SQLite-backed object has a 10 GB storage limit on Workers Paid (1 GB on
  Free). SQLite-backed objects are GA and the recommended default for new
  namespaces.
- A single object is inherently single-threaded and has a soft limit around
  1,000 requests per second.
- WebSocket received messages are limited to 32 MiB.
- Per-connection state survives hibernation via `WebSocket.serializeAttachment()`
  (≤16 KiB, structured-clone) / `deserializeAttachment()`.
- CPU per request/message/alarm defaults to 30 seconds (excludes I/O wait) and
  can be configured up to 5 minutes.
- HTTP and Durable Object request wall time has no hard limit while the caller
  remains connected. Separately, Cloudflare's edge applies a ~100s idle timeout
  on Free/Pro plans (HTTP 524), so long-lived idle SSE/WebSocket streams need
  periodic keep-alive traffic regardless of the runtime's lack of a duration cap.
- Outbound WebSockets prevent hibernation and can keep the object alive for up
  to 15 minutes per connection.
- Hibernation discards in-memory state, so important state must be stored or
  recoverable.
- `ctx.storage.sql.exec(query, ...bindings)` runs SQLite synchronously and
  returns a cursor; fully consume the cursor (`.toArray()`/`.one()`) before any
  `await` (no snapshot isolation across async boundaries) — relevant if a
  `SessionObject` uses SQLite as the mirrored event log.

## Fit for Tethon

### Strong fits

Host-initiated control plane:

Durable Objects fit the design goal that hostd should dial out. A Mac hostd can
open an outbound WebSocket to Cloudflare, while web/mobile clients connect to
the same Cloudflare surface. This removes the current requirement that the VPS
hub reach the Mac daemon URL.

Per-host coordination:

A `HostObject` keyed by host id could hold live host connection state,
capabilities, recent heartbeat state, and connected client sockets. Commands
from clients can be routed over the hostd socket instead of via inbound HTTP to
the Mac.

Per-session live rooms:

A `SessionObject` keyed by `host_id/session_id` could coordinate connected web
and mobile clients, replay mirrored events, and fan out live updates. This maps
well to Tethon's append-only event stream.

Mobile resilience:

Durable Objects are well suited to flaky-client coordination. The phone can
disconnect and reconnect through Cloudflare without requiring direct access to
the host or VPS private network.

Approvals and push:

Approval intents can flow to the same object that knows the host connection.
The Durable Object can also be a natural place to coordinate push notification
fan-out and dedupe. The daemon should still own the pending approval state.

### Weak fits

Agent execution:

Durable Objects cannot run local coding agents, access the user's Mac repo, or
manage worktrees. That remains hostd's job.

Source of truth:

The daemon's local SQLite event log should stay authoritative for MVP. Durable
Object storage can mirror enough state for routing, replay acceleration, and
push, but it should not become the only record of agent state unless Tethon is
ready for a much larger consistency design.

Drop-in hub replacement:

The current hub is a Go service deployed through Dokploy. A Durable Object hub
would be a Workers/TypeScript implementation with different deployment,
testing, auth, observability, and generated-client implications. That is a
rewrite, not a simple deployment move.

High-frequency raw terminal:

For structured events and approval cards, Durable Objects are a good fit. For
raw PTY streams, especially if output is very chatty, batching and backpressure
would matter. Raw terminal should remain the escape hatch, as already planned.

## Candidate architecture

Start with the smallest useful Durable Object experiment.

```txt
web/mobile
  -> Cloudflare Worker route
  -> HostObject(host_id)
  <-> outbound WebSocket from tethon-hostd
  -> agent sessions on hostd
```

The `HostObject` would handle:

- hostd WebSocket attach/detach
- host capabilities and liveness mirror
- client command routing: create session, input, approval, cancel
- live event fan-out to connected web/mobile clients
- optional short replay cache or mirrored event log

Hostd would handle:

- session creation
- worktree allocation
- agent process lifecycle
- append-only SQLite event log
- pending approval truth
- cursor replay source of truth

Session stream options:

1. Host-object-only stream:
   - Hostd streams all events over its host WebSocket.
   - `HostObject` fans out events to attached clients.
   - On reconnect, client asks from cursor.
   - If the object has the needed mirrored events, it replays directly.
   - Otherwise it asks hostd for replay from cursor.

2. Per-session object:
   - `HostObject` handles host connection and routes to `SessionObject`.
   - `SessionObject(host_id/session_id)` stores/fans out session events.
   - This scales session fan-out and persistence more cleanly, but adds more
     moving parts.

For the first prototype, prefer option 1. Add `SessionObject` only if the
host-level object becomes too broad.

## Protocol sketch

Hostd opens an outbound WebSocket:

```txt
GET /hosts/:host_id/connect
Upgrade: websocket
Authorization: Bearer <host token>
```

Hostd sends:

```json
{ "type": "hello", "host_id": "macbook", "agents": [], "repos": [] }
{ "type": "event", "session_id": "ses_...", "event": { "seq": 12, "type": "output" } }
{ "type": "replay_response", "request_id": "r1", "events": [] }
```

Worker/Object sends to hostd:

```json
{ "type": "create_session", "request_id": "r2", "payload": {} }
{ "type": "input", "session_id": "ses_...", "request_id": "r3", "text": "..." }
{ "type": "resolve_approval", "session_id": "ses_...", "request_id": "...", "action": "approve" }
{ "type": "replay", "session_id": "ses_...", "from": 42, "request_id": "r4" }
```

Clients can keep the current HTTP/SSE API initially if the Worker translates
between HTTP routes and the host WebSocket. WebSocket clients can come later.

## Decision recommendation

Do not adopt Durable Objects immediately.

The immediate MVP risk is not the Go hub deployment. It is finishing the core
client experience:

- mobile session list/detail
- mobile auth
- stream cursor persistence
- approval actions
- push approvals

Durable Objects should be considered after those surfaces prove the protocol.
The first adoption trigger should be the planned removal of the current routing
caveat: "the hub must be able to reach hostd".

If Durable Objects are adopted, use them for:

- host-initiated persistent connection routing
- live WebSocket fan-out
- host/session registry mirror
- optional event replay cache
- push coordination

Do not use them for:

- agent execution
- repo/worktree management
- the first authoritative session log
- raw terminal as the primary workflow

## Risks

Platform rewrite risk:

Moving hub behavior to Durable Objects means adding a second backend platform
beside Go. That increases deployment and test surface area.

Consistency risk:

If both hostd SQLite and Durable Object SQLite store events, the system needs a
clear authority rule. The safest rule is: hostd is authoritative, Durable
Objects mirror.

Cost and hibernation risk:

Incoming client WebSockets can hibernate cheaply, but outbound WebSockets from
the Durable Object do not hibernate and can keep an object billable. A design
where the Durable Object itself holds outbound connections for long periods
needs cost testing.

Backpressure risk:

Agent output can be bursty. The hostd-to-object protocol needs batching,
acknowledgement, and replay from hostd so Cloudflare connection interruptions do
not lose events.

Auth risk:

The current hub uses bearer-token auth. A Worker/Durable Object hub needs a
separate auth design for hostd, web, mobile, CLI, and push callbacks.

Generated API risk:

The web app currently depends on the hub OpenAPI shape. If a Worker becomes the
client-facing hub, either preserve the existing API or regenerate clients from a
new source of truth.

## Open questions

- Should the Durable Object namespace be keyed by host id only, or by both host
  and session?
- How much event history should the edge mirror keep before asking hostd to
  replay?
- Should web/mobile continue to use SSE over HTTP, or switch session detail to
  WebSockets once the hub layer changes?
- What is the minimum hostd protocol needed to prove NAT-free session creation
  and event streaming?
- Can Cloudflare Access stay only on the web app while hub API auth remains
  token-based, or should the Worker hub validate Access/service tokens too?

## Suggested prototype

Build a separate spike, not a migration:

1. Add a small Worker with one `HostObject`.
2. Add a temporary hostd command or dev-only flag that opens an outbound
   WebSocket to that object.
3. Route only `list hosts`, `create session`, and live output events.
4. Keep the existing Go hub untouched.
5. Verify that a Mac behind NAT can start and stream a session without Tailscale
   reachability from the VPS.

Success criterion:

- A client reaches Cloudflare.
- Hostd dials out to Cloudflare.
- A session starts on the Mac.
- Events stream to the client.
- Client reconnect with cursor still works through hostd replay.

If that works, Durable Objects are worth considering for the real hub layer.
If it does not, keep the Go hub and implement host-initiated WebSockets there.

## Sources

- Tethon design brief: `kb/agent-control-design.md`
- Tethon known limitations: `kb/known-limitations.md`
- Tethon road to MVP: `kb/road-to-mvp.md`
- Cloudflare Durable Objects overview: https://developers.cloudflare.com/durable-objects/
- Durable Objects WebSockets and hibernation: https://developers.cloudflare.com/durable-objects/best-practices/websockets/
- Durable Objects SQLite storage API: https://developers.cloudflare.com/durable-objects/api/sqlite-storage-api/
- Durable Objects alarms: https://developers.cloudflare.com/durable-objects/api/alarms/
- Durable Object lifecycle: https://developers.cloudflare.com/durable-objects/concepts/durable-object-lifecycle/
- Durable Objects limits: https://developers.cloudflare.com/durable-objects/platform/limits/
- Durable Objects pricing: https://developers.cloudflare.com/durable-objects/platform/pricing/
- Workers streaming responses (ReadableStream / verbatim pass-through guidance): https://developers.cloudflare.com/workers/runtime-apis/streams/
- Workers platform limits (no hard duration limit while connected): https://developers.cloudflare.com/workers/platform/limits/
- Agents HTTP and Server-Sent Events: https://developers.cloudflare.com/agents/api-reference/http-sse/
- "Network connection lost" field reports (consume-and-re-emit pattern): https://community.cloudflare.com/t/worker-throws-network-connection-lost/287314 and https://github.com/cloudflare/workers-sdk/issues/8087
