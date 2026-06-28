# tethon web

SvelteKit web companion — the **secondary screen** for tethon: a dashboard plus a full xterm.js raw terminal, driven by the same hub API the mobile client uses. It is deliberately the _thinner_ surface; the mobile Expo client is the primary one. Per the build order (`../kb/agent-control-design.md`), this is the fastest place to validate the session protocol — home turf — so prefer proving the core loop here over polishing UI.

General repo rules (git, commit tags, issue tracking, working principles, never reading `.env`) live in the root `CLAUDE.md`. This file covers web-specific rules only. Commit with the `[web]` tag.

## Tech Stack

- **Package manager / runtime**: Bun (not npm/pnpm/yarn).
- **Framework**: SvelteKit 2, **Svelte 5 with runes** (runes are forced on for project code — use `$state`/`$derived`/`$props`, not Svelte 4 reactive syntax).
- **Deploy target**: Cloudflare Workers via `@sveltejs/adapter-cloudflare`; `wrangler` for types, preview, and deploy.
- **Styling**: Tailwind CSS 4 — via the `@tailwindcss/vite` plugin, imported in `src/routes/layout.css` (`@import 'tailwindcss'`). No `tailwind.config` file; configure with `@theme` in CSS. `prettier-plugin-tailwindcss` sorts class lists on format.
- **Language**: TypeScript, `strict` mode.
- **Tooling**: Prettier + ESLint.

Experimental SvelteKit features are on (see `vite.config.ts`): async (`await` in components), **remote functions**, and rendering-error handling. Keep them on.

No database and no component library is set up yet — don't introduce one without it being asked for. This is a thin client over the hub, not a data app.

## Documentation

When you need Svelte or SvelteKit docs, use the LLM-optimized versions:

- Svelte — https://svelte.dev/docs/svelte/llms.txt
- SvelteKit — https://svelte.dev/docs/kit/llms.txt
- Remote functions — https://svelte.dev/docs/kit/remote-functions

## SvelteKit conventions

- Use direct `export const` / `export function` for route exports (`load`, `entries`, `prerender`, etc.). Don't add explicit `$types` annotations unless the code genuinely needs them.
- Prefer **remote functions** (`.remote.ts`) for client–server communication: `query` (read), `form` (form submit with progressive enhancement), `command` (write without a form). Place them in `src/lib/remote-functions/` and import via the `$lib` alias.
- Reserve `+server.ts` API routes for things remote functions can't do: webhooks, non-browser clients, third-party callbacks.
- The hub is the source of truth for session state — the web app reads/relays it, it does not own it. Mirror the protocol invariants in `../control/CLAUDE.md` (append-only events, cursor replay, reconnect-with-last-cursor); don't invent client-side state that diverges from the event log.

## Environment & bindings

Runtime env vars and bindings are Cloudflare Workers / Wrangler concerns.

- Access Worker env/bindings through `event.platform.env` (typed as `Env` in `src/app.d.ts`) — not `$env/static/private`.
- Bindings live in `wrangler.jsonc`. Run `bun gen` after editing it so generated Worker types stay current.
- **Never read `.env`.** When env vars need adding/updating, describe what's needed and let the user handle it.

## After editing

Run from `web/`:

```bash
bun format <changed files>   # prettier --write
bun lint                     # prettier --check + eslint
bun check                    # wrangler types + svelte-check (the type check)
```

`bun check` is the type check — it runs `svelte-check` against `tsconfig.json` plus `wrangler types`. If you start `bun dev` while working, kill it before finishing.

## Commands

```bash
bun dev        # dev server
bun build      # production build (wrangler types --check && vite build)
bun preview    # preview the built Worker locally
bun gen        # regenerate Cloudflare Worker types (after editing wrangler.jsonc)
bun check      # svelte-check + wrangler types
bun lint       # prettier + eslint checks
bun format     # prettier write
```
