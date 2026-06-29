# macOS Host Daemon

This is the current install/run story for running `tethon-hostd` as a user
LaunchAgent on a Mac.

## Prerequisites

- A deployed hub, for example `https://hub.tethon.dev`.
- The same hub bearer token configured on the hub as `TETHON_HUB_AUTH_TOKEN`.
- Go installed on the Mac.
- A repo root that `tethon-hostd` should advertise.
- For the current prototype, a Mac address that the VPS hub can reach. Use the
  Mac's Tailscale IP or DNS name for `--addr`.

The current hub still calls the registered host URL directly. Host-initiated
persistent hub connections have not landed yet, so `--addr 127.0.0.1:39181`
only works when the hub is on the same machine.

## Install

From the repo root on the Mac:

```sh
scripts/macos-hostd.sh install \
  --hub-url https://hub.tethon.dev \
  --addr <mac-tailscale-ip>:39181 \
  --repo-root /path/to/repos \
  --name "$(hostname -s)"
```

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

- `scripts/macos-hostd.sh logs`
- the token in `~/.config/tethon/hostd.env`
- that the hub can reach `http://<mac-tailscale-ip>:39181`
- that the repo root exists and contains git repositories

## Paths

`tethon-hostd` defaults its macOS SQLite state to:

```txt
~/Library/Application Support/Tethon/hostd/hostd.db
```

The LaunchAgent wrapper passes explicit paths for:

- `-db ~/Library/Application Support/Tethon/hostd/hostd.db`
- `-worktree-root ~/Library/Application Support/Tethon/hostd/worktrees`
- `-cache-root ~/Library/Application Support/Tethon/hostd/cache`
