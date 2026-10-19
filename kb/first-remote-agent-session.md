# First remote agent session

Use this guide to run Claude Code on a Mac from the web app.
The [mobile app](../mobile/README.md) can control the same sessions.
See [known limitations](known-limitations.md) before deployment.

## Prerequisites

- Go and Bun installed.
- A deployed HTTPS hub and an accepted bearer token.
- A Mac with Claude Code installed and authenticated.
- A Git repository on the Mac.

## 1. Verify the hub

Complete [hub setup and verification](hub-deploy-dokploy.md#verify).
Keep the hub outside Cloudflare Access until clients support its service-token headers.

## 2. Install the Mac host daemon

Follow [Mac installation](macos-host-daemon.md#install) and [host verification](macos-host-daemon.md#verify).
The host must appear online with `claude-cli` and the target repository.
Use [host troubleshooting](macos-host-daemon.md#troubleshooting) if a check fails.

## 3. Start the web companion

Follow [web setup](../web/README.md#develop).
Use the hub URL and an accepted hub token.
Open the local URL printed by Vite.
Confirm that the Hosts page shows the Mac online.

## 4. Start a session

1. Open `Sessions` → `New session`.
2. Select the Mac host.
3. Select the repository.
4. Select `claude-cli`.
5. Set `cwd`, `branch`, or `base_ref` if needed.
6. Enter this prompt:

   ```txt
   Inspect this repo and summarize the current test command. Do not edit files.
   ```

7. Submit the form.
8. Open the session.

Expected result: `session_started`, agent output, usage, and a terminal `done` or `error` event.
The agent runs in a worktree under the [host state directory](macos-host-daemon.md#paths).

## 5. Operate and reconnect

Use the session page to send follow-up input.
Press Enter to send. Use Shift+Enter for a newline.
For a pending approval, select Approve, Deny, or Reply.
The host daemon owns the pending state.

Reload the page during an active session.
Expected result: stored events remain visible and the stream resumes from the last cursor.

## CLI fallback

Run these commands from `control/` with `TETHON_HUB_AUTH_TOKEN` set to an accepted token.

Start a session:

```sh
go run ./cmd/tethon session start \
  --hub-url https://hub.tethon.dev \
  --host <host-id> \
  --repo /path/to/repo \
  --agent claude-cli \
  --prompt "Inspect this repo and summarize the current test command. Do not edit files."
```

Read events from the beginning:

```sh
go run ./cmd/tethon session stream \
  --hub-url https://hub.tethon.dev \
  <host-id>/<session-id> \
  --from 0
```

Send input:

```sh
go run ./cmd/tethon session input \
  --hub-url https://hub.tethon.dev \
  <host-id>/<session-id> \
  "Run the documented test command now."
```

Resolve an approval:

```sh
go run ./cmd/tethon session approve \
  --hub-url https://hub.tethon.dev \
  <host-id>/<session-id> \
  <request-id>
```

Use `deny` to reject the request.
Use `reply <request-id> <text>` to send a text decision.

## Troubleshooting

For host, authentication, or agent errors, use [host troubleshooting](macos-host-daemon.md#troubleshooting).
For token changes, follow [token rotation](hub-deploy-dokploy.md#token-rotation).
For web stream failures, inspect the browser request and Worker logs.
The [dated stream research](cloudflare-durable-objects-research.md) records the earlier Worker relay failure.
