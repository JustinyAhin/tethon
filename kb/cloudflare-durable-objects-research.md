# Cloudflare Durable Objects research

Research date: 2026-06-29. Code review: 2026-10-19.
This review did not test the deployed services or recheck Cloudflare limits and prices.

## Current conclusion

Keep the Go hub and host-owned session log.
Durable Objects remain a possible routing, event-distribution, or replay-cache layer.
They must not run agents or become the authoritative session store without a separate consistency design.

The original implementation triggers have changed:

- Host-initiated routing is implemented in the [host connection loop](../control/cmd/tethon-hostd/reverse.go) and [hub relay](../control/internal/hub/reverse.go).
- The web app has an [SSE pass-through route](../web/src/routes/sessions/[host]/[local]/stream/+server.ts).
- The [session page](../web/src/routes/sessions/[host]/[local]/+page.svelte) stores events and resumes from their highest sequence number.
- Mobile authentication, event storage, and approval notifications have implementations under `mobile/src/lib/`.

The old instructions to add host-initiated routing and the SSE pass-through are superseded.
Neither change required Durable Objects.
The research did not establish a present need for a hub rewrite.
Use the findings below as dated evidence, not current deployment instructions.

## 2026-06-29 streaming incident

The reported request to `/_app/remote/.../streamSession` failed with `[500] Error: Network connection lost.` on `web.tethon.dev`.
The same stream worked under local `bun dev`.
The document request returned `200` with `handleRenderingErrors` enabled.
The failing operation was the streaming remote call.

### Recorded stream path

These source locations describe the code inspected on the research date:

- `web/src/routes/sessions/[host]/[local]/+page.svelte:31` called `streamSession({ id, from })` with `for await`.
- `web/src/lib/remote-functions/sessions.remote.ts:84` used `query.live` to read and emit events again.
- `web/src/lib/server/hub-client.ts:143` fetched SSE and parsed it through `TextDecoderStream` and `reader.read()`.

The review attributed the failure to consuming and emitting the upstream body again inside the Worker.
Cloudflare guidance favored returning the upstream body without reading it.
The cited field reports described similar premature stream disconnections.
This was a diagnosis of the observed relay path, not a duration-limit benchmark.

Recorded configuration:

- `web/wrangler.toml` used `nodejs_als` without `nodejs_compat`.
- The Worker name was `tethon-web`.
- There were no Durable Object bindings or migrations.
- There were no `+server.ts`, `EventSource`, or WebSocket client paths.
- The hub relayed SSE with flushing and passed the cursor to hostd.
- Hostd replayed `seq > from`, then distributed live events.
- Neither stream leg emitted periodic SSE comments.

### Proposed pass-through

The review proposed this Worker response:

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

It moved stream parsing to the browser and kept the token on the server.
The proposal passed `?from=<lastSeq>` and retained remote functions for non-streaming requests.
It also recommended periodic `: keep-alive\n\n` comments, approximately every 30 seconds, for idle streams.
The implementation now has a pass-through route.

### Proposed session object

A `SessionObject(host_id/session_id)` could share one upstream connection among multiple clients.
It could mirror events for replay and retain connection state with `serializeAttachment()`.
The review referenced `tethon-bko.10` for existing browser cursor persistence.
A cache at the edge would change replay ownership and requires a separate design.
It does not remove the host's authority over session events.

The proposal required Durable Object bindings and migrations.
An outbound object-to-hub connection raised hibernation and cost concerns.
Those concerns require measurement before adoption.

## 2026-06-29 architecture snapshot

The original research described hub requests to registered host URLs.
That setup required inbound reachability, usually through Tailscale.
It treated host-initiated routing as future work.
That snapshot is superseded by the current [design](agent-control-design.md) and reverse connection implementation.

## Recorded Cloudflare properties

The research recorded these properties from the sources below on 2026-06-29.
Recheck them before choosing an architecture or estimating cost.

| Area               | Recorded behavior                                                                                                                                 |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| Identity           | Globally unique object names route clients and hosts to one coordination point.                                                                   |
| Storage            | Attached storage is strongly consistent. SQLite-backed storage was generally available and recommended for new namespaces.                        |
| Capacity           | SQLite storage limits were 10 GB on Paid and 1 GB on Free.                                                                                        |
| Execution          | Each object was single-threaded, with a soft limit near 1,000 requests per second.                                                                |
| WebSocket messages | Received messages were limited to 32 MiB.                                                                                                         |
| Connection state   | `serializeAttachment()` retained structured-clone state up to 16 KiB across hibernation.                                                          |
| CPU                | The default limit was 30 seconds per request, message, or alarm, configurable to five minutes. I/O wait was excluded.                             |
| Duration           | Connected HTTP requests had no fixed wall-time limit. The review separately cited an approximately 100-second edge idle timeout.                  |
| Outbound sockets   | Outbound WebSockets prevented hibernation and could keep an object active for up to 15 minutes per connection.                                    |
| Hibernation        | Client sockets could remain connected while in-memory state was discarded. Important state needed storage or recovery.                            |
| Alarms             | Alarms could wake an object with at-least-once execution.                                                                                         |
| SQL cursors        | `ctx.storage.sql.exec(query, ...bindings)` was synchronous. Consume its cursor before `await` because async boundaries had no snapshot isolation. |

## Candidate scope

| Component       | Proposed responsibility                                                           |
| --------------- | --------------------------------------------------------------------------------- |
| `HostObject`    | Host connection, capabilities, liveness, command routing, and event distribution. |
| `SessionObject` | Optional per-session event distribution and mirrored replay history.              |
| Host daemon     | Agent execution, worktrees, approvals, authoritative SQLite events, and replay.   |

The historical topology sketch was:

```txt
web/mobile
  -> Cloudflare Worker route
  -> HostObject(host_id)
  <-> outbound WebSocket from tethon-hostd
  -> agent sessions on hostd
```

Two stream options were considered:

1. A host object distributes events and asks hostd for history missing from its cache.
2. A host object routes to session objects that mirror and distribute individual session events.

The original recommendation favored the host-only experiment before adding session objects.
A replacement hub would need new deployment, authentication, observability, testing, and generated-client arrangements.
Raw PTY traffic would also need batching and backpressure control.

## Historical protocol sketch

These messages were proposals, not the current Go reverse protocol.

Host connection:

```txt
GET /hosts/:host_id/connect
Upgrade: websocket
Authorization: Bearer <host token>
```

Host messages:

```json
{ "type": "hello", "host_id": "macbook", "agents": [], "repos": [] }
{ "type": "event", "session_id": "ses_...", "event": { "seq": 12, "type": "output" } }
{ "type": "replay_response", "request_id": "r1", "events": [] }
```

Worker or object messages:

```json
{ "type": "create_session", "request_id": "r2", "payload": {} }
{ "type": "input", "session_id": "ses_...", "request_id": "r3", "text": "..." }
{ "type": "resolve_approval", "session_id": "ses_...", "request_id": "...", "action": "approve" }
{ "type": "replay", "session_id": "ses_...", "from": 42, "request_id": "r4" }
```

The proposal retained HTTP/SSE for clients while translating requests to the host connection.

## Risks and open decisions

- Keep hostd authoritative if an object also stores event copies.
- Define replay behavior when the cache is incomplete or unavailable.
- Preserve the hub API or regenerate clients from its replacement contract.
- Design authentication for hosts, web, mobile, CLI, and push callbacks.
- Measure outbound connection cost and hibernation behavior.
- Handle bursty output with batching, acknowledgement, and replay.
- Choose host-only or host-and-session object keys.
- Define event retention at the edge.
- Decide whether clients retain SSE or move to WebSockets.
- Decide how Cloudflare Access and bearer-token authentication interact.

## Prototype criteria

The original proposal was a separate experiment, not a production migration.
It included one Worker, one host object, and a temporary host connection.
It routed host listing, session creation, and output while retaining the Go hub.

Required results:

- The client connects to Cloudflare.
- A host behind NAT connects outbound without a VPS-to-host route.
- A real session starts and emits events.
- A reconnect recovers events through host replay.

Host-initiated routing now exists in Go.
Any new experiment needs a distinct benefit beyond that completed transport work.

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
