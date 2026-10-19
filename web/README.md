# Tethon web

Use the web app to start sessions, read output, send input, and resolve approvals.
See [known limitations](../kb/known-limitations.md) for current gaps.

## Develop

Install dependencies from the repository root:

```sh
bun install
```

Set these server-side environment variables:

| Variable         | Value                                     |
| ---------------- | ----------------------------------------- |
| `HUB_BASE_URL`   | Hub URL, such as `https://hub.tethon.dev` |
| `HUB_AUTH_TOKEN` | Accepted hub bearer token                 |

Keep the token out of Git.
Start the app from `web/`:

```sh
bun dev
```

## Checks

Run from `web/`:

```sh
bun lint
bun check
bun build
```

If generated Worker types are stale, run `bun gen`.
For API changes, follow the [generation and client checks](../control/CLAUDE.md#dependencies-and-schema-changes).
See [component rules](CLAUDE.md) for code changes and [Worker deployment](../kb/ci-deploy.md#web-worker) for release steps.
