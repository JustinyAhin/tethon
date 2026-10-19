# Tethon web

Follow the [root instructions](../AGENTS.md).
This SvelteKit app provides browser access to sessions. Use the `[web]` commit tag.
See [known limitations](../kb/known-limitations.md) for missing features.

## Application rules

- Use Bun. Install dependencies from the repository root.
- Use Svelte 5 runes, strict TypeScript, and Tailwind CSS 4.
- Keep the experimental features enabled in `vite.config.ts`.
- Use Valibot with Standard Schema. Do not add another validation library.
- Use Runed when a rune utility fits. Preserve remote-function queries for server data.
- Do not add a database or component library unless asked.
- Keep direct `export const` declarations for SvelteKit route exports.
- Add `$types` annotations only when needed.
- Put remote functions in `src/lib/remote-functions/`.
- Use `query` for reads, `form` for forms, and `command` for other writes.
- Use `src/lib/server/hub-client.ts` for hub requests.
- Use generated types from `@tethon/hub-sdk/hub-openapi` and `@tethon/hub-sdk/protocol`.
- Use raw `fetch` only when the generated SDK cannot express the request.
- Reserve `+server.ts` routes for streaming, webhooks, and other requests that remote functions cannot serve.
- Pass the upstream SSE body through without reading and emitting it again in the Worker.
- Keep session state authoritative on the host daemon. Follow the [protocol invariants](../control/CLAUDE.md#protocol-invariants).

## Environment and bindings

Declare environment variables in `src/env.ts` with `defineEnvVars`.
Import private values from `$app/env/private` only in server code.
Describe required local values for the user to set.
Keep Worker bindings in `wrangler.toml`.
After binding changes, run `bun gen` from `web/`.

## Checks

Format changed files with Prettier.
Run from `web/`:

```sh
bun lint
bun check
```

For protocol or hub API changes, follow the [generation and client checks](../control/CLAUDE.md#dependencies-and-schema-changes).
See [web setup](README.md) for development commands.

## Framework documentation

- [Svelte](https://svelte.dev/docs/svelte/llms.txt)
- [SvelteKit](https://svelte.dev/docs/kit/llms.txt)
- [Remote functions](https://svelte.dev/docs/kit/remote-functions)
