# tethon web

SvelteKit web companion — the **secondary screen** for tethon: a dashboard plus a full xterm.js raw terminal, driven by the same hub API the mobile client uses. It is deliberately the _thinner_ surface; the mobile Expo client is the primary one. Per the build order (`../kb/agent-control-design.md`), this is the fastest place to validate the session protocol — home turf — so prefer proving the core loop here over polishing UI.

General repo rules (git, commit tags, issue tracking, working principles, never reading `.env`) live in the root `CLAUDE.md`. This file covers web-specific rules only. Commit with the `[web]` tag.

## Tech Stack

- **Package manager / runtime**: Bun (not npm/pnpm/yarn).
- **Framework**: SvelteKit 2, **Svelte 5 with runes** (runes are forced on for project code — use `$state`/`$derived`/`$props`, not Svelte 4 reactive syntax).
- **Deploy target**: Cloudflare Workers via `@sveltejs/adapter-cloudflare`; `wrangler` for types, preview, and deploy.
- **Styling**: Tailwind CSS 4 — via the `@tailwindcss/vite` plugin, imported in `src/routes/layout.css` (`@import 'tailwindcss'`). No `tailwind.config` file; configure with `@theme` in CSS. `prettier-plugin-tailwindcss` sorts class lists on format.
- **Language**: TypeScript, `strict` mode.
- **Validation**: valibot — runtime validation via Standard Schema; pass schemas straight to remote `form(schema, fn)` / `query(schema, fn)`. Don't add a second validation library (no zod).
- **Svelte utilities**: Runed (`runed.dev`) — reach for it where a rune utility fits (e.g. `PersistedState`), but not where a better-suited tool already exists (don't replace a remote-function `query` with Runed's `resource`).
- **Tooling**: Prettier + ESLint.

Experimental SvelteKit features are on (see `vite.config.ts`): async (`await` in components), **remote functions**, and rendering-error handling. Keep them on.

No database and no component library is set up yet — don't introduce one without it being asked for. This is a thin client over the hub, not a data app.

## Documentation

When you need Svelte or SvelteKit docs, use the LLM-optimized versions:

- Svelte — https://svelte.dev/docs/svelte/llms.txt
- SvelteKit — https://svelte.dev/docs/kit/llms.txt
- Remote functions — https://svelte.dev/docs/kit/remote-functions

## Code conventions

- Use `type`, not `interface`.
- All exports at the end of the file, not inline (`const x = …` then `export { x };`). Route exports are the exception — see SvelteKit conventions below.
- Arrow functions, not function declarations.
- Functions with more than one parameter take a single object argument.

## SvelteKit conventions

- Use direct `export const` for route exports (`load`, `entries`, `prerender`, etc.) — these must be inline named exports, so they're exempt from the exports-at-end rule. Don't add explicit `$types` annotations unless the code genuinely needs them.
- Prefer **remote functions** (`.remote.ts`) for client–server communication: `query` (read), `form` (form submit with progressive enhancement), `command` (write without a form). Place them in `src/lib/remote-functions/` and import via the `$lib` alias.
- Hub HTTP calls should go through **`src/lib/server/hub-client.ts`**, which wraps the generated OpenAPI client. Avoid raw `fetch` to the hub in remote functions unless the generated SDK cannot express the behavior, such as the typed SSE stream helper.
- Reserve `+server.ts` API routes for things remote functions can't do: webhooks, non-browser clients, third-party callbacks.
- Name component files in kebab-case (e.g. `host-card.svelte`, not `HostCard.svelte`); import them under a PascalCase identifier.
- The hub is the source of truth for session state — the web app reads/relays it, it does not own it. Mirror the protocol invariants in `../control/CLAUDE.md` (append-only events, cursor replay, reconnect-with-last-cursor); don't invent client-side state that diverges from the event log.

## Environment & bindings

Env vars use SvelteKit's `explicitEnvironmentVariables` (enabled in `vite.config.ts`): declare each one in **`src/env.ts`** via `defineEnvVars` (with an optional valibot `schema`), then import it server-side from **`$app/env/private`**.

- **Values flow in by name**: `.env` in local dev, and the Worker environment in prod (`wrangler.toml` `[env.*.vars]` or a secret — `wrangler secret put NAME`). The same import works in both.
- `$app/env/private` is **server-only** — import it from `*.remote.ts`, `+*.server.ts`, or `hooks.server.ts`, never from a component or shared `$lib` module the client bundles.
- Worker **bindings** (not plain env vars) live in `wrangler.toml`; run `bun gen` after editing it so generated Worker types stay current.
- **Never read `.env`.** When an env var needs adding/updating, declare it in `src/env.ts`, then describe the `.env` line needed and let the user set it.

## After editing

Run from `web/`:

```bash
bun format <changed files>   # prettier --write
bun lint                     # prettier --check + eslint
bun check                    # wrangler types + svelte-check (the type check)
```

If `../control/internal/protocol` changes any JSON-facing type, event payload, request/response shape, or exported protocol constant, also run `bun run gen:protocol` before `bun check`.

If `../control/internal/hub` changes any client-facing hub route, request/response shape, path/query parameter, status behavior, or OpenAPI metadata, also run `bun run gen:hub-sdk` before `bun check`.

`bun check` is the type check — it runs `svelte-check` against `tsconfig.json` plus `wrangler types`. If you start `bun dev` while working, kill it before finishing.

## Commands

```bash
bun dev        # dev server
bun build      # production build (wrangler types --check && vite build)
bun preview    # preview the built Worker locally
bun gen        # regenerate Cloudflare Worker types (after editing wrangler.toml)
bun gen:protocol # regenerate TS protocol types from ../control/internal/protocol
bun gen:hub-sdk # regenerate Hub OpenAPI JSON and TypeScript SDK types
bun check      # svelte-check + wrangler types
bun lint       # prettier + eslint checks
bun format     # prettier write
```
