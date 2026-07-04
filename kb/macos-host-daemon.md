# macOS Host Daemon

This is the current install/run story for running `tethon-hostd` as a user
LaunchAgent on a Mac.

## Prerequisites

- A deployed hub, for example `https://hub.tethon.dev`.
- The same hub bearer token configured on the hub as `TETHON_HUB_AUTH_TOKEN`.
- Go installed on the Mac.
- A repo root that `tethon-hostd` should advertise. This may be either a git
  repository or a workspace parent whose immediate children are git
  repositories.
- A local listener address for host diagnostics, for example
  `127.0.0.1:39181`.

The hub routes session operations over a host-initiated WebSocket, so the hub
does not need to reach the `--addr` listener. Keep `--addr` bound to a local or
private interface unless you intentionally want remote diagnostics.

## Install

From the repo root on the Mac:

```sh
scripts/macos-hostd.sh install \
  --hub-url https://hub.tethon.dev \
  --addr 127.0.0.1:39181 \
  --repo-root /path/to/repos \
  --name "$(hostname -s)"
```

`--repo-root` may point at one git repository or at a workspace parent such as
`~/work/projects`; hostd discovers git repositories in the parent directory's
immediate children and ignores non-repo children.

The script:

- builds `control/cmd/tethon-hostd` to `~/.local/bin/tethon-hostd`
- creates `~/Library/Application Support/Tethon/hostd/`
- creates `~/.config/tethon/hostd.env` with `0600` permissions if missing
- writes `~/Library/LaunchAgents/dev.tethon.hostd.plist`
- bootstraps and starts the LaunchAgent

Then add the hub token to:

```txt
~/.config/tethon/hostd.env
```

Use this shape:

```sh
TETHON_HUB_AUTH_TOKEN=<same-token-as-the-hub>
```

Do not commit this file. After editing it, restart the daemon:

```sh
scripts/macos-hostd.sh restart
```

When rotating the hub token, update this file after the new token has been
added to the hub's accepted token list. See `kb/hub-deploy-dokploy.md`.

## Operate

Check launchd status:

```sh
scripts/macos-hostd.sh status
```

Follow logs:

```sh
scripts/macos-hostd.sh logs
```

This tails both LaunchAgent log files:

```txt
~/Library/Logs/Tethon/hostd.log
~/Library/Logs/Tethon/hostd.err.log
```

Useful hostd log events:

- `host_registered`: registration reached the hub. Check `hub_url`,
  `host_id`, `advertised_url`, `repo_count`, and `agent_count`.
- `host_registration_failed`: hostd could not register. Check `error` and
  `next_retry_delay`.
- `hub_heartbeat_failed`: hostd registered before, but heartbeat failed. Check
  the hub URL, token, network route, and `next_retry_delay`.
- `hub_heartbeat_recovered`: heartbeat recovered after one or more failures.
- `hub_reverse_connected`: hostd established the outbound session-operation
  tunnel to the hub.
- `hub_reverse_connect_failed` / `hub_reverse_disconnected`: hostd could not
  establish or maintain the outbound tunnel.
- `hostd_serving`: confirms the local diagnostic listener.

Run the local doctor:

```sh
scripts/macos-hostd.sh doctor
```

The doctor prints `PASS`, `WARN`, and `FAIL` checks for launchd state, the
configured listener, stale manual `tethon-hostd` processes, recent logs, local
hostd `/healthz` and `/status`, hub `/healthz`, token presence, and whether the
host appears online through authenticated hub `/hosts` when a token is present.
It does not print token values.

The host daemon also exposes local diagnostics on its configured listener:

```sh
curl -fsS http://<mac-tailscale-ip>:39181/healthz
curl -fsS http://<mac-tailscale-ip>:39181/status
```

`/status` includes the host id, advertised URL, hub URL, registered repo names,
uptime, last hub success/failure, retry state, and reverse connection state. It
does not include bearer tokens or environment file contents.

Restart after changing config or reinstalling:

```sh
scripts/macos-hostd.sh restart
```

Uninstall the LaunchAgent and installed binary:

```sh
scripts/macos-hostd.sh uninstall
```

Uninstall leaves state, config, and logs in place:

- `~/Library/Application Support/Tethon/hostd/`
- `~/.config/tethon/hostd.env`
- `~/Library/Logs/Tethon/`

## Smoke Test

From `control/`, list hosts through the hub:

```sh
export TETHON_HUB_AUTH_TOKEN='<same-token-as-the-hub>'
go run ./cmd/tethon hosts list --hub-url https://hub.tethon.dev
```

The installed Mac should appear with `online: true`. If it does not, check:

- `scripts/macos-hostd.sh doctor`
- `scripts/macos-hostd.sh logs`
- the token in `~/.config/tethon/hostd.env`
- that logs show `hub_reverse_connected`
- that the repo root exists and contains git repositories

Common causes:

- `401 Unauthorized` or repeated `host_registration_failed`: the host token does
  not match the hub's accepted bearer token.
- Host is missing from `/hosts`: the LaunchAgent is not running, the wrapper
  cannot start `tethon-hostd`, or hostd cannot reach the hub.
- Host appears but is offline: the reverse WebSocket is disconnected, the Mac
  slept, the network changed, or the daemon restarted and is still reconnecting.
- Host is online but session operations fail: check `hub_reverse_disconnected`
  and hostd session logs for the failed operation.

## Paths

`tethon-hostd` defaults its macOS SQLite state to:

```txt
~/Library/Application Support/Tethon/hostd/hostd.db
```

The LaunchAgent wrapper passes explicit paths for:

- `-db ~/Library/Application Support/Tethon/hostd/hostd.db`
- `-worktree-root ~/Library/Application Support/Tethon/hostd/worktrees`
- `-cache-root ~/Library/Application Support/Tethon/hostd/cache`
