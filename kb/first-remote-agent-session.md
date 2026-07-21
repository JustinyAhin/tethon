# First Remote Agent Session

This is the current happy path from install to a first remote `claude-cli`
session running on a Mac host. It uses the hub, the macOS host daemon, and the
web companion. The mobile client supports the same main session workflow after
it has been connected to the hub through an internal preview build.

For current gaps that affect this path, see `kb/known-limitations.md`.

## Prerequisites

- A hub deployed at a public HTTPS URL, for example `https://hub.tethon.dev`.
- The hub bearer token configured as `TETHON_HUB_AUTH_TOKEN`.
- Go and Bun installed where you run the repo tools.
- Claude Code installed and authenticated on the Mac:

  ```sh
  claude auth status --text
  ```

- A git repo or repo root on the Mac that `tethon-hostd` should advertise.

Do not put the hub behind Cloudflare Access yet. The current clients send the
hub bearer token, not Cloudflare Access service-token headers.

## 1. Verify the hub

From any machine that can reach the hub:

```sh
curl -fsS https://hub.tethon.dev/healthz
```

Then verify authenticated access:

```sh
export TETHON_HUB_AUTH_TOKEN='<same-token-as-the-hub>'
curl -fsS \
  -H "Authorization: Bearer $TETHON_HUB_AUTH_TOKEN" \
  https://hub.tethon.dev/hosts
```

Expected result before the Mac daemon registers:

```json
{"hosts":[]}
```

## 2. Install the Mac host daemon

On the Mac, from the repo root:

```sh
scripts/macos-hostd.sh install \
  --hub-url https://hub.tethon.dev \
  --addr 127.0.0.1:39181 \
  --repo-root /path/to/repos \
  --name "$(hostname -s)"
```

Add the hub token to:

```txt
~/.config/tethon/hostd.env
```

Use this shape:

```sh
TETHON_HUB_AUTH_TOKEN=<same-token-as-the-hub>
```

Restart the daemon after editing the config:

```sh
scripts/macos-hostd.sh restart
```

Check status and logs:

```sh
scripts/macos-hostd.sh status
scripts/macos-hostd.sh logs
scripts/macos-hostd.sh doctor
```

Look for `host_registered` in the hostd logs. It should include the expected
`hub_url`, `host_id`, `advertised_url`, and `repo_count`. If registration or
heartbeat fails, the log line includes `error` and `next_retry_delay`.

The doctor checks launchd, local hostd `/healthz` and `/status`, hub `/healthz`,
token presence, and whether the host appears online through the hub when a token
is configured.

Session operations use the host daemon's outbound WebSocket to the hub. The
hub does not need to reach the local diagnostics listener.

## 3. Confirm the host is online

From `control/`:

```sh
export TETHON_HUB_AUTH_TOKEN='<same-token-as-the-hub>'
go run ./cmd/tethon hosts list --hub-url https://hub.tethon.dev
```

Expected result:

- the Mac host appears with `online: true`
- `agents` includes `claude-cli`
- `repos` includes the target repository

If `claude-cli` is missing, confirm `claude` is on the PATH seen by launchd and
restart the daemon. If the host is missing or offline, check the token and
`scripts/macos-hostd.sh logs`.

## 4. Start the web companion

From `web/`:

```sh
export HUB_BASE_URL='https://hub.tethon.dev'
export HUB_AUTH_TOKEN='<same-token-as-the-hub>'
bun install
bun run dev
```

Open the local URL printed by Vite, then verify the Hosts page shows the Mac as
online.

## 5. Start the first remote session

In the web companion:

1. Open `Sessions` -> `New session`.
2. Select the Mac host.
3. Select the target repo.
4. Choose `claude-cli` as the agent.
5. Optionally set `cwd`, `branch`, or `base_ref`.
6. Enter a prompt, for example:

   ```txt
   Inspect this repo and summarize the current test command. Do not edit files.
   ```

7. Submit the form and open the created session.

The session page should show `session_started`, streamed output, usage, and a
terminal `done` or `error` event. The agent process runs on the Mac, inside a
per-session worktree under:

```txt
~/Library/Application Support/Tethon/hostd/worktrees
```

## 6. Operate the session

Use the session detail page to send follow-up input. Press Enter to send, or
Shift+Enter for a newline.

If the agent emits an approval request, the web page shows an approval card with
Approve, Deny, and Reply actions. The daemon owns the pending approval state;
the web client only sends the chosen intent back through the hub.

Reloading the session page should replay missed events from the session log
instead of depending on a live-only socket.

## CLI fallback

The CLI can drive the same hub API when the web UI is not enough.

List hosts:

```sh
go run ./cmd/tethon hosts list \
  --hub-url https://hub.tethon.dev
```

Start a session:

```sh
go run ./cmd/tethon session start \
  --hub-url https://hub.tethon.dev \
  --host <host-id> \
  --repo /path/to/repo \
  --agent claude-cli \
  --prompt "Inspect this repo and summarize the current test command. Do not edit files."
```

Stream events from the beginning:

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

Use `deny` or `reply <request-id> <text>` instead of `approve` when appropriate.

## Troubleshooting

- `401 Unauthorized`: the hub token does not match between hub, host, web, or
  CLI environment.
- Host missing from `/hosts`: check `scripts/macos-hostd.sh status` and
  `scripts/macos-hostd.sh doctor`. Common causes are a stopped LaunchAgent, a
  missing token in `~/.config/tethon/hostd.env`, or hostd being unable to reach
  the hub.
- Host appears but is offline: heartbeats are not reaching the hub. Check
  hostd logs for `hub_heartbeat_failed`, the retry delay, Mac sleep/network
  state, and hub logs for `hub_host_heartbeat`.
- Host online but sessions fail: check `hub_reverse_connected` in the host
  logs, then confirm the repository is registered.
- Host online but session list/start returns `502`: check the hub and hostd
  logs for a dropped reverse WebSocket or a rejected session operation.
- Local daemon status unclear: query hostd directly with
  `curl -fsS http://127.0.0.1:39181/status` and check `registered`,
  `retrying`, `last_hub_success`, and `last_hub_failure`.
- `unknown agent`: `claude` was not available to `tethon-hostd` when it started.
- `prompt is required for claude-cli sessions`: start real Claude sessions with
  a non-empty prompt.
- `claude-cli refuses to run while ANTHROPIC_API_KEY is set`: unset the API key
  for subscription-backed Claude Code use, or install hostd with
  `--allow-anthropic-api-key` if that is intentional.
