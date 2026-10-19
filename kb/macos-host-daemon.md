# macOS host daemon

Run `tethon-hostd` as a user LaunchAgent on a Mac.
Run script commands from the repository root.

## Prerequisites

- A deployed HTTPS hub and an accepted bearer token.
- Go and the selected agent CLI installed.
- A Git repository or a parent directory containing repositories.

Confirm Claude Code authentication:

```sh
claude auth status --text
```

For native Codex, install a supported version and sign in with `codex login`.
Check the version with `codex app-server daemon version`.
See [supported versions and ownership](agent-control-design.md#native-codex-sessions).

The daemon opens an outbound WebSocket to the hub.
Keep its diagnostics listener on a local or private interface.

## Install

```sh
scripts/macos-hostd.sh install \
  --hub-url https://hub.tethon.dev \
  --addr 127.0.0.1:39181 \
  --repo-root /path/to/repos \
  --name "$(hostname -s)"
```

`--repo-root` accepts one repository or a parent directory.
For a parent directory, the daemon registers immediate child repositories.

The script builds `~/.local/bin/tethon-hostd`, writes the LaunchAgent, and starts it.
Install and restart enable the LaunchAgent if macOS has disabled it.
It creates the state directory and a protected token file if needed.
Set the accepted hub token in `~/.config/tethon/hostd.env`:

```sh
TETHON_HUB_AUTH_TOKEN=<same-token-as-the-hub>
```

Keep this file out of Git.
Restart after setting the token:

```sh
scripts/macos-hostd.sh restart
```

Expected result: the host registers and opens its reverse WebSocket connection.
Follow [verification](#verify) before starting a session.
For token replacement, follow [hub token rotation](hub-deploy-dokploy.md#token-rotation).

## Operate

| Action                            | Command                            |
| --------------------------------- | ---------------------------------- |
| Check LaunchAgent state           | `scripts/macos-hostd.sh status`    |
| Follow both daemon logs           | `scripts/macos-hostd.sh logs`      |
| Check local and hub connectivity  | `scripts/macos-hostd.sh doctor`    |
| Restart the daemon                | `scripts/macos-hostd.sh restart`   |
| Remove the LaunchAgent and binary | `scripts/macos-hostd.sh uninstall` |

The doctor reports `PASS`, `WARN`, or `FAIL` for launchd, processes, listeners, logs, and hub connectivity.
It checks token presence without printing the token.

Query local diagnostics:

```sh
curl -fsS http://127.0.0.1:39181/healthz
curl -fsS http://127.0.0.1:39181/status
```

`/status` includes host identity, repositories, registration, retries, and reverse connection state.
It excludes tokens and environment file contents.

After code changes, reinstall with the same arguments before restarting.
Uninstall preserves state, configuration, and logs.

## Verify

From `control/`, run:

```sh
export TETHON_HUB_AUTH_TOKEN='<same-token-as-the-hub>'
go run ./cmd/tethon hosts list --hub-url https://hub.tethon.dev
```

Expected result:

- The host has `online: true`.
- Its agent list includes the installed, signed-in agent.
- Its repository list includes the intended repository.

The host log should include `host_registered` and `hub_reverse_connected`.

For Codex, select the host and working folder in the phone or browser.
Enter a prompt to start a conversation. Use Find conversations to resume one.
Keep the default settings unless an override is needed.
An override on a resumed conversation requires a prompt for the next turn.

To open the same native terminal on that host, run from `control/`:

```sh
go run ./cmd/tethon session attach <host-id/session-id> --hub-url https://hub.tethon.dev
```

Stop interrupts a turn. Archive, history deletion, and workspace cleanup are
separate actions. Native history deletion preserves working files.

## Troubleshooting

| Result                                                     | Check                                                                           |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `401 Unauthorized` or `host_registration_failed`           | Confirm that the host token matches a hub-accepted token.                       |
| Host missing from `/hosts`                                 | Check LaunchAgent state, the wrapper, the token, and outbound hub access.       |
| Host offline                                               | Check Mac sleep, network access, heartbeat errors, and reverse connection logs. |
| Session request returns `502`                              | Check hub logs and `hub_reverse_disconnected` or `hub_reverse_connect_failed`.  |
| Host online but session fails                              | Confirm repository registration and inspect the session error.                  |
| `unknown agent`                                            | Confirm that `claude` is on the PATH available to launchd. Restart the daemon.  |
| `prompt is required for claude-cli sessions`               | Supply a non-empty prompt.                                                      |
| `claude-cli refuses to run while ANTHROPIC_API_KEY is set` | Remove the API key for subscription-backed use.                                 |

Use `--allow-anthropic-api-key` only when API-key use is intentional.
Inspect `error` and `next_retry_delay` after registration or heartbeat failures.
`hub_heartbeat_recovered` confirms recovery. `hostd_serving` confirms the diagnostics listener.

## Paths

| Data          | Path                                                   |
| ------------- | ------------------------------------------------------ |
| Binary        | `~/.local/bin/tethon-hostd`                            |
| LaunchAgent   | `~/Library/LaunchAgents/dev.tethon.hostd.plist`        |
| Token file    | `~/.config/tethon/hostd.env`                           |
| Database      | `~/Library/Application Support/Tethon/hostd/hostd.db`  |
| Worktrees     | `~/Library/Application Support/Tethon/hostd/worktrees` |
| Shared caches | `~/Library/Application Support/Tethon/hostd/cache`     |
| Standard log  | `~/Library/Logs/Tethon/hostd.log`                      |
| Error log     | `~/Library/Logs/Tethon/hostd.err.log`                  |

The LaunchAgent wrapper passes explicit database, worktree, and cache paths.
