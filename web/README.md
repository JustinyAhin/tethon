# tethon web

SvelteKit companion app for operating Tethon sessions from a browser.

Current web capabilities:

- host dashboard with online/offline state
- new-session form backed by the hub API
- session list and session detail views
- structured SSE event stream rendering
- input composer for follow-up messages
- approval cards with approve, deny, and reply actions
- Cloudflare Access gate in deployed environments

Not landed yet:

- raw xterm.js terminal escape hatch
- persisted client cursor resume across page reload/app restarts
- richer repo ref/cwd pickers
- mobile parity

## Develop

Use Bun, not npm/pnpm/yarn.

```sh
bun install
bun dev
```

Required server-side environment variables:

- `HUB_BASE_URL` - hub URL, for example `https://hub.tethon.dev`
- `HUB_AUTH_TOKEN` - bearer token accepted by the hub

Do not commit local environment files. If values need changing, update your local environment yourself.

## Checks

```sh
bun lint
bun check
bun build
```

If `wrangler` reports stale generated Worker types, run:

```sh
bun gen
```

If hub routes or protocol types change, use:

```sh
bun run gen:protocol
bun run gen:hub-sdk
```

Component-specific coding rules live in `web/CLAUDE.md`.
