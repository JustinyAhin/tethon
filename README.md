# Tethon

Run coding agents on your machines from a phone or browser.
Each session uses a separate Git worktree.
The clients show agent output and approval requests.
You can send input, stop sessions, and approve tool calls.

Tethon is in early development. It can run Claude Code.
Codex support and raw terminal access are planned.

## Start

You need a hub, a host machine, and an authenticated Claude Code installation.
Follow the [first-session guide](kb/first-remote-agent-session.md) for setup.
Read the [known limitations](kb/known-limitations.md) before deployment.

## Architecture

Clients send requests to the hub.
Each host daemon opens an outbound WebSocket connection to the hub.
The daemon runs agents and stores session events in SQLite.
Clients can reconnect and read stored events.

The mobile app uses Expo and React Native.
The web app uses SvelteKit on Cloudflare Workers.
The hub, host daemon, and CLI use Go.

## Development

Install Go and Bun.
Run these checks from the repository root:

```sh
bun install

cd control && go test ./...
cd ../web && bun run check && bun run lint
cd ../mobile && bun run lint
```

## Security

The hub token permits agent control on registered machines.

- Use HTTPS for the hub.
- Keep tokens in environment secrets or mobile secure storage.
- Keep secrets out of Git.
- Bind host diagnostics to a local or private network interface.
- Use the outbound host connection for session operations.

## Documentation

- [Design](kb/agent-control-design.md)
- [Hub deployment](kb/hub-deploy-dokploy.md)
- [macOS host installation](kb/macos-host-daemon.md)
- [Builds and deployment](kb/ci-deploy.md)
- [Mobile setup](mobile/README.md)

## License

[MIT](LICENSE).
