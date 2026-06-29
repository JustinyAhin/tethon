# First Remote Agent Session

This is the current happy path from install to a first remote `claude-cli`
session running on a Mac host. It uses the hub, the macOS host daemon, and the
web companion. Mobile is intentionally not in this path yet; the mobile client
is covered by the Phase 8 client-parity work.

For current gaps that affect this path, see `kb/known-limitations.md`.

## Prerequisites

- A hub deployed at a public HTTPS URL, for example `https://hub.tethon.dev`.
- The hub bearer token configured as `TETHON_HUB_AUTH_TOKEN`.
- Tailscale on the VPS and Mac, or another route that lets the VPS hub reach
  the Mac daemon URL.
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
  --addr <mac-tailscale-ip>:39181 \
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
```

Current routing caveat: the hub calls the host daemon URL that the Mac
registers. Until host-initiated persistent hub connections land, the VPS must be
able to reach `http://<mac-tailscale-ip>:39181`.

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
- Host missing from `/hosts`: the LaunchAgent is not running, the token is
  missing from `~/.config/tethon/hostd.env`, or the daemon cannot reach the hub.
- Host online but sessions fail: the hub cannot reach
  `http://<mac-tailscale-ip>:39181`, or the repo path is not registered.
- `unknown agent`: `claude` was not available to `tethon-hostd` when it started.
- `prompt is required for claude-cli sessions`: start real Claude sessions with
  a non-empty prompt.
- `claude-cli refuses to run while ANTHROPIC_API_KEY is set`: unset the API key
  for subscription-backed Claude Code use, or install hostd with
  `--allow-anthropic-api-key` if that is intentional.
